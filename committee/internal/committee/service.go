package committee

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"committee-mvp/internal/attestation"
	"committee-mvp/internal/chain"
	"committee-mvp/internal/config"
	"committee-mvp/internal/crypto"
	"committee-mvp/internal/p2p"
	"committee-mvp/internal/protocol"
	"committee-mvp/internal/rpc"
	"committee-mvp/internal/share"
	"committee-mvp/internal/wire"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// Service orchestrates committee protocol flow for the MVP runtime.
type Service struct {
	cfg           *config.NodeConfig
	net           p2p.Network
	bls           *crypto.BN254BLS
	router        *protocol.Router
	quoteVerifier attestation.Verifier
	chainGateway  chain.Gateway

	nodeIndex int

	nonce    uint64
	autoSign uint32
	dkgOnce  sync.Once
	dkgReady chan struct{}

	// shareKeyCache holds signer public keys already decoded to G2, keyed by
	// signer index. Committee membership is fixed within an epoch, so a key is
	// decoded once and reused across every signing session; G2.Unmarshal is
	// ~0.6ms and was 12.8ms of the 16.9ms share batch at N=21.
	shareKeyCache map[int][]byte
	// shareKeys decodes each signer's public key once and reuses it across
	// signing sessions. Committee membership is fixed within an epoch, and
	// G2.Unmarshal is ~0.6ms, so re-decoding 21 keys per session cost 12.8ms of
	// a 16.9ms batch.
	shareKeys            *crypto.ShareKeyCache
	sessions             map[string]*signSession
	pending              map[string]pendingSignRequest
	dkgShares            map[int]*big.Int
	dealerPubKeys        map[int][]byte
	committeePubKey      []byte
	aggResults           map[string]wire.AggResultPayload
	lastAggSessionID     string
	quoteSessions        map[string]*quoteRegistrationSession
	redactSessions       map[string]*redactAuthorizationSession
	teeRevocations       map[string]*teeRevocationSession
	activeSetCerts       map[string]*activeSetCertSession
	activeSetCertByEpoch map[string]string
	mu                   sync.Mutex
	controlServer        *http.Server

	wg sync.WaitGroup
}

type signSession struct {
	message   []byte
	responses map[int]wire.SignResponsePayload
	// verifying holds signer indices whose share is currently being verified
	// off-lock, so a duplicate arriving mid-verification cannot be accepted
	// twice and cannot queue a second verification of the same share.
	verifying map[int]struct{}
	// aggregating is claimed under s.mu by the goroutine that observes the
	// threshold crossing, so exactly one caller aggregates a given session.
	// It is released on failure to preserve the retry-on-next-share behaviour.
	aggregating bool
	aggregated  bool
	// done is closed once the certificate has been stored, letting a caller
	// block until the threshold signature is ready instead of polling. Never
	// send on it: close only, so every waiter observes the completion.
	done         chan struct{}
	requestNonce uint64
	// startedAt is when the coordinator published the sign request; anchors use
	// it to report elapsed time without cross-process clock assumptions.
	startedAt time.Time
}

// completion returns the channel closed when the session's certificate is
// stored, creating it on first use. Callers must hold s.mu.
func (s *signSession) completion() chan struct{} {
	if s.done == nil {
		s.done = make(chan struct{})
	}
	return s.done
}

// waitForAggregate blocks until the session's certificate is stored, the
// context is done, or deadline elapses. It returns immediately when the result
// is already available, so callers never pay for a poll when the threshold
// signature finished before they asked.
func (s *Service) waitForAggregate(ctx context.Context, sessionID string, deadline time.Time) (wire.AggResultPayload, bool) {
	for {
		s.mu.Lock()
		if result, ok := s.aggResults[sessionID]; ok {
			s.mu.Unlock()
			return result, true
		}
		if time.Now().After(deadline) {
			s.mu.Unlock()
			return wire.AggResultPayload{}, false
		}
		state, ok := s.sessions[sessionID]
		if !ok {
			s.mu.Unlock()
			return wire.AggResultPayload{}, false
		}
		// completion() and the aggResults check share this lock hold, so a
		// completing aggregation either publishes before we look (and we
		// return above) or closes the channel we just captured.
		done := state.completion()
		s.mu.Unlock()

		select {
		case <-done:
		case <-ctx.Done():
			return wire.AggResultPayload{}, false
		case <-time.After(time.Until(deadline)):
			return wire.AggResultPayload{}, false
		}
	}
}

type pendingSignRequest struct {
	message []byte
	epoch   uint64
}

type quoteRegistrationSession struct {
	requestID        string
	ownerAddress     string
	pendingNonce     string
	quote            []byte
	quoteHash        []byte
	mrEnclave        string
	mrSigner         string
	hardwareID       string
	committeePubKey  string
	sigPubKey        string
	vrfPubKey        string
	enclavePK        string
	reportDataHash   string
	registerCertHash string
	challengeHash    string
	message          []byte
	votes            map[int]wire.QuoteVotePayload
	done             chan struct{}
	result           *TEERegistrationResult
	completed        bool
}

// TEERegistrationResult captures semantic result of register_tee workflow.
type TEERegistrationResult struct {
	Accepted       bool
	Reason         string
	TxHash         string
	RequestID      string
	OwnerAddress   string
	PendingNonce   string
	MrEnclave      string
	MrSigner       string
	HardwareID     string
	SigPubKey      string
	VrfPubKey      string
	EnclavePK      string
	ReportDataHash string
}

func NewService(cfg *config.NodeConfig) *Service {
	return NewServiceWithNetworkAndOptions(cfg, nil)
}

func NewServiceWithNetwork(cfg *config.NodeConfig, netw p2p.Network) *Service {
	return NewServiceWithNetworkAndOptions(cfg, netw)
}

func NewServiceWithNetworkAndOptions(cfg *config.NodeConfig, netw p2p.Network, opts ...Option) *Service {
	if netw == nil {
		netw = p2p.NewStaticNetwork(cfg.NodeID, cfg.ListenAddr, cfg.StaticNodeAddrs)
	}
	router := protocol.NewRouter()
	idx := indexOfNode(cfg.StaticNodes, cfg.NodeID)
	if idx < 0 {
		idx = 0
	}
	sk := derivePrivateKey(cfg.NodeID)
	bls, err := crypto.NewBN254BLSFromBigInt(sk, cfg.DomainSeparation)
	if err != nil {
		panic(err)
	}
	svc := &Service{
		cfg:                  cfg,
		net:                  netw,
		bls:                  bls,
		router:               router,
		quoteVerifier:        attestation.NoopVerifier{RejectEmptyQuote: true},
		chainGateway:         chain.NewMockGateway(),
		nodeIndex:            idx,
		autoSign:             1,
		dkgReady:             make(chan struct{}),
		shareKeys:            crypto.NewShareKeyCache(),
		sessions:             make(map[string]*signSession),
		pending:              make(map[string]pendingSignRequest),
		dkgShares:            make(map[int]*big.Int),
		dealerPubKeys:        make(map[int][]byte),
		aggResults:           make(map[string]wire.AggResultPayload),
		quoteSessions:        make(map[string]*quoteRegistrationSession),
		redactSessions:       make(map[string]*redactAuthorizationSession),
		teeRevocations:       make(map[string]*teeRevocationSession),
		activeSetCerts:       make(map[string]*activeSetCertSession),
		activeSetCertByEpoch: make(map[string]string),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	if svc.quoteVerifier == nil {
		svc.quoteVerifier = attestation.NoopVerifier{RejectEmptyQuote: true}
	}
	if svc.chainGateway == nil {
		svc.chainGateway = chain.NewMockGateway()
	}
	svc.registerEnvelopeHandlers()
	return svc
}

func (s *Service) Start(ctx context.Context) error {
	if err := s.net.Start(ctx); err != nil {
		return err
	}
	if err := s.startControlServer(ctx); err != nil {
		return err
	}
	log.Printf("committee node started node_id=%s threshold=%d static_peers=%d", s.cfg.NodeID, s.cfg.Threshold, len(s.cfg.StaticNodes))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-s.net.Subscribe():
				if !ok {
					return
				}
				s.handleEnvelope(msg)
			}
		}
	}()

	s.startDKG(ctx)
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if s.controlServer != nil {
		_ = s.controlServer.Shutdown(ctx)
	}
	if err := s.net.Stop(ctx); err != nil {
		return err
	}
	s.wg.Wait()
	return nil
}

func (s *Service) startControlServer(ctx context.Context) error {
	if s.cfg.ControlAddr == "" {
		return nil
	}
	server := &http.Server{
		Addr:              s.cfg.ControlAddr,
		Handler:           rpc.NewServer(newRPCAdapter(s)).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.controlServer = server
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("rpc server stopped err=%v", err)
		}
	}()
	return nil
}

func newSessionID(kind string, request []byte) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read session nonce: %w", err)
	}
	h := sha256.New()
	_, _ = h.Write([]byte("committee-session-id/v1"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(request)))
	_, _ = h.Write(n[:])
	_, _ = h.Write(request)
	_, _ = h.Write(nonce)
	return kind + "-" + hex.EncodeToString(h.Sum(nil)), nil
}

// startSignRequest triggers an internal threshold signing round.
func (s *Service) startSignRequest(message []byte) (string, error) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return "", fmt.Errorf("only coordinator can submit sign request")
	}
	if !s.isDKGReady() {
		return "", fmt.Errorf("committee key not ready yet")
	}
	payload, err := wire.EncodePayload(wire.SignRequestPayload{Message: message})
	if err != nil {
		return "", fmt.Errorf("encode sign request: %w", err)
	}
	sessionID, err := newSessionID("sign", payload)
	if err != nil {
		return "", err
	}
	nonce := atomic.AddUint64(&s.nonce, 1)
	s.mu.Lock()
	if _, exists := s.sessions[sessionID]; exists {
		s.mu.Unlock()
		return "", fmt.Errorf("session id already exists: %s", sessionID)
	}
	s.sessions[sessionID] = &signSession{
		message:      append([]byte(nil), message...),
		responses:    make(map[int]wire.SignResponsePayload),
		requestNonce: nonce,
		startedAt:    time.Now(),
	}
	s.mu.Unlock()
	// Anchor: coordinator starts broadcasting the request. The gap to
	// signer_received is the request leg, to threshold_reached is the full run.
	timing("sign_request_publish", sessionID)
	s.net.Publish(wire.Envelope{
		Type:      wire.MsgSignRequest,
		SessionID: sessionID,
		Epoch:     1,
		Nonce:     nonce,
		From:      s.cfg.NodeID,
		Version:   s.cfg.MessageVersion,
		Payload:   payload,
	})
	return sessionID, nil
}

func (s *Service) handleEnvelope(msg wire.Envelope) {
	if msg.Version != "" && msg.Version != s.cfg.MessageVersion {
		return
	}
	if msg.To != "" && msg.To != s.cfg.NodeID {
		return
	}
	s.router.Dispatch(msg)
}

func (s *Service) registerEnvelopeHandlers() {
	s.router.Register(wire.MsgDealShare, s.onDealShare)
	s.router.Register(wire.MsgSignRequest, s.onSignRequest)
	s.router.Register(wire.MsgSignResponse, s.onSignResponse)
	s.router.Register(wire.MsgAggResult, s.onAggResult)
	s.router.Register(wire.MsgQuoteRequest, s.onQuoteRequest)
	s.router.Register(wire.MsgQuoteVote, s.onQuoteVote)
}

func (s *Service) startDKG(ctx context.Context) {
	s.dkgOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			if err := s.broadcastDealerShares(); err != nil {
				log.Printf("dkg broadcast shares failed node_id=%s err=%v", s.cfg.NodeID, err)
			}
		}()
	})
}

func (s *Service) broadcastDealerShares() error {
	if s.nodeIndex < 0 || s.nodeIndex >= s.cfg.CommitteeSize {
		return fmt.Errorf("invalid node index %d", s.nodeIndex)
	}

	secret, err := rand.Int(rand.Reader, frCompat{}.Modulus())
	if err != nil {
		return fmt.Errorf("sample dealer secret: %w", err)
	}
	if secret.Sign() == 0 {
		secret.SetInt64(1)
	}
	dealerSigner, err := crypto.NewBN254BLSFromBigInt(secret, s.cfg.DomainSeparation)
	if err != nil {
		return fmt.Errorf("new dealer signer: %w", err)
	}
	dealerPub, err := dealerSigner.PublicKey()
	if err != nil {
		return fmt.Errorf("derive dealer public key: %w", err)
	}

	shares, err := share.Split(secret, s.cfg.Threshold, s.cfg.CommitteeSize)
	if err != nil {
		return fmt.Errorf("split dealer shares: %w", err)
	}

	for _, sh := range shares {
		recipientIdx := int(sh.Index) - 1
		if recipientIdx < 0 || recipientIdx >= len(s.cfg.StaticNodes) {
			continue
		}
		payload, err := wire.EncodePayload(wire.DealSharePayload{
			DealerIndex:    s.nodeIndex,
			RecipientIndex: recipientIdx,
			ShareValue:     sh.Value.String(),
			DealerPubKey:   dealerPub,
		})
		if err != nil {
			return fmt.Errorf("encode deal share: %w", err)
		}
		s.net.Publish(wire.Envelope{
			Type:      wire.MsgDealShare,
			SessionID: "dkg-1",
			Epoch:     1,
			Nonce:     atomic.AddUint64(&s.nonce, 1),
			From:      s.cfg.NodeID,
			To:        s.cfg.StaticNodes[recipientIdx],
			Version:   s.cfg.MessageVersion,
			Payload:   payload,
		})
	}
	return nil
}

func (s *Service) onDealShare(msg wire.Envelope) {
	var payload wire.DealSharePayload
	if err := wire.DecodePayload(msg.Payload, &payload); err != nil {
		log.Printf("decode deal-share failed from=%s err=%v", msg.From, err)
		return
	}
	if payload.RecipientIndex != s.nodeIndex {
		return
	}
	if payload.DealerIndex < 0 || payload.DealerIndex >= s.cfg.CommitteeSize {
		return
	}
	v, ok := new(big.Int).SetString(payload.ShareValue, 10)
	if !ok {
		log.Printf("invalid share value from=%s", msg.From)
		return
	}
	v.Mod(v, frCompat{}.Modulus())

	s.mu.Lock()
	if _, exists := s.dkgShares[payload.DealerIndex]; !exists {
		s.dkgShares[payload.DealerIndex] = v
	}
	if _, exists := s.dealerPubKeys[payload.DealerIndex]; !exists {
		s.dealerPubKeys[payload.DealerIndex] = append([]byte(nil), payload.DealerPubKey...)
	}
	ready := len(s.dkgShares) == s.cfg.CommitteeSize && len(s.dealerPubKeys) == s.cfg.CommitteeSize
	s.mu.Unlock()

	if ready {
		s.finalizeDKG()
	}
}

func (s *Service) finalizeDKG() {
	s.mu.Lock()
	if len(s.committeePubKey) > 0 {
		s.mu.Unlock()
		return
	}
	localShare := big.NewInt(0)
	mod := frCompat{}.Modulus()
	for _, v := range s.dkgShares {
		localShare.Add(localShare, v)
		localShare.Mod(localShare, mod)
	}

	pubs := make([][]byte, 0, len(s.dealerPubKeys))
	for i := 0; i < s.cfg.CommitteeSize; i++ {
		pk, ok := s.dealerPubKeys[i]
		if !ok {
			s.mu.Unlock()
			return
		}
		pubs = append(pubs, pk)
	}
	bitmap := make([]byte, (s.cfg.CommitteeSize+7)/8)
	for i := 0; i < s.cfg.CommitteeSize; i++ {
		bitmap[i/8] |= 1 << uint(i%8)
	}
	aggPub, err := crypto.AggregatePublicKeys(pubs, bitmap)
	if err != nil {
		s.mu.Unlock()
		log.Printf("dkg aggregate committee pubkey failed node_id=%s err=%v", s.cfg.NodeID, err)
		return
	}
	bls, err := crypto.NewBN254BLSFromBigInt(localShare, s.cfg.DomainSeparation)
	if err != nil {
		s.mu.Unlock()
		log.Printf("dkg create signer failed node_id=%s err=%v", s.cfg.NodeID, err)
		return
	}
	s.bls = bls
	s.committeePubKey = aggPub
	readyCh := s.dkgReady
	s.mu.Unlock()

	select {
	case <-readyCh:
	default:
		close(readyCh)
	}
	log.Printf("dkg completed node_id=%s committee_pub_ready=true", s.cfg.NodeID)
}

func (s *Service) isDKGReady() bool {
	select {
	case <-s.dkgReady:
		return true
	default:
		return false
	}
}

// CommitteePublicKey returns the fixed committee-level public key once DKG is complete.
func (s *Service) CommitteePublicKey() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.committeePubKey) == 0 {
		return nil, fmt.Errorf("committee key not ready yet")
	}
	out := make([]byte, len(s.committeePubKey))
	copy(out, s.committeePubKey)
	return out, nil
}

func (s *Service) storeAggregateResult(sessionID string, result wire.AggResultPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aggResults[sessionID] = wire.AggResultPayload{
		Bitmap:             append([]byte(nil), result.Bitmap...),
		AggregateSig:       append([]byte(nil), result.AggregateSig...),
		AggregatePublicKey: append([]byte(nil), result.AggregatePublicKey...),
		Message:            append([]byte(nil), result.Message...),
	}
	s.lastAggSessionID = sessionID
	// Closing under the same lock that publishes aggResults guarantees a waiter
	// woken by done always finds the result already visible.
	if state, ok := s.sessions[sessionID]; ok && state.done != nil {
		close(state.done)
		state.done = nil
	}
}

func (s *Service) onSignRequest(msg wire.Envelope) {
	timing("sign_request_received", msg.SessionID, "node", s.cfg.NodeID)
	if !s.isDKGReady() {
		log.Printf("ignore sign request before dkg ready session_id=%s node_id=%s", msg.SessionID, s.cfg.NodeID)
		return
	}
	var req wire.SignRequestPayload
	if err := wire.DecodePayload(msg.Payload, &req); err != nil {
		log.Printf("decode sign request failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	if len(req.Message) == 0 {
		log.Printf("empty sign request message session_id=%s", msg.SessionID)
		return
	}

	if s.cfg.NodeID == s.cfg.CoordinatorID {
		s.mu.Lock()
		if _, ok := s.sessions[msg.SessionID]; !ok {
			s.sessions[msg.SessionID] = &signSession{
				message:      append([]byte(nil), req.Message...),
				responses:    make(map[int]wire.SignResponsePayload),
				requestNonce: msg.Nonce,
			}
		}
		s.mu.Unlock()
	}

	if !s.isAutoSignEnabled() {
		s.mu.Lock()
		s.pending[msg.SessionID] = pendingSignRequest{
			message: append([]byte(nil), req.Message...),
			epoch:   msg.Epoch,
		}
		s.mu.Unlock()
		log.Printf("auto signing disabled, request queued session_id=%s node_id=%s", msg.SessionID, s.cfg.NodeID)
		return
	}

	if err := s.publishSignResponse(msg.SessionID, msg.Epoch, req.Message); err != nil {
		log.Printf("publish sign response failed session_id=%s node_id=%s err=%v", msg.SessionID, s.cfg.NodeID, err)
	}
}

func (s *Service) publishSignResponse(sessionID string, epoch uint64, message []byte) error {
	signStart := time.Now()
	shareSig, err := s.bls.SignShare(message)
	if err != nil {
		return fmt.Errorf("sign share failed: %w", err)
	}
	// Anchor: local share computation cost, and when the response leaves.
	timing("sign_share_computed", sessionID, "node", s.cfg.NodeID,
		"sign_ns", time.Since(signStart).Nanoseconds())
	pub, err := s.bls.PublicKey()
	if err != nil {
		return fmt.Errorf("derive pubkey failed: %w", err)
	}
	respPayload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    s.nodeIndex,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		return fmt.Errorf("encode sign response failed: %w", err)
	}
	s.net.Publish(wire.Envelope{
		Type:      wire.MsgSignResponse,
		SessionID: sessionID,
		Epoch:     epoch,
		Nonce:     atomic.AddUint64(&s.nonce, 1),
		From:      s.cfg.NodeID,
		To:        s.cfg.CoordinatorID,
		Version:   s.cfg.MessageVersion,
		Payload:   respPayload,
	})
	return nil
}

func (s *Service) setAutoSign(enabled bool) {
	if enabled {
		atomic.StoreUint32(&s.autoSign, 1)
		log.Printf("auto sign enabled node_id=%s", s.cfg.NodeID)
		return
	}
	atomic.StoreUint32(&s.autoSign, 0)
	log.Printf("auto sign disabled node_id=%s", s.cfg.NodeID)
}

// SetAutoSign updates the node's automatic signing mode for RPC/admin callers.
func (s *Service) SetAutoSign(enabled bool) {
	s.setAutoSign(enabled)
}

func (s *Service) isAutoSignEnabled() bool {
	return atomic.LoadUint32(&s.autoSign) == 1
}

func (s *Service) onSignResponse(msg wire.Envelope) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return
	}
	var resp wire.SignResponsePayload
	if err := wire.DecodePayload(msg.Payload, &resp); err != nil {
		log.Printf("decode sign response failed session_id=%s from=%s err=%v", msg.SessionID, msg.From, err)
		return
	}
	if resp.SignerIndex < 0 || resp.SignerIndex >= s.cfg.CommitteeSize {
		log.Printf("invalid signer index session_id=%s from=%s index=%d", msg.SessionID, msg.From, resp.SignerIndex)
		return
	}

	// Claim the signer index under the lock, then verify the share off-lock in
	// its own goroutine. VerifyShare is a BN254 multi-pairing (~3.4ms measured);
	// verifying 21 shares serially measured 68.9ms versus 24.9ms concurrent
	// (2.8x). The shares are independent, but they previously ran back-to-back
	// while holding s.mu and blocked the single envelope dispatch loop for the
	// whole aggregation.
	s.mu.Lock()
	state, ok := s.sessions[msg.SessionID]
	if !ok {
		s.mu.Unlock()
		log.Printf("unknown sign session response ignored session_id=%s from=%s", msg.SessionID, msg.From)
		return
	}
	if state.aggregated {
		s.mu.Unlock()
		return
	}
	if _, exists := state.responses[resp.SignerIndex]; exists {
		s.mu.Unlock()
		return
	}
	if state.verifying == nil {
		state.verifying = make(map[int]struct{})
	}
	if _, busy := state.verifying[resp.SignerIndex]; busy {
		s.mu.Unlock()
		return
	}
	state.verifying[resp.SignerIndex] = struct{}{}
	elapsed := time.Since(state.startedAt)
	s.mu.Unlock()
	timing("sign_response_received", msg.SessionID, "signer_index", resp.SignerIndex,
		"elapsed_ns", elapsed.Nanoseconds())

	// Shares are no longer verified one at a time here. Every share signs the
	// same message, so once the threshold is reached acceptSignResponse checks
	// them all with a single pairing (crypto.ShareBatch). Doing it per share
	// cost ~3.3ms each, all of it on the critical path.
	s.acceptSignResponse(msg.SessionID, msg.Epoch, state, resp)
}

// acceptSignResponse records an already-verified share and, when the share
// count reaches the threshold, aggregates and broadcasts the certificate.
// The threshold crossing is observed under s.mu so exactly one caller claims it.
func (s *Service) acceptSignResponse(sessionID string, epoch uint64, state *signSession, resp wire.SignResponsePayload) {
	s.mu.Lock()
	delete(state.verifying, resp.SignerIndex)
	if state.aggregated {
		s.mu.Unlock()
		return
	}
	state.responses[resp.SignerIndex] = resp
	if len(state.responses) < s.cfg.Threshold {
		collected := len(state.responses)
		s.mu.Unlock()
		log.Printf("signatures collected session_id=%s progress=%d/%d", sessionID, collected, s.cfg.Threshold)
		return
	}
	if state.aggregating {
		s.mu.Unlock()
		return
	}
	state.aggregating = true
	timing("threshold_reached", sessionID, "shares", len(state.responses),
		"elapsed_ns", time.Since(state.startedAt).Nanoseconds())

	indices := make([]int, 0, len(state.responses))
	for idx := range state.responses {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	bitmap := makeBitmap(indices, s.cfg.CommitteeSize)

	sigs := make([][]byte, 0, len(indices))
	pubs := make([][]byte, 0, len(indices))
	for _, idx := range indices {
		sigs = append(sigs, state.responses[idx].ShareSignature)
		pubs = append(pubs, state.responses[idx].SignerPubKey)
	}
	message := append([]byte(nil), state.message...)
	committeePub := append([]byte(nil), s.committeePubKey...)
	s.mu.Unlock()

	// One pairing for all shares instead of one per share; on failure fall back
	// to per-share verification purely to attribute the offending index.
	// Public keys come from the per-epoch cache, so they are decoded once
	// rather than re-unmarshalled on every session (G2.Unmarshal is ~0.6ms each
	// and accounted for 12.8ms of the 16.9ms batch).
	aggStart := time.Now()
	batch := crypto.NewShareBatch(message, []byte(s.cfg.DomainSeparation))
	for i, sig := range sigs {
		if addErr := batch.Add(sig); addErr != nil {
			s.releaseAggregation(state)
			log.Printf("batch add share failed session_id=%s err=%v", sessionID, addErr)
			return
		}
		key, keyErr := s.shareKeys.Get(indices[i], pubs[i])
		if keyErr != nil {
			s.releaseAggregation(state)
			log.Printf("decode share public key failed session_id=%s signer_index=%d err=%v", sessionID, indices[i], keyErr)
			return
		}
		batch.AddDecodedKey(key)
	}
	batchReady := time.Now()
	if err := batch.VerifyAll(nil); err != nil {
		idx, eachErr := batch.VerifyEach(nil)
		s.releaseAggregation(state)
		if eachErr != nil && idx >= 0 && idx < len(indices) {
			log.Printf("invalid share signature session_id=%s signer_index=%d err=%v", sessionID, indices[idx], eachErr)
			return
		}
		log.Printf("batch verify shares failed session_id=%s err=%v", sessionID, err)
		return
	}

	verifiedAt := time.Now()
	aggSig, err := crypto.AggregateThresholdSignatures(sigs, indices)
	if err != nil {
		s.releaseAggregation(state)
		log.Printf("aggregate signatures failed session_id=%s err=%v", sessionID, err)
		return
	}
	interpAt := time.Now()
	if err := s.bls.VerifyAggregate(committeePub, message, aggSig, bitmap); err != nil {
		s.releaseAggregation(state)
		log.Printf("aggregate verify failed session_id=%s err=%v", sessionID, err)
		return
	}
	resultPayload, err := wire.EncodePayload(wire.AggResultPayload{
		Bitmap:             bitmap,
		AggregateSig:       aggSig,
		AggregatePublicKey: committeePub,
		Message:            message,
	})
	if err != nil {
		s.releaseAggregation(state)
		log.Printf("encode aggregate result failed session_id=%s err=%v", sessionID, err)
		return
	}
	// Sub-anchors for the local tail: batch setup, share verification,
	// Lagrange interpolation, and certificate verification.
	timing("agg_substage", sessionID,
		"batch_add_ns", batchReady.Sub(aggStart).Nanoseconds(),
		"verify_ns", verifiedAt.Sub(batchReady).Nanoseconds(),
		"interpolate_ns", interpAt.Sub(verifiedAt).Nanoseconds(),
		"certverify_ns", time.Since(interpAt).Nanoseconds(),
		"total_ns", time.Since(aggStart).Nanoseconds())
	timing("aggregate_ready", sessionID,
		"elapsed_ns", time.Since(state.startedAt).Nanoseconds())
	s.mu.Lock()
	state.aggregated = true
	s.mu.Unlock()
	s.storeAggregateResult(sessionID, wire.AggResultPayload{
		Bitmap:             bitmap,
		AggregateSig:       aggSig,
		AggregatePublicKey: committeePub,
		Message:            message,
	})
	timing("aggregate_broadcast", sessionID,
		"elapsed_ns", time.Since(state.startedAt).Nanoseconds())
	s.net.Publish(wire.Envelope{
		Type:      wire.MsgAggResult,
		SessionID: sessionID,
		Epoch:     epoch,
		Nonce:     atomic.AddUint64(&s.nonce, 1),
		From:      s.cfg.NodeID,
		Version:   s.cfg.MessageVersion,
		Payload:   resultPayload,
	})
	log.Printf("aggregate result broadcast session_id=%s signers=%d threshold=%d", sessionID, len(indices), s.cfg.Threshold)
}

// releaseAggregation clears the in-progress claim so a later share can retry
// aggregation, preserving the behaviour before the claim existed.
func (s *Service) releaseAggregation(state *signSession) {
	s.mu.Lock()
	state.aggregating = false
	s.mu.Unlock()
}

func (s *Service) onAggResult(msg wire.Envelope) {
	var result wire.AggResultPayload
	if err := wire.DecodePayload(msg.Payload, &result); err != nil {
		log.Printf("decode aggregate result failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	if err := s.bls.VerifyAggregate(result.AggregatePublicKey, result.Message, result.AggregateSig, result.Bitmap); err != nil {
		log.Printf("aggregate result verify failed session_id=%s from=%s err=%v", msg.SessionID, msg.From, err)
		return
	}
	s.storeAggregateResult(msg.SessionID, result)
	log.Printf("aggregate result accepted session_id=%s from=%s", msg.SessionID, msg.From)
}

// RegisterTEE runs quote validation across committee nodes and submits aggregated registration.
func (s *Service) RegisterTEE(ctx context.Context, requestID string, ownerAddress string, quote []byte, reportData rpc.TEEReportData) (*TEERegistrationResult, error) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return nil, fmt.Errorf("only coordinator can register tee")
	}
	if !s.isDKGReady() {
		return nil, fmt.Errorf("committee key not ready yet")
	}
	normalizedOwner, err := normalizeOwnerAddress(ownerAddress)
	if err != nil {
		return nil, err
	}
	if len(quote) == 0 {
		return nil, fmt.Errorf("quote is required")
	}
	quoteHash := sha256.Sum256(quote)
	if strings.TrimSpace(requestID) == "" {
		requestID, err = newSessionID("tee", append([]byte(normalizedOwner), quoteHash[:]...))
		if err != nil {
			return nil, err
		}
	}

	vote := s.buildQuoteVote(ctx, wire.QuoteRequestPayload{
		RequestID:       requestID,
		OwnerAddress:    normalizedOwner,
		Quote:           append([]byte(nil), quote...),
		HardwareID:      reportData.HardwareID,
		CommitteePubKey: reportData.CommitteePubKey,
		SigPubKey:       reportData.SigPubKey,
		VrfPubKey:       reportData.VrfPubKey,
		EnclavePK:       reportData.EnclavePK,
	})
	if !vote.Accepted {
		return &TEERegistrationResult{
			Accepted:       false,
			Reason:         vote.Reason,
			RequestID:      requestID,
			OwnerAddress:   normalizedOwner,
			PendingNonce:   vote.PendingNonce,
			MrEnclave:      vote.MrEnclave,
			MrSigner:       vote.MrSigner,
			HardwareID:     vote.HardwareID,
			SigPubKey:      vote.SigPubKey,
			VrfPubKey:      vote.VrfPubKey,
			EnclavePK:      vote.EnclavePK,
			ReportDataHash: vote.ReportDataHash,
		}, nil
	}

	vote.RequestID = requestID
	vote.QuoteHash = append([]byte(nil), quoteHash[:]...)
	vote.Signature = append([]byte(nil), vote.Signature...)
	vote.SignerPubKey = append([]byte(nil), vote.SignerPubKey...)

	reqPayload := wire.QuoteRequestPayload{
		RequestID:        requestID,
		OwnerAddress:     normalizedOwner,
		PendingNonce:     vote.PendingNonce,
		Quote:            append([]byte(nil), quote...),
		QuoteHash:        append([]byte(nil), quoteHash[:]...),
		MrEnclave:        vote.MrEnclave,
		MrSigner:         vote.MrSigner,
		HardwareID:       vote.HardwareID,
		CommitteePubKey:  vote.CommitteePubKey,
		SigPubKey:        vote.SigPubKey,
		VrfPubKey:        vote.VrfPubKey,
		EnclavePK:        vote.EnclavePK,
		ReportDataHash:   vote.ReportDataHash,
		RegisterCertHash: vote.RegisterCertHash,
		ChallengeHash:    vote.ChallengeHash,
		MessageHash:      append([]byte(nil), vote.MessageHash...),
	}

	session := &quoteRegistrationSession{
		requestID:        requestID,
		ownerAddress:     normalizedOwner,
		pendingNonce:     vote.PendingNonce,
		quote:            append([]byte(nil), quote...),
		quoteHash:        append([]byte(nil), quoteHash[:]...),
		mrEnclave:        normalizeClaim(vote.MrEnclave),
		mrSigner:         normalizeClaim(vote.MrSigner),
		hardwareID:       normalizeClaim(vote.HardwareID),
		committeePubKey:  normalizeClaim(vote.CommitteePubKey),
		sigPubKey:        normalizeClaim(vote.SigPubKey),
		vrfPubKey:        normalizeClaim(vote.VrfPubKey),
		enclavePK:        normalizeClaim(vote.EnclavePK),
		reportDataHash:   normalizeClaim(vote.ReportDataHash),
		registerCertHash: normalizeClaim(vote.RegisterCertHash),
		challengeHash:    normalizeClaim(vote.ChallengeHash),
		message:          append([]byte(nil), vote.MessageHash...),
		votes:            map[int]wire.QuoteVotePayload{s.nodeIndex: vote},
		done:             make(chan struct{}),
	}

	s.mu.Lock()
	if _, exists := s.quoteSessions[requestID]; exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("quote session id already exists: %s", requestID)
	}
	s.quoteSessions[requestID] = session
	s.mu.Unlock()

	payload, err := wire.EncodePayload(reqPayload)
	if err != nil {
		s.mu.Lock()
		delete(s.quoteSessions, requestID)
		s.mu.Unlock()
		return nil, fmt.Errorf("encode quote request: %w", err)
	}

	s.net.Publish(wire.Envelope{
		Type:      wire.MsgQuoteRequest,
		SessionID: requestID,
		RequestID: requestID,
		Epoch:     1,
		Nonce:     atomic.AddUint64(&s.nonce, 1),
		From:      s.cfg.NodeID,
		Version:   s.cfg.MessageVersion,
		Payload:   payload,
	})

	s.tryFinalizeQuoteSession(requestID)

	select {
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.quoteSessions, requestID)
		s.mu.Unlock()
		return nil, ctx.Err()
	case <-session.done:
	case <-time.After(15 * time.Second):
		s.mu.Lock()
		delete(s.quoteSessions, requestID)
		s.mu.Unlock()
		return &TEERegistrationResult{
			Accepted:       false,
			Reason:         "quote voting timed out",
			RequestID:      requestID,
			OwnerAddress:   normalizedOwner,
			PendingNonce:   vote.PendingNonce,
			MrEnclave:      vote.MrEnclave,
			MrSigner:       vote.MrSigner,
			HardwareID:     vote.HardwareID,
			SigPubKey:      vote.SigPubKey,
			VrfPubKey:      vote.VrfPubKey,
			EnclavePK:      vote.EnclavePK,
			ReportDataHash: vote.ReportDataHash,
		}, nil
	}

	s.mu.Lock()
	result := session.result
	delete(s.quoteSessions, requestID)
	s.mu.Unlock()
	if result == nil {
		return nil, fmt.Errorf("quote registration failed: no result")
	}
	return result, nil
}

func (s *Service) onQuoteRequest(msg wire.Envelope) {
	if !s.isDKGReady() {
		log.Printf("ignore quote request before dkg ready session_id=%s node_id=%s", msg.SessionID, s.cfg.NodeID)
		return
	}
	if s.cfg.NodeID == s.cfg.CoordinatorID {
		return
	}

	var req wire.QuoteRequestPayload
	if err := wire.DecodePayload(msg.Payload, &req); err != nil {
		log.Printf("decode quote request failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		req.RequestID = msg.RequestID
	}
	if req.RequestID == "" {
		req.RequestID = msg.SessionID
	}

	vote := s.buildQuoteVote(context.Background(), req)
	vote.RequestID = req.RequestID

	payload, err := wire.EncodePayload(vote)
	if err != nil {
		log.Printf("encode quote vote failed request_id=%s node_id=%s err=%v", req.RequestID, s.cfg.NodeID, err)
		return
	}
	s.net.Publish(wire.Envelope{
		Type:      wire.MsgQuoteVote,
		SessionID: req.RequestID,
		RequestID: req.RequestID,
		Epoch:     msg.Epoch,
		Nonce:     atomic.AddUint64(&s.nonce, 1),
		From:      s.cfg.NodeID,
		To:        s.cfg.CoordinatorID,
		Version:   s.cfg.MessageVersion,
		Payload:   payload,
	})
}

func (s *Service) onQuoteVote(msg wire.Envelope) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return
	}

	var vote wire.QuoteVotePayload
	if err := wire.DecodePayload(msg.Payload, &vote); err != nil {
		log.Printf("decode quote vote failed session_id=%s from=%s err=%v", msg.SessionID, msg.From, err)
		return
	}

	s.mu.Lock()
	session, ok := s.quoteSessions[msg.SessionID]
	if !ok {
		s.mu.Unlock()
		return
	}
	if session.completed {
		s.mu.Unlock()
		return
	}
	if vote.SignerIndex < 0 || vote.SignerIndex >= s.cfg.CommitteeSize {
		s.mu.Unlock()
		return
	}
	if _, exists := session.votes[vote.SignerIndex]; exists {
		s.mu.Unlock()
		return
	}

	if vote.Accepted {
		if !bytes.Equal(vote.QuoteHash, session.quoteHash) {
			s.mu.Unlock()
			return
		}
		if normalizeOwnerAddressLoose(vote.OwnerAddress) != session.ownerAddress {
			s.mu.Unlock()
			return
		}
		if strings.TrimSpace(vote.PendingNonce) != session.pendingNonce {
			s.mu.Unlock()
			return
		}
		if normalizeClaim(vote.MrEnclave) != session.mrEnclave || normalizeClaim(vote.MrSigner) != session.mrSigner {
			s.mu.Unlock()
			return
		}
		if normalizeClaim(vote.HardwareID) != session.hardwareID {
			s.mu.Unlock()
			return
		}
		if normalizeClaim(vote.CommitteePubKey) != session.committeePubKey || normalizeClaim(vote.ReportDataHash) != session.reportDataHash {
			s.mu.Unlock()
			return
		}
		if normalizeClaim(vote.SigPubKey) != session.sigPubKey || normalizeClaim(vote.VrfPubKey) != session.vrfPubKey || normalizeClaim(vote.EnclavePK) != session.enclavePK {
			s.mu.Unlock()
			return
		}
		if normalizeClaim(vote.RegisterCertHash) != session.registerCertHash || normalizeClaim(vote.ChallengeHash) != session.challengeHash {
			s.mu.Unlock()
			return
		}
		if !bytes.Equal(vote.MessageHash, session.message) {
			s.mu.Unlock()
			return
		}
		if err := s.bls.VerifyShare(vote.SignerPubKey, session.message, vote.Signature); err != nil {
			s.mu.Unlock()
			log.Printf("invalid quote vote signature request_id=%s signer_index=%d err=%v", session.requestID, vote.SignerIndex, err)
			return
		}
	}

	session.votes[vote.SignerIndex] = vote
	s.mu.Unlock()

	s.tryFinalizeQuoteSession(msg.SessionID)
}

func (s *Service) tryFinalizeQuoteSession(sessionID string) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return
	}

	s.mu.Lock()
	session, ok := s.quoteSessions[sessionID]
	if !ok || session.completed {
		s.mu.Unlock()
		return
	}

	acceptedVotes := make([]wire.QuoteVotePayload, 0, len(session.votes))
	acceptedIndices := make([]int, 0, len(session.votes))
	for idx, vote := range session.votes {
		if vote.Accepted {
			acceptedVotes = append(acceptedVotes, vote)
			acceptedIndices = append(acceptedIndices, idx)
		}
	}

	if len(acceptedVotes) < s.cfg.Threshold && len(session.votes) < s.cfg.CommitteeSize {
		s.mu.Unlock()
		return
	}

	session.completed = true
	requestID := session.requestID
	ownerAddress := session.ownerAddress
	pendingNonce := session.pendingNonce
	quoteHash := append([]byte(nil), session.quoteHash...)
	mrEnclave := session.mrEnclave
	mrSigner := session.mrSigner
	hardwareID := session.hardwareID
	sigPubKey := session.sigPubKey
	vrfPubKey := session.vrfPubKey
	enclavePK := session.enclavePK
	reportDataHash := session.reportDataHash
	registerCertHash := session.registerCertHash
	challengeHash := session.challengeHash
	message := append([]byte(nil), session.message...)
	committeePub := append([]byte(nil), s.committeePubKey...)
	s.mu.Unlock()

	if len(acceptedVotes) < s.cfg.Threshold {
		s.finishQuoteSession(sessionID, &TEERegistrationResult{
			Accepted:       false,
			Reason:         "not enough accepted votes",
			RequestID:      requestID,
			OwnerAddress:   ownerAddress,
			PendingNonce:   pendingNonce,
			MrEnclave:      with0x(mrEnclave),
			MrSigner:       with0x(mrSigner),
			HardwareID:     with0x(hardwareID),
			SigPubKey:      with0x(sigPubKey),
			VrfPubKey:      with0x(vrfPubKey),
			EnclavePK:      with0x(enclavePK),
			ReportDataHash: with0x(reportDataHash),
		})
		return
	}

	sort.Ints(acceptedIndices)
	sigs := make([][]byte, 0, len(acceptedIndices))
	voteByIndex := make(map[int]wire.QuoteVotePayload, len(acceptedVotes))
	for _, vote := range acceptedVotes {
		voteByIndex[vote.SignerIndex] = vote
	}
	for _, idx := range acceptedIndices {
		sigs = append(sigs, voteByIndex[idx].Signature)
	}
	bitmap := makeBitmap(acceptedIndices, s.cfg.CommitteeSize)

	aggSig, err := crypto.AggregateThresholdSignatures(sigs, acceptedIndices)
	if err != nil {
		s.finishQuoteSession(sessionID, &TEERegistrationResult{
			Accepted:       false,
			Reason:         fmt.Sprintf("aggregate quote signatures failed: %v", err),
			RequestID:      requestID,
			OwnerAddress:   ownerAddress,
			PendingNonce:   pendingNonce,
			MrEnclave:      with0x(mrEnclave),
			MrSigner:       with0x(mrSigner),
			HardwareID:     with0x(hardwareID),
			SigPubKey:      with0x(sigPubKey),
			VrfPubKey:      with0x(vrfPubKey),
			EnclavePK:      with0x(enclavePK),
			ReportDataHash: with0x(reportDataHash),
		})
		return
	}
	if err := s.bls.VerifyAggregate(committeePub, message, aggSig, bitmap); err != nil {
		s.finishQuoteSession(sessionID, &TEERegistrationResult{
			Accepted:       false,
			Reason:         fmt.Sprintf("aggregate quote verify failed: %v", err),
			RequestID:      requestID,
			OwnerAddress:   ownerAddress,
			PendingNonce:   pendingNonce,
			MrEnclave:      with0x(mrEnclave),
			MrSigner:       with0x(mrSigner),
			HardwareID:     with0x(hardwareID),
			SigPubKey:      with0x(sigPubKey),
			VrfPubKey:      with0x(vrfPubKey),
			EnclavePK:      with0x(enclavePK),
			ReportDataHash: with0x(reportDataHash),
		})
		return
	}

	pendingNonceInt, ok := new(big.Int).SetString(strings.TrimSpace(pendingNonce), 10)
	if !ok {
		s.finishQuoteSession(sessionID, &TEERegistrationResult{
			Accepted:       false,
			Reason:         "invalid pending nonce in quote session",
			RequestID:      requestID,
			OwnerAddress:   ownerAddress,
			PendingNonce:   pendingNonce,
			MrEnclave:      with0x(mrEnclave),
			MrSigner:       with0x(mrSigner),
			HardwareID:     with0x(hardwareID),
			SigPubKey:      with0x(sigPubKey),
			VrfPubKey:      with0x(vrfPubKey),
			EnclavePK:      with0x(enclavePK),
			ReportDataHash: with0x(reportDataHash),
		})
		return
	}

	txHash, err := s.chainGateway.SubmitTEERegistration(context.Background(), chain.RegistrationInput{
		RequestID:          requestID,
		OwnerAddress:       ownerAddress,
		PendingNonce:       pendingNonceInt,
		MREnclave:          with0x(mrEnclave),
		MRSigner:           with0x(mrSigner),
		HardwareID:         with0x(hardwareID),
		SigPubKey:          with0x(sigPubKey),
		VrfPubKey:          with0x(vrfPubKey),
		EnclavePK:          with0x(enclavePK),
		RegisterCertHash:   with0x(registerCertHash),
		ChallengeHash:      with0x(challengeHash),
		QuoteHash:          quoteHash,
		MessageHash:        message,
		AggregateSignature: aggSig,
		AggregatePublicKey: committeePub,
	})
	if err != nil {
		s.finishQuoteSession(sessionID, &TEERegistrationResult{
			Accepted:       false,
			Reason:         fmt.Sprintf("submit tee registration failed: %v", err),
			RequestID:      requestID,
			OwnerAddress:   ownerAddress,
			PendingNonce:   pendingNonce,
			MrEnclave:      with0x(mrEnclave),
			MrSigner:       with0x(mrSigner),
			HardwareID:     with0x(hardwareID),
			SigPubKey:      with0x(sigPubKey),
			VrfPubKey:      with0x(vrfPubKey),
			EnclavePK:      with0x(enclavePK),
			ReportDataHash: with0x(reportDataHash),
		})
		return
	}

	s.finishQuoteSession(sessionID, &TEERegistrationResult{
		Accepted:       true,
		Reason:         "registration accepted",
		TxHash:         txHash,
		RequestID:      requestID,
		OwnerAddress:   ownerAddress,
		PendingNonce:   pendingNonce,
		MrEnclave:      with0x(mrEnclave),
		MrSigner:       with0x(mrSigner),
		HardwareID:     with0x(hardwareID),
		SigPubKey:      with0x(sigPubKey),
		VrfPubKey:      with0x(vrfPubKey),
		EnclavePK:      with0x(enclavePK),
		ReportDataHash: with0x(reportDataHash),
	})
}

func (s *Service) finishQuoteSession(sessionID string, result *TEERegistrationResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.quoteSessions[sessionID]
	if !ok {
		return
	}
	if session.result == nil {
		session.result = result
	}
	if session.completed {
		select {
		case <-session.done:
		default:
			close(session.done)
		}
	}
}

func (s *Service) buildQuoteVote(ctx context.Context, req wire.QuoteRequestPayload) wire.QuoteVotePayload {
	vote := wire.QuoteVotePayload{SignerIndex: s.nodeIndex, Accepted: false}
	normalizedOwner, err := normalizeOwnerAddress(req.OwnerAddress)
	if err != nil {
		vote.Reason = err.Error()
		return vote
	}
	vote.OwnerAddress = normalizedOwner
	if len(req.Quote) == 0 {
		vote.Reason = "quote is required"
		return vote
	}

	computedHash := sha256.Sum256(req.Quote)
	quoteHash := computedHash[:]
	if len(req.QuoteHash) > 0 {
		if !bytes.Equal(req.QuoteHash, quoteHash) {
			vote.Reason = "quote hash mismatch"
			return vote
		}
		quoteHash = req.QuoteHash
	}

	verified, err := s.VerifyQuote(ctx, req.Quote, attestation.Policy{})
	if err != nil {
		vote.Reason = err.Error()
		return vote
	}
	if !verified.Accepted {
		vote.Reason = verified.Reason
		if vote.Reason == "" {
			vote.Reason = "quote rejected"
		}
		return vote
	}

	measurements, err := attestation.ParseSGXQuoteMeasurements(req.Quote)
	if err != nil {
		vote.Reason = fmt.Sprintf("parse sgx quote failed: %v", err)
		return vote
	}

	parsedMrEnclave := normalizeClaim(measurements.MREnclave)
	parsedMrSigner := normalizeClaim(measurements.MRSigner)
	parsedReportDataHash := normalizeClaim(measurements.ReportDataHash)
	reportTail := normalizeClaim(measurements.ReportData)
	if len(reportTail) == 128 && reportTail[64:] != strings.Repeat("0", 64) {
		vote.Reason = "quote report_data padding must be zero"
		return vote
	}
	reportDataHash, parsedHardwareID, parsedCommitteePubKey, parsedSigPubKey, parsedVrfPubKey, parsedEnclavePK, err := computeTEEReportDataHash(req.HardwareID, req.CommitteePubKey, req.SigPubKey, req.VrfPubKey, req.EnclavePK)
	if err != nil {
		vote.Reason = err.Error()
		return vote
	}
	reportDataHashHex := hex.EncodeToString(reportDataHash)
	if parsedReportDataHash != reportDataHashHex {
		vote.Reason = "report_data hash mismatch"
		return vote
	}
	committeePub, err := s.CommitteePublicKey()
	if err != nil {
		vote.Reason = err.Error()
		return vote
	}
	if parsedCommitteePubKey != hex.EncodeToString(committeePub) {
		vote.Reason = "committee public key mismatch"
		return vote
	}

	mrEnclave := parsedMrEnclave
	mrSigner := parsedMrSigner
	if claim := normalizeClaim(claimValue(verified.Claims, "mrenclave")); claim != "" {
		if parsedMrEnclave != "" && parsedMrEnclave != claim {
			vote.Reason = "mrenclave mismatch between verifier and quote"
			return vote
		}
		mrEnclave = claim
	}
	if claim := normalizeClaim(claimValue(verified.Claims, "mrsigner")); claim != "" {
		if parsedMrSigner != "" && parsedMrSigner != claim {
			vote.Reason = "mrsigner mismatch between verifier and quote"
			return vote
		}
		mrSigner = claim
	}

	if req.MrEnclave != "" && mrEnclave != normalizeClaim(req.MrEnclave) {
		vote.Reason = "mrenclave mismatch"
		return vote
	}
	if req.MrSigner != "" && mrSigner != normalizeClaim(req.MrSigner) {
		vote.Reason = "mrsigner mismatch"
		return vote
	}
	if !isAllowedClaim(mrEnclave, s.cfg.AllowedMREnclaves) {
		vote.Reason = "mrenclave not allowed by local policy"
		return vote
	}
	if !isAllowedClaim(mrSigner, s.cfg.AllowedMRSigners) {
		vote.Reason = "mrsigner not allowed by local policy"
		return vote
	}

	if parsedHardwareID == "" || parsedHardwareID == strings.Repeat("0", 64) {
		vote.Reason = "report_data.hardware_id is required"
		return vote
	}
	if parsedSigPubKey == "" || parsedSigPubKey == strings.Repeat("0", 64) {
		vote.Reason = "report_data.sig_pub_key is required"
		return vote
	}
	if parsedVrfPubKey == "" || parsedVrfPubKey == strings.Repeat("0", 64) {
		vote.Reason = "report_data.vrf_pub_key is required"
		return vote
	}
	if parsedEnclavePK == "" || parsedEnclavePK == strings.Repeat("0", 128) {
		vote.Reason = "report_data.enclave_pk is required"
		return vote
	}
	if !bytes32ClaimFitsUint160(parsedSigPubKey) {
		vote.Reason = "signature public key must be address"
		return vote
	}
	if !bytes32ClaimFitsUint160(parsedVrfPubKey) {
		vote.Reason = "vrf public key must be address"
		return vote
	}

	pending, err := s.chainGateway.GetPendingTEE(ctx, normalizedOwner)
	if err != nil {
		vote.Reason = fmt.Sprintf("query pending tee failed: %v", err)
		return vote
	}
	if pending == nil || !pending.Exists {
		status, statusErr := s.chainGateway.GetTEEStatusByOwner(ctx, normalizedOwner)
		if statusErr == nil && status != nil && status.Status != 0 {
			vote.Reason = fmt.Sprintf("owner status %d is not pending", status.Status)
			return vote
		}
		vote.Reason = "owner has no pending tee"
		return vote
	}

	pendingNonce := pending.Nonce
	if pendingNonce == nil {
		vote.Reason = "pending tee nonce is missing"
		return vote
	}
	pendingChallengeHash := normalizeClaim(pending.ChallengeHash)
	if pendingChallengeHash == "" || pendingChallengeHash == strings.Repeat("0", 64) {
		vote.Reason = "pending tee challenge hash is missing"
		return vote
	}
	if strings.TrimSpace(req.PendingNonce) != "" && strings.TrimSpace(req.PendingNonce) != pendingNonce.String() {
		vote.Reason = "pending nonce mismatch"
		return vote
	}
	if req.ChallengeHash != "" && pendingChallengeHash != normalizeClaim(req.ChallengeHash) {
		vote.Reason = "challenge hash mismatch"
		return vote
	}

	registerCertHash, err := buildRegisterCertHash(normalizedOwner, pendingNonce, mrEnclave, mrSigner, parsedHardwareID, parsedSigPubKey, parsedVrfPubKey, quoteHash, pendingChallengeHash)
	if err != nil {
		vote.Reason = fmt.Sprintf("build register cert hash failed: %v", err)
		return vote
	}
	registerCertHashHex := hex.EncodeToString(registerCertHash)
	if req.RegisterCertHash != "" && registerCertHashHex != normalizeClaim(req.RegisterCertHash) {
		vote.Reason = "register cert hash mismatch"
		return vote
	}

	message, err := buildRegisterTEEMessageHash(normalizedOwner, pendingNonce, mrEnclave, parsedHardwareID, parsedSigPubKey, parsedVrfPubKey, parsedEnclavePK, mrSigner, quoteHash, registerCertHash, pendingChallengeHash)
	if err != nil {
		vote.Reason = fmt.Sprintf("build register tee message failed: %v", err)
		return vote
	}
	if len(req.MessageHash) > 0 && !bytes.Equal(req.MessageHash, message) {
		vote.Reason = "message hash mismatch"
		return vote
	}

	shareSig, err := s.bls.SignShare(message)
	if err != nil {
		vote.Reason = fmt.Sprintf("sign quote vote failed: %v", err)
		return vote
	}
	pub, err := s.bls.PublicKey()
	if err != nil {
		vote.Reason = fmt.Sprintf("derive pubkey failed: %v", err)
		return vote
	}

	vote.Accepted = true
	vote.Reason = "accepted"
	vote.OwnerAddress = normalizedOwner
	vote.PendingNonce = pendingNonce.String()
	vote.QuoteHash = append([]byte(nil), quoteHash...)
	vote.MrEnclave = mrEnclave
	vote.MrSigner = mrSigner
	vote.HardwareID = parsedHardwareID
	vote.CommitteePubKey = parsedCommitteePubKey
	vote.SigPubKey = parsedSigPubKey
	vote.VrfPubKey = parsedVrfPubKey
	vote.EnclavePK = parsedEnclavePK
	vote.ReportDataHash = reportDataHashHex
	vote.RegisterCertHash = registerCertHashHex
	vote.ChallengeHash = pendingChallengeHash
	vote.MessageHash = append([]byte(nil), message...)
	vote.Signature = shareSig
	vote.SignerPubKey = pub
	return vote
}

func normalizeClaim(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
		v = v[2:]
	}
	return strings.ToLower(v)
}

func computeTEEReportDataHash(hardwareID, committeePubKey, sigPubKey, vrfPubKey, enclavePK string) ([]byte, string, string, string, string, string, error) {
	hardwareRaw, normalizedHardwareID, err := decodeFixedHexField("report_data.hardware_id", hardwareID, 32)
	if err != nil {
		return nil, "", "", "", "", "", err
	}
	committeeRaw, normalizedCommitteePubKey, err := decodeFixedHexField("report_data.committee_pub_key", committeePubKey, 128)
	if err != nil {
		return nil, "", "", "", "", "", err
	}
	sigRaw, normalizedSigPubKey, err := decodeFixedHexField("report_data.sig_pub_key", sigPubKey, 32)
	if err != nil {
		return nil, "", "", "", "", "", err
	}
	vrfRaw, normalizedVrfPubKey, err := decodeFixedHexField("report_data.vrf_pub_key", vrfPubKey, 32)
	if err != nil {
		return nil, "", "", "", "", "", err
	}
	enclavePKRaw, normalizedEnclavePK, err := decodeFixedHexField("report_data.enclave_pk", enclavePK, 64)
	if err != nil {
		return nil, "", "", "", "", "", err
	}
	payload := make([]byte, 0, 288)
	payload = append(payload, hardwareRaw...)
	payload = append(payload, committeeRaw...)
	payload = append(payload, sigRaw...)
	payload = append(payload, vrfRaw...)
	payload = append(payload, enclavePKRaw...)
	sum := sha256.Sum256(payload)
	return sum[:], normalizedHardwareID, normalizedCommitteePubKey, normalizedSigPubKey, normalizedVrfPubKey, normalizedEnclavePK, nil
}

func decodeFixedHexField(name string, value string, wantBytes int) ([]byte, string, error) {
	normalized := normalizeClaim(value)
	if normalized == "" {
		return nil, "", fmt.Errorf("%s is required", name)
	}
	if len(normalized) != wantBytes*2 {
		return nil, "", fmt.Errorf("%s must be %d bytes", name, wantBytes)
	}
	raw, err := hex.DecodeString(normalized)
	if err != nil {
		return nil, "", fmt.Errorf("%s must be hex: %w", name, err)
	}
	return raw, normalized, nil
}

func bytes32ClaimFitsUint160(v string) bool {
	claim := normalizeClaim(v)
	if len(claim) != 64 {
		return false
	}
	return claim[:24] == strings.Repeat("0", 24)
}

func with0x(v string) string {
	v = normalizeClaim(v)
	if v == "" {
		return ""
	}
	return "0x" + v
}

func normalizeOwnerAddress(owner string) (string, error) {
	owner = strings.TrimSpace(owner)
	if !ethcommon.IsHexAddress(owner) {
		return "", fmt.Errorf("invalid owner address")
	}
	return strings.ToLower(ethcommon.HexToAddress(owner).Hex()), nil
}

func normalizeOwnerAddressLoose(owner string) string {
	normalized, err := normalizeOwnerAddress(owner)
	if err != nil {
		return ""
	}
	return normalized
}

func bytes32FromHex(value string) ([32]byte, error) {
	var out [32]byte
	value = normalizeClaim(value)
	if len(value) != 64 {
		return out, fmt.Errorf("invalid bytes32 hex length: got %d want 64", len(value))
	}
	raw, err := hex.DecodeString(value)
	if err != nil {
		return out, fmt.Errorf("decode bytes32 hex: %w", err)
	}
	copy(out[:], raw)
	return out, nil
}

func bytes32FromBytes(value []byte) ([32]byte, error) {
	var out [32]byte
	if len(value) != 32 {
		return out, fmt.Errorf("invalid bytes32 length: got %d want 32", len(value))
	}
	copy(out[:], value)
	return out, nil
}

func bytes32PairFromHex(value string) ([2][32]byte, error) {
	var out [2][32]byte
	normalized := normalizeClaim(value)
	if len(normalized) != 128 {
		return out, fmt.Errorf("invalid bytes32[2] length: got %d want 128 hex chars", len(normalized))
	}
	raw, err := hex.DecodeString(normalized)
	if err != nil {
		return out, err
	}
	copy(out[0][:], raw[:32])
	copy(out[1][:], raw[32:])
	return out, nil
}

func buildRegisterTEEMessageHash(owner string, pendingNonce *big.Int, mrEnclave string, hardwareID string, sigPubKey string, vrfPubKey string, enclavePK string, mrSigner string, quoteHash []byte, registerCertHash []byte, challengeHash string) ([]byte, error) {
	if pendingNonce == nil {
		return nil, fmt.Errorf("pending nonce is required")
	}
	normalizedOwner, err := normalizeOwnerAddress(owner)
	if err != nil {
		return nil, err
	}
	mrEnclaveBytes, err := bytes32FromHex(mrEnclave)
	if err != nil {
		return nil, fmt.Errorf("invalid mr_enclave: %w", err)
	}
	hardwareIDBytes, err := bytes32FromHex(hardwareID)
	if err != nil {
		return nil, fmt.Errorf("invalid hardware id: %w", err)
	}
	sigPubKeyBytes, err := bytes32FromHex(sigPubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid signature public key: %w", err)
	}
	vrfPubKeyBytes, err := bytes32FromHex(vrfPubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid vrf public key: %w", err)
	}
	enclavePKParts, err := bytes32PairFromHex(enclavePK)
	if err != nil {
		return nil, fmt.Errorf("invalid enclave public key: %w", err)
	}
	mrSignerBytes, err := bytes32FromHex(mrSigner)
	if err != nil {
		return nil, fmt.Errorf("invalid mr_signer: %w", err)
	}
	quoteHashBytes, err := bytes32FromBytes(quoteHash)
	if err != nil {
		return nil, fmt.Errorf("invalid quote hash: %w", err)
	}
	registerCertHashBytes, err := bytes32FromBytes(registerCertHash)
	if err != nil {
		return nil, fmt.Errorf("invalid register cert hash: %w", err)
	}
	challengeHashBytes, err := bytes32FromHex(challengeHash)
	if err != nil {
		return nil, fmt.Errorf("invalid challenge hash: %w", err)
	}

	addressType, err := ethabi.NewType("address", "", nil)
	if err != nil {
		return nil, err
	}
	uint256Type, err := ethabi.NewType("uint256", "", nil)
	if err != nil {
		return nil, err
	}
	bytes32Type, err := ethabi.NewType("bytes32", "", nil)
	if err != nil {
		return nil, err
	}
	bytes32PairType, err := ethabi.NewType("bytes32[2]", "", nil)
	if err != nil {
		return nil, err
	}

	args := ethabi.Arguments{
		{Type: addressType},
		{Type: uint256Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: bytes32PairType},
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
	}
	packed, err := args.Pack(
		ethcommon.HexToAddress(normalizedOwner),
		pendingNonce,
		mrEnclaveBytes,
		hardwareIDBytes,
		sigPubKeyBytes,
		vrfPubKeyBytes,
		enclavePKParts,
		mrSignerBytes,
		quoteHashBytes,
		registerCertHashBytes,
		challengeHashBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("abi pack register tee message: %w", err)
	}
	return ethcrypto.Keccak256(packed), nil
}

func buildRegisterCertHash(owner string, pendingNonce *big.Int, mrEnclave string, mrSigner string, hardwareID string, sigPubKey string, vrfPubKey string, quoteHash []byte, challengeHash string) ([]byte, error) {
	if pendingNonce == nil {
		return nil, fmt.Errorf("pending nonce is required")
	}
	normalizedOwner, err := normalizeOwnerAddress(owner)
	if err != nil {
		return nil, err
	}
	quoteHashBytes, err := bytes32FromBytes(quoteHash)
	if err != nil {
		return nil, err
	}
	fields := []string{
		"committee-sig/register-cert/v1",
		normalizedOwner,
		pendingNonce.String(),
		normalizeClaim(mrEnclave),
		normalizeClaim(mrSigner),
		normalizeClaim(hardwareID),
		normalizeClaim(sigPubKey),
		normalizeClaim(vrfPubKey),
		hex.EncodeToString(quoteHashBytes[:]),
		normalizeClaim(challengeHash),
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return sum[:], nil
}

func claimValue(claims map[string]string, key string) string {
	if len(claims) == 0 {
		return ""
	}
	for k, v := range claims {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func isAllowedClaim(value string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	value = normalizeClaim(value)
	if value == "" {
		return false
	}
	for _, candidate := range allowed {
		if value == normalizeClaim(candidate) {
			return true
		}
	}
	return false
}

func indexOfNode(nodes []string, nodeID string) int {
	for i, n := range nodes {
		if n == nodeID {
			return i
		}
	}
	return -1
}

func derivePrivateKey(nodeID string) *big.Int {
	_ = nodeID
	return nil
}

// frCompat keeps bn254 scalar modulus local to avoid importing fr in this layer.
type frCompat struct{}

func (frCompat) Modulus() *big.Int {
	v, _ := new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
	return v
}

func makeBitmap(indices []int, committeeSize int) []byte {
	if committeeSize <= 0 {
		return []byte{0}
	}
	bitmap := make([]byte, (committeeSize+7)/8)
	for _, idx := range indices {
		if idx < 0 || idx >= committeeSize {
			continue
		}
		byteIdx := idx / 8
		bitIdx := idx % 8
		bitmap[byteIdx] |= 1 << uint(bitIdx)
	}
	return bitmap
}
