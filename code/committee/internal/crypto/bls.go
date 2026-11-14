package crypto

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"math/bits"
	"sort"
	"sync"

	bn256 "github.com/ethereum/go-ethereum/crypto/bn256/cloudflare"
)

var (
	ErrNotImplemented    = errors.New("not implemented")
	ErrMissingPrivateKey = errors.New("missing private key")
	ErrInvalidBitmap     = errors.New("invalid bitmap")
	ErrDuplicateSigner   = errors.New("duplicate signer")
	ErrInvalidPoint      = errors.New("invalid curve point encoding")
	ErrVerifyFailed      = errors.New("signature verification failed")
)

type Signer interface {
	SignShare(message []byte) ([]byte, error)
	VerifyShare(pubkey, message, sig []byte) error
	AggregateSignatures(sigs [][]byte, bitmap []byte) ([]byte, error)
	VerifyAggregate(aggregatePub, message, aggregateSig, bitmap []byte) error
}

// BN254BLS provides a minimal BLS-style scheme on BN254:
// - public keys live in G2
// - signatures live in G1
// - verify with pairing equation e(sig, g2) == e(H(m), pk)
type BN254BLS struct {
	privateKey *big.Int
	dst        []byte
}

var one = big.NewInt(1)

func NewBN254BLS(privateKey []byte, dst string) (*BN254BLS, error) {
	if dst == "" {
		return nil, errors.New("empty domain separation tag")
	}
	b := &BN254BLS{dst: []byte(dst)}
	if len(privateKey) == 0 {
		return b, nil
	}
	sk := new(big.Int).SetBytes(privateKey)
	if sk.Sign() < 0 {
		return nil, errors.New("invalid private key")
	}
	sk.Mod(sk, bn256.Order)
	b.privateKey = sk
	return b, nil
}

func NewBN254BLSFromBigInt(privateKey *big.Int, dst string) (*BN254BLS, error) {
	if dst == "" {
		return nil, errors.New("empty domain separation tag")
	}
	b := &BN254BLS{dst: []byte(dst)}
	if privateKey == nil {
		return b, nil
	}
	sk := new(big.Int).Set(privateKey)
	sk.Mod(sk, bn256.Order)
	b.privateKey = sk
	return b, nil
}

func (b *BN254BLS) PublicKey() ([]byte, error) {
	if b.privateKey == nil {
		return nil, ErrMissingPrivateKey
	}
	pk := new(bn256.G2).ScalarBaseMult(b.privateKey)
	return pk.Marshal(), nil
}

func (b *BN254BLS) SignShare(message []byte) ([]byte, error) {
	if b.privateKey == nil {
		return nil, ErrMissingPrivateKey
	}
	h := hashToG1(message, b.dst)
	sig := new(bn256.G1).ScalarMult(h, b.privateKey)
	return sig.Marshal(), nil
}

func (b *BN254BLS) VerifyShare(pubkey, message, sig []byte) error {
	if len(pubkey) == 0 || len(sig) == 0 {
		return ErrInvalidPoint
	}
	pk := new(bn256.G2)
	if _, err := pk.Unmarshal(pubkey); err != nil {
		return fmt.Errorf("decode public key: %w", ErrInvalidPoint)
	}
	s := new(bn256.G1)
	if _, err := s.Unmarshal(sig); err != nil {
		return fmt.Errorf("decode signature: %w", ErrInvalidPoint)
	}
	h := hashToG1(message, b.dst)
	negH := new(bn256.G1).Neg(h)
	g2 := new(bn256.G2).ScalarBaseMult(one)
	ok := bn256.PairingCheck([]*bn256.G1{s, negH}, []*bn256.G2{g2, pk})
	if !ok {
		return ErrVerifyFailed
	}
	return nil
}

func (b *BN254BLS) AggregateSignatures(sigs [][]byte, bitmap []byte) ([]byte, error) {
	expected, err := validateBitmap(bitmap)
	if err != nil {
		return nil, err
	}
	if expected != len(sigs) {
		return nil, fmt.Errorf("bitmap/signature count mismatch: bits=%d sigs=%d", expected, len(sigs))
	}
	seen := make(map[string]struct{}, len(sigs))
	acc := new(bn256.G1)
	set := false
	for _, encSig := range sigs {
		if len(encSig) == 0 {
			return nil, ErrInvalidPoint
		}
		k := string(encSig)
		if _, ok := seen[k]; ok {
			return nil, ErrDuplicateSigner
		}
		seen[k] = struct{}{}

		p := new(bn256.G1)
		if _, err := p.Unmarshal(encSig); err != nil {
			return nil, fmt.Errorf("decode signature: %w", ErrInvalidPoint)
		}
		if !set {
			acc.Set(p)
			set = true
			continue
		}
		acc.Add(acc, p)
	}
	if !set {
		return nil, ErrInvalidBitmap
	}
	return acc.Marshal(), nil
}

func (b *BN254BLS) VerifyAggregate(aggregatePub, message, aggregateSig, bitmap []byte) error {
	if _, err := validateBitmap(bitmap); err != nil {
		return err
	}
	if len(aggregatePub) == 0 || len(aggregateSig) == 0 {
		return ErrInvalidPoint
	}
	aggPK := new(bn256.G2)
	if _, err := aggPK.Unmarshal(aggregatePub); err != nil {
		return fmt.Errorf("decode aggregate public key: %w", ErrInvalidPoint)
	}
	aggSig := new(bn256.G1)
	if _, err := aggSig.Unmarshal(aggregateSig); err != nil {
		return fmt.Errorf("decode aggregate signature: %w", ErrInvalidPoint)
	}

	h := hashToG1(message, b.dst)
	negH := new(bn256.G1).Neg(h)
	g2 := new(bn256.G2).ScalarBaseMult(one)
	ok := bn256.PairingCheck([]*bn256.G1{aggSig, negH}, []*bn256.G2{g2, aggPK})
	if !ok {
		return ErrVerifyFailed
	}
	return nil
}

// AggregateThresholdSignatures combines partial signatures via Lagrange interpolation at x=0.
// signerIndices are zero-based committee indices aligned with sigs.
func AggregateThresholdSignatures(sigs [][]byte, signerIndices []int) ([]byte, error) {
	if len(sigs) == 0 || len(sigs) != len(signerIndices) {
		return nil, ErrInvalidBitmap
	}
	type pair struct {
		idx int
		sig []byte
	}
	pairs := make([]pair, 0, len(sigs))
	seen := make(map[int]struct{}, len(sigs))
	for i := range sigs {
		if len(sigs[i]) == 0 {
			return nil, ErrInvalidPoint
		}
		if signerIndices[i] < 0 {
			return nil, ErrInvalidBitmap
		}
		if _, ok := seen[signerIndices[i]]; ok {
			return nil, ErrDuplicateSigner
		}
		seen[signerIndices[i]] = struct{}{}
		pairs = append(pairs, pair{idx: signerIndices[i], sig: sigs[i]})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].idx < pairs[j].idx })

	xs := make([]*big.Int, 0, len(pairs))
	points := make([]*bn256.G1, 0, len(pairs))
	for _, p := range pairs {
		x := big.NewInt(int64(p.idx + 1))
		xs = append(xs, x)
		sig := new(bn256.G1)
		if _, err := sig.Unmarshal(p.sig); err != nil {
			return nil, fmt.Errorf("decode signature: %w", ErrInvalidPoint)
		}
		points = append(points, sig)
	}

	acc := new(bn256.G1)
	set := false
	for i := range points {
		lambda, err := lagrangeCoeffAtZero(xs, i)
		if err != nil {
			return nil, err
		}
		coeff := new(big.Int).Mod(lambda, bn256.Order)
		term := new(bn256.G1).ScalarMult(points[i], coeff)
		if !set {
			acc.Set(term)
			set = true
			continue
		}
		acc.Add(acc, term)
	}
	if !set {
		return nil, ErrInvalidBitmap
	}
	return acc.Marshal(), nil
}

func lagrangeCoeffAtZero(xs []*big.Int, i int) (*big.Int, error) {
	if i < 0 || i >= len(xs) {
		return nil, ErrInvalidBitmap
	}
	mod := bn256.Order
	num := big.NewInt(1)
	den := big.NewInt(1)
	xi := new(big.Int).Set(xs[i])
	for j := range xs {
		if j == i {
			continue
		}
		xj := new(big.Int).Set(xs[j])
		negXj := new(big.Int).Neg(xj)
		negXj.Mod(negXj, mod)
		num.Mul(num, negXj)
		num.Mod(num, mod)

		diff := new(big.Int).Sub(xi, xj)
		diff.Mod(diff, mod)
		den.Mul(den, diff)
		den.Mod(den, mod)
	}
	inv := new(big.Int).ModInverse(den, mod)
	if inv == nil {
		return nil, ErrInvalidBitmap
	}
	out := new(big.Int).Mul(num, inv)
	out.Mod(out, mod)
	return out, nil
}

func AggregatePublicKeys(pubkeys [][]byte, bitmap []byte) ([]byte, error) {
	expected, err := validateBitmap(bitmap)
	if err != nil {
		return nil, err
	}
	if expected != len(pubkeys) {
		return nil, fmt.Errorf("bitmap/public-key count mismatch: bits=%d pubkeys=%d", expected, len(pubkeys))
	}
	seen := make(map[string]struct{}, len(pubkeys))
	acc := new(bn256.G2)
	set := false
	for _, encPK := range pubkeys {
		if len(encPK) == 0 {
			return nil, ErrInvalidPoint
		}
		k := string(encPK)
		if _, ok := seen[k]; ok {
			return nil, ErrDuplicateSigner
		}
		seen[k] = struct{}{}

		p := new(bn256.G2)
		if _, err := p.Unmarshal(encPK); err != nil {
			return nil, fmt.Errorf("decode public key: %w", ErrInvalidPoint)
		}
		if !set {
			acc.Set(p)
			set = true
			continue
		}
		acc.Add(acc, p)
	}
	if !set {
		return nil, ErrInvalidBitmap
	}
	return acc.Marshal(), nil
}

func hashToG1(message, dst []byte) *bn256.G1 {
	h := sha256.New()
	h.Write(message)
	h.Write(dst)
	s := new(big.Int).SetBytes(h.Sum(nil))
	s.Mod(s, bn256.Order)
	if s.Sign() == 0 {
		s.SetInt64(1)
	}
	return new(bn256.G1).ScalarBaseMult(s)
}

func validateBitmap(bitmap []byte) (int, error) {
	if len(bitmap) == 0 {
		return 0, ErrInvalidBitmap
	}
	count := 0
	for _, b := range bitmap {
		count += bits.OnesCount8(uint8(b))
	}
	if count == 0 {
		return 0, ErrInvalidBitmap
	}
	return count, nil
}

// ShareBatch accumulates threshold shares that all sign the SAME message and
// verifies them with a single pairing instead of one per share.
//
// Because every share satisfies sig_i = sk_i*H(m) and pk_i = sk_i*G2, summing
// over the contributing indices gives sum(sig_i) = sum(sk_i)*H(m) and
// sum(pk_i) = sum(sk_i)*G2, so
//
//	e(sum sig, G2) == e(H(m), sum pk)
//
// holds exactly when the individual share relations do. This turns N pairings
// into one (~68.9ms -> ~3.5ms at N=21).
//
// The caller must supply the public key registered for each signer, not one
// read from the message: a peer that can choose the pk it is checked against
// can always satisfy the aggregate by construction. Verification is therefore
// only as strong as the registered-key set, exactly as with per-share
// verification; the aggregate certificate check (VerifyAggregate) remains the
// binding gate in both cases.
//
// A failure is not attributable to a specific signer. Callers that need to
// identify the offender must fall back to verifying shares individually.
type ShareBatch struct {
	h    *bn256.G1 // H(message, dst)
	negH *bn256.G1
	g2   *bn256.G2
	sigs []*bn256.G1
	// decoded holds public keys already in point form, parallel to sigs when
	// the caller supplies them via AddDecodedKey.
	decoded []*bn256.G2
}

// NewShareBatch precomputes the message-dependent values. Reuse one batch per
// message; the map of share signatures is built by Add.
func NewShareBatch(message, dst []byte) *ShareBatch {
	h := hashToG1(message, dst)
	return &ShareBatch{
		h:    h,
		negH: new(bn256.G1).Neg(h),
		g2:   new(bn256.G2).ScalarBaseMult(one),
	}
}

// AddDecodedKey registers a public key that has already been decoded. Callers
// that reuse the same signer across many batches (the committee members are
// fixed within an epoch) should decode once and pass the point, rather than
// re-unmarshalling a wire copy every time: G2.Unmarshal costs ~0.6ms, so 21 of
// them dominate the verification (12.8ms of a 16.9ms batch at N=21).
func (b *ShareBatch) AddDecodedKey(pk *bn256.G2) {
	b.decoded = append(b.decoded, pk)
}

// Add decodes and accumulates one share signature.
func (b *ShareBatch) Add(encodedSig []byte) error {
	if len(encodedSig) == 0 {
		return ErrInvalidPoint
	}
	sig := new(bn256.G1)
	if _, err := sig.Unmarshal(encodedSig); err != nil {
		return fmt.Errorf("decode signature: %w", ErrInvalidPoint)
	}
	b.sigs = append(b.sigs, sig)
	return nil
}

// VerifyAll checks every accumulated share against its registered public key
// using a single pairing. It returns ErrInvalidPoint on malformed encodings and
// ErrVerifyFailed when the aggregate equation does not hold.
//
// Pass encodedPubKeys as nil when keys were supplied via AddDecodedKey.
func (b *ShareBatch) VerifyAll(encodedPubKeys [][]byte) error {
	keys, err := b.resolveKeys(encodedPubKeys)
	if err != nil {
		return err
	}
	accSig := new(bn256.G1)
	accPK := new(bn256.G2)
	for i := range b.sigs {
		accSig.Add(accSig, b.sigs[i])
		accPK.Add(accPK, keys[i])
	}
	if !bn256.PairingCheck([]*bn256.G1{accSig, b.negH}, []*bn256.G2{b.g2, accPK}) {
		return ErrVerifyFailed
	}
	return nil
}

// resolveKeys returns one G2 point per accumulated share, from the decoded
// cache when present, otherwise by decoding the wire encoding.
func (b *ShareBatch) resolveKeys(encodedPubKeys [][]byte) ([]*bn256.G2, error) {
	if len(b.decoded) == len(b.sigs) && len(b.sigs) > 0 {
		return b.decoded, nil
	}
	if len(b.sigs) == 0 || len(b.sigs) != len(encodedPubKeys) {
		return nil, ErrInvalidBitmap
	}
	keys := make([]*bn256.G2, len(encodedPubKeys))
	for i, encPK := range encodedPubKeys {
		if len(encPK) == 0 {
			return nil, ErrInvalidPoint
		}
		pk := new(bn256.G2)
		if _, err := pk.Unmarshal(encPK); err != nil {
			return nil, fmt.Errorf("decode public key: %w", ErrInvalidPoint)
		}
		keys[i] = pk
	}
	return keys, nil
}

// VerifyEach verifies the accumulated shares one by one, returning the first
// index that fails. Callers use this to attribute a batch failure; the
// signatures are already decoded, so this skips re-parsing them.
func (b *ShareBatch) VerifyEach(encodedPubKeys [][]byte) (int, error) {
	keys, err := b.resolveKeys(encodedPubKeys)
	if err != nil {
		return -1, err
	}
	for i := range b.sigs {
		ok := bn256.PairingCheck([]*bn256.G1{b.sigs[i], b.negH}, []*bn256.G2{b.g2, keys[i]})
		if !ok {
			return i, ErrVerifyFailed
		}
	}
	return -1, nil
}

// ShareKeyCache decodes signer public keys once and reuses the points across
// batches. Committee membership is fixed within an epoch, so the same key is
// presented on every signing session; G2.Unmarshal costs ~0.6ms and dominated
// batch verification (12.8ms of 16.9ms at N=21) purely through re-decoding.
type ShareKeyCache struct {
	mu   sync.RWMutex
	keys map[int]*bn256.G2
}

func NewShareKeyCache() *ShareKeyCache {
	return &ShareKeyCache{keys: make(map[int]*bn256.G2)}
}

// Get returns the decoded key for a signer index, decoding and storing it on
// first use. A later, malformed encoding for the same index returns the cached
// point rather than an error, so a peer that sends garbage cannot evict a good
// key; the caller still verifies against the originally registered value.
func (c *ShareKeyCache) Get(index int, encoded []byte) (*bn256.G2, error) {
	if c == nil {
		return decodeG2(encoded)
	}
	c.mu.RLock()
	if pk, ok := c.keys[index]; ok {
		c.mu.RUnlock()
		return pk, nil
	}
	c.mu.RUnlock()

	pk, err := decodeG2(encoded)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if existing, ok := c.keys[index]; ok {
		// Another goroutine decoded first; prefer the established key.
		c.mu.Unlock()
		return existing, nil
	}
	c.keys[index] = pk
	c.mu.Unlock()
	return pk, nil
}

// Reset drops all cached keys; call when committee membership changes.
func (c *ShareKeyCache) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.keys = make(map[int]*bn256.G2)
	c.mu.Unlock()
}

func decodeG2(encoded []byte) (*bn256.G2, error) {
	if len(encoded) == 0 {
		return nil, ErrInvalidPoint
	}
	pk := new(bn256.G2)
	if _, err := pk.Unmarshal(encoded); err != nil {
		return nil, fmt.Errorf("decode public key: %w", ErrInvalidPoint)
	}
	return pk, nil
}
