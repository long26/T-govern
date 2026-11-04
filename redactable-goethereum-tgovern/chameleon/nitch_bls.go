package chameleon

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"sync"

	bls12381 "github.com/kilic/bls12-381"
)

const (
	nitchG1PointSize      = 96
	nitchG1CompressedSize = 48
	nitchG2PointSize      = 192
	nitchH1DST            = "NITCH_BLS12381_H1"
)

var (
	nitchScalarOrder, _ = new(big.Int).SetString("73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)

	ErrNITCHBLSInsufficientShares = errors.New("insufficient NITCH BLS shares")

	systemNITCHMu  sync.RWMutex
	systemNITCHKey *NITCHBLSKey

	nitchRootCache = struct {
		sync.RWMutex
		roots map[int]*big.Int
	}{roots: make(map[int]*big.Int)}
)

// NITCHBLSPublicParameters contains the BLS12-381 generators and scalar order.
type NITCHBLSPublicParameters struct {
	G1 []byte
	G2 []byte
	P  *big.Int
}

// NITCHBLSKey is the public/private material for a committee member.
// SecretShare is only for non-TEE tests and compatibility helpers; production
// DKG/DKRS keeps it inside the enclave.
type NITCHBLSKey struct {
	PP              *NITCHBLSPublicParameters
	GlobalPK        []byte
	GlobalPKG1      []byte
	Threshold       int
	NumParties      int
	SecretShare     *big.Int
	VerificationKey []byte
	Index           int
}

type NITCHBLSHashOutput struct {
	Hash       []byte
	CheckValue []byte
}

type NITCHBLSProof struct {
	Challenge *big.Int
	Response  *big.Int
}

type NITCHBLSAdaptationShare struct {
	Index int
	Share []byte
	Proof *NITCHBLSProof
}

type NITCHBLSDistKGDeal struct {
	DealerIndex int
	Threshold   int
	NumParties  int
	A           []byte
	B0          []byte
	G1          []byte
	G2          []byte
	P           string
}

type NITCHBLSDistKGPrivate struct {
	DealerIndex int
	Coeffs      []*big.Int
	Shares      map[int]*big.Int
}

type NITCHBLSDistKGShare struct {
	FromIndex int
	ToIndex   int
	Share     *big.Int
}

func NITCHBLSSetup() (*NITCHBLSPublicParameters, error) {
	engine := bls12381.NewEngine()
	g1 := engine.G1.One()
	g2 := engine.G2.One()
	if g1 == nil || g2 == nil {
		return nil, errors.New("failed to initialize BLS12-381 generators")
	}
	return &NITCHBLSPublicParameters{
		G1: engine.G1.ToBytes(g1),
		G2: engine.G2.ToBytes(g2),
		P:  new(big.Int).Set(nitchScalarOrder),
	}, nil
}

func NITCHBLSSimulatedDistKG(pp *NITCHBLSPublicParameters, threshold, numParties int) ([]*NITCHBLSKey, error) {
	if err := validateNITCHThreshold(threshold, numParties); err != nil {
		return nil, err
	}
	if pp == nil || pp.P == nil {
		return nil, errors.New("nil public parameters")
	}
	engine := bls12381.NewEngine()
	g1, g2, err := nitchGenerators(engine, pp)
	if err != nil {
		return nil, err
	}
	secret := nitchRandomScalar(pp.P)
	coeffs := make([]*big.Int, threshold+1)
	coeffs[0] = secret
	for i := 1; i <= threshold; i++ {
		coeffs[i] = nitchRandomScalar(pp.P)
	}

	globalPK := engine.G2.New()
	engine.G2.MulScalar(globalPK, g2, nitchBigIntToFr(secret))
	globalPKG1 := engine.G1.New()
	engine.G1.MulScalar(globalPKG1, g1, nitchBigIntToFr(secret))

	keys := make([]*NITCHBLSKey, numParties)
	for i := 1; i <= numParties; i++ {
		x, err := nitchEvaluationPoint(pp.P, numParties, i)
		if err != nil {
			return nil, err
		}
		share := nitchEvaluatePolynomial(coeffs, x, pp.P)
		vk := engine.G1.New()
		engine.G1.MulScalar(vk, g1, nitchBigIntToFr(share))
		keys[i-1] = &NITCHBLSKey{
			PP:              pp,
			GlobalPK:        engine.G2.ToBytes(globalPK),
			GlobalPKG1:      engine.G1.ToBytes(globalPKG1),
			Threshold:       threshold,
			NumParties:      numParties,
			SecretShare:     share,
			VerificationKey: engine.G1.ToBytes(vk),
			Index:           i,
		}
	}
	return keys, nil
}

func NITCHBLSCreateDistKGDeal(pp *NITCHBLSPublicParameters, dealerIndex, threshold, numParties int) (*NITCHBLSDistKGDeal, *NITCHBLSDistKGPrivate, error) {
	if err := validateNITCHThreshold(threshold, numParties); err != nil {
		return nil, nil, err
	}
	if dealerIndex < 1 || dealerIndex > numParties {
		return nil, nil, fmt.Errorf("dealerIndex must be in [1,%d]", numParties)
	}
	engine := bls12381.NewEngine()
	g1, g2, err := nitchGenerators(engine, pp)
	if err != nil {
		return nil, nil, err
	}
	coeffs := make([]*big.Int, threshold+1)
	for i := range coeffs {
		coeffs[i] = nitchRandomScalar(pp.P)
	}

	aBytes := make([]byte, 0, (threshold+1)*nitchG1PointSize)
	for _, coeff := range coeffs {
		a := engine.G1.New()
		engine.G1.MulScalar(a, g1, nitchBigIntToFr(coeff))
		aBytes = append(aBytes, engine.G1.ToBytes(a)...)
	}
	b0 := engine.G2.New()
	engine.G2.MulScalar(b0, g2, nitchBigIntToFr(coeffs[0]))

	shares := make(map[int]*big.Int, numParties)
	for i := 1; i <= numParties; i++ {
		x, err := nitchEvaluationPoint(pp.P, numParties, i)
		if err != nil {
			return nil, nil, err
		}
		shares[i] = nitchEvaluatePolynomial(coeffs, x, pp.P)
	}
	deal := &NITCHBLSDistKGDeal{
		DealerIndex: dealerIndex,
		Threshold:   threshold,
		NumParties:  numParties,
		A:           aBytes,
		B0:          engine.G2.ToBytes(b0),
		G1:          append([]byte(nil), pp.G1...),
		G2:          append([]byte(nil), pp.G2...),
		P:           pp.P.Text(16),
	}
	priv := &NITCHBLSDistKGPrivate{
		DealerIndex: dealerIndex,
		Coeffs:      coeffs,
		Shares:      shares,
	}
	return deal, priv, nil
}

func NITCHBLSVerifyDistKGShare(pp *NITCHBLSPublicParameters, deal *NITCHBLSDistKGDeal, share *big.Int, receiverIndex int) (bool, error) {
	if pp == nil || deal == nil || share == nil {
		return false, errors.New("nil DistKG input")
	}
	if err := validateNITCHThreshold(deal.Threshold, deal.NumParties); err != nil {
		return false, err
	}
	if receiverIndex < 1 || receiverIndex > deal.NumParties {
		return false, fmt.Errorf("receiverIndex must be in [1,%d]", deal.NumParties)
	}
	if len(deal.A) != (deal.Threshold+1)*nitchG1PointSize {
		return false, fmt.Errorf("invalid commitment length: %d", len(deal.A))
	}
	engine := bls12381.NewEngine()
	g1, g2, err := nitchGenerators(engine, pp)
	if err != nil {
		return false, err
	}
	b0, err := engine.G2.FromBytes(deal.B0)
	if err != nil {
		return false, err
	}
	a0, err := engine.G1.FromBytes(deal.A[:nitchG1PointSize])
	if err != nil {
		return false, err
	}
	engine.Reset()
	engine.AddPair(g1, b0)
	left := engine.Result()
	engine.Reset()
	engine.AddPair(a0, g2)
	right := engine.Result()
	if !left.Equal(right) {
		return false, nil
	}

	x, err := nitchEvaluationPoint(pp.P, deal.NumParties, receiverIndex)
	if err != nil {
		return false, err
	}
	xPower := big.NewInt(1)
	expected := engine.G1.Zero()
	for i := 0; i <= deal.Threshold; i++ {
		start := i * nitchG1PointSize
		commitment, err := engine.G1.FromBytes(deal.A[start : start+nitchG1PointSize])
		if err != nil {
			return false, err
		}
		term := engine.G1.New()
		engine.G1.MulScalar(term, commitment, nitchBigIntToFr(xPower))
		engine.G1.Add(expected, expected, term)
		xPower.Mul(xPower, x)
		xPower.Mod(xPower, pp.P)
	}
	actual := engine.G1.New()
	engine.G1.MulScalar(actual, g1, nitchBigIntToFr(share))
	return bytes.Equal(engine.G1.ToBytes(actual), engine.G1.ToBytes(expected)), nil
}

func NITCHBLSDistKGExtract(pp *NITCHBLSPublicParameters, myIndex int, qual []int, deals map[int]*NITCHBLSDistKGDeal, sharesToMe map[int]*big.Int) (*NITCHBLSKey, error) {
	if pp == nil || pp.P == nil || len(qual) == 0 {
		return nil, errors.New("invalid DistKG extract input")
	}
	first := deals[qual[0]]
	if first == nil {
		return nil, fmt.Errorf("missing first qualified deal")
	}
	if myIndex < 1 || myIndex > first.NumParties {
		return nil, fmt.Errorf("myIndex must be in [1,%d]", first.NumParties)
	}
	engine := bls12381.NewEngine()
	g1, _, err := nitchGenerators(engine, pp)
	if err != nil {
		return nil, err
	}
	globalPK := engine.G2.Zero()
	globalPKG1 := engine.G1.Zero()
	secretShare := new(big.Int)
	for _, idx := range qual {
		deal := deals[idx]
		share := sharesToMe[idx]
		if deal == nil || share == nil {
			return nil, fmt.Errorf("missing deal/share from %d", idx)
		}
		b0, err := engine.G2.FromBytes(deal.B0)
		if err != nil {
			return nil, err
		}
		a0, err := engine.G1.FromBytes(deal.A[:nitchG1PointSize])
		if err != nil {
			return nil, err
		}
		engine.G2.Add(globalPK, globalPK, b0)
		engine.G1.Add(globalPKG1, globalPKG1, a0)
		secretShare.Add(secretShare, share)
		secretShare.Mod(secretShare, pp.P)
	}
	vk := engine.G1.New()
	engine.G1.MulScalar(vk, g1, nitchBigIntToFr(secretShare))
	return &NITCHBLSKey{
		PP:              pp,
		GlobalPK:        engine.G2.ToBytes(globalPK),
		GlobalPKG1:      engine.G1.ToBytes(globalPKG1),
		Threshold:       first.Threshold,
		NumParties:      first.NumParties,
		SecretShare:     secretShare,
		VerificationKey: engine.G1.ToBytes(vk),
		Index:           myIndex,
	}, nil
}

func SetSystemNITCHBLSKey(key *NITCHBLSKey) {
	systemNITCHMu.Lock()
	defer systemNITCHMu.Unlock()
	systemNITCHKey = key.ClonePublic()
}

func GetSystemNITCHBLSKey() (*NITCHBLSKey, error) {
	systemNITCHMu.RLock()
	defer systemNITCHMu.RUnlock()
	if systemNITCHKey == nil {
		return nil, errors.New("system NITCH BLS key is not initialized")
	}
	return systemNITCHKey.ClonePublic(), nil
}

func (key *NITCHBLSKey) ClonePublic() *NITCHBLSKey {
	if key == nil {
		return nil
	}
	clone := *key
	if key.PP != nil {
		clone.PP = &NITCHBLSPublicParameters{
			G1: append([]byte(nil), key.PP.G1...),
			G2: append([]byte(nil), key.PP.G2...),
			P:  new(big.Int).Set(key.PP.P),
		}
	}
	clone.GlobalPK = append([]byte(nil), key.GlobalPK...)
	clone.GlobalPKG1 = append([]byte(nil), key.GlobalPKG1...)
	clone.VerificationKey = append([]byte(nil), key.VerificationKey...)
	clone.SecretShare = nil
	return &clone
}

func NITCHBLSComputeR(key *NITCHBLSKey, seedParts ...[]byte) ([]byte, error) {
	if key == nil || key.PP == nil || len(key.GlobalPKG1) != nitchG1PointSize {
		return nil, errors.New("invalid NITCH key")
	}
	engine := bls12381.NewEngine()
	pkG1, err := engine.G1.FromBytes(key.GlobalPKG1)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	for _, part := range seedParts {
		h.Write(part)
	}
	seedPoint, err := nitchHashToG1(engine, h.Sum(nil))
	if err != nil {
		return nil, err
	}
	r := engine.G1.New()
	engine.G1.Add(r, seedPoint, pkG1)
	return engine.G1.ToCompressed(r), nil
}

func NITCHBLSHash(key *NITCHBLSKey, message []byte, r []byte) (*NITCHBLSHashOutput, error) {
	if key == nil || key.PP == nil {
		return nil, errors.New("invalid NITCH key")
	}
	engine := bls12381.NewEngine()
	_, g2, err := nitchGenerators(engine, key.PP)
	if err != nil {
		return nil, err
	}
	rPoint, err := decodeNITCHBLSR(engine, r)
	if err != nil {
		return nil, err
	}
	globalPK, err := engine.G2.FromBytes(key.GlobalPK)
	if err != nil {
		return nil, err
	}
	h1, err := nitchHashToG1(engine, message)
	if err != nil {
		return nil, err
	}
	engine.Reset()
	engine.AddPair(rPoint, g2)
	engine.AddPair(h1, globalPK)
	gt := engine.Result()
	return &NITCHBLSHashOutput{
		Hash:       engine.GT().ToBytes(gt),
		CheckValue: append([]byte(nil), r...),
	}, nil
}

func NITCHBLSPublicKeyToBigInt(key *NITCHBLSKey) (*big.Int, error) {
	if key == nil || len(key.GlobalPK) != nitchG2PointSize {
		return nil, errors.New("invalid NITCH BLS public key")
	}
	return new(big.Int).SetBytes(key.GlobalPK), nil
}

func NITCHBLSRToBigInt(r []byte) (*big.Int, error) {
	if len(r) != nitchG1CompressedSize {
		return nil, fmt.Errorf("invalid NITCH BLS r length: %d", len(r))
	}
	if _, err := decodeNITCHBLSR(bls12381.NewEngine(), r); err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(r), nil
}

func NITCHBLSBigIntToR(r *big.Int) ([]byte, error) {
	if r == nil || r.Sign() == 0 {
		return nil, errors.New("invalid NITCH BLS r")
	}
	if len(r.Bytes()) != nitchG1CompressedSize {
		return nil, fmt.Errorf("invalid NITCH BLS compressed r length: %d", len(r.Bytes()))
	}
	out := append([]byte(nil), r.Bytes()...)
	if _, err := decodeNITCHBLSR(bls12381.NewEngine(), out); err != nil {
		return nil, err
	}
	return out, nil
}

// nitchBLSHashContext keeps the pairing engine and the pre-parsed system
// points (generator g2, system public key) alive across legacy Hash/Verify
// calls, mirroring the nitch fork's Hash design: the kilic engine's internal
// scratch and the shared system points are mutated in place during pairing,
// so all use is serialized under one mutex. Keyed by the system public key
// bytes; rebuilt automatically when the committee key rotates.
type nitchBLSHashContext struct {
	engine   *bls12381.Engine
	g2       *bls12381.PointG2
	globalPK *bls12381.PointG2
}

var (
	nitchBLSCtxMu sync.Mutex
	nitchBLSCtx   *nitchBLSHashContext
	nitchBLSCtxPk []byte
)

// nitchBLSCtxFor returns the cached context for pkBytes, rebuilding it when
// the system public key changed. Caller must hold nitchBLSCtxMu.
func nitchBLSCtxFor(pkBytes []byte) (*nitchBLSHashContext, error) {
	if nitchBLSCtx != nil && bytes.Equal(nitchBLSCtxPk, pkBytes) {
		return nitchBLSCtx, nil
	}
	engine := bls12381.NewEngine()
	g2 := engine.G2.One()
	if g2 == nil {
		return nil, errors.New("failed to initialize BLS G2 generator")
	}
	globalPK, err := engine.G2.FromBytes(pkBytes)
	if err != nil {
		return nil, err
	}
	nitchBLSCtx = &nitchBLSHashContext{engine: engine, g2: g2, globalPK: globalPK}
	nitchBLSCtxPk = append([]byte(nil), pkBytes...)
	return nitchBLSCtx, nil
}

func nitchBLSHashBigInt(publicKey, message, r *big.Int) (*big.Int, bool) {
	if publicKey == nil || message == nil || r == nil || publicKey.Sign() == 0 || r.Sign() == 0 {
		return nil, false
	}
	if len(publicKey.Bytes()) > nitchG2PointSize || len(r.Bytes()) != nitchG1CompressedSize {
		return nil, false
	}
	pkBytes := publicKey.FillBytes(make([]byte, nitchG2PointSize))
	rBytes := append([]byte(nil), r.Bytes()...)

	nitchBLSCtxMu.Lock()
	defer nitchBLSCtxMu.Unlock()
	ctx, err := nitchBLSCtxFor(pkBytes)
	if err != nil {
		return nil, false
	}
	rPoint, err := decodeNITCHBLSR(ctx.engine, rBytes)
	if err != nil {
		return nil, false
	}
	h1, err := nitchHashToG1(ctx.engine, message.Bytes())
	if err != nil {
		return nil, false
	}
	// Same pairing layout as NITCHBLSHash: e(r, g2) * e(H1(m), pk), where g2
	// is the standard generator (engine.G2.One(), i.e. exactly what
	// NITCHBLSSetup serializes) and pk was parsed once per key rotation.
	ctx.engine.Reset()
	ctx.engine.AddPair(rPoint, ctx.g2)
	ctx.engine.AddPair(h1, ctx.globalPK)
	gt := ctx.engine.Result()
	if gt == nil {
		return nil, false
	}
	return new(big.Int).SetBytes(ctx.engine.GT().ToBytes(gt)), true
}

func NITCHBLSVerify(key *NITCHBLSKey, hash []byte, message []byte, r []byte) bool {
	out, err := NITCHBLSHash(key, message, r)
	if err != nil {
		return false
	}
	return bytes.Equal(out.Hash, hash)
}

func NITCHBLSPartialAdapt(key *NITCHBLSKey, oldMessage, newMessage []byte) (*NITCHBLSAdaptationShare, error) {
	if key == nil || key.PP == nil || key.SecretShare == nil || len(key.VerificationKey) != nitchG1PointSize {
		return nil, errors.New("invalid NITCH key for partial adapt")
	}
	engine := bls12381.NewEngine()
	hOld, err := nitchHashToG1(engine, oldMessage)
	if err != nil {
		return nil, err
	}
	hNew, err := nitchHashToG1(engine, newMessage)
	if err != nil {
		return nil, err
	}
	y := engine.G1.New()
	negNew := engine.G1.New()
	engine.G1.Neg(negNew, hNew)
	engine.G1.Add(y, hOld, negNew)

	contribution := engine.G1.New()
	engine.G1.MulScalar(contribution, y, nitchBigIntToFr(key.SecretShare))
	proof, err := nitchProveAdaptation(engine, key, y, contribution)
	if err != nil {
		return nil, err
	}
	return &NITCHBLSAdaptationShare{
		Index: key.Index,
		Share: engine.G1.ToBytes(contribution),
		Proof: proof,
	}, nil
}

func NITCHBLSPartialVerify(key *NITCHBLSKey, share *NITCHBLSAdaptationShare, oldMessage, newMessage []byte) bool {
	if key == nil || key.PP == nil || share == nil || share.Proof == nil {
		return false
	}
	engine := bls12381.NewEngine()
	hOld, err := nitchHashToG1(engine, oldMessage)
	if err != nil {
		return false
	}
	hNew, err := nitchHashToG1(engine, newMessage)
	if err != nil {
		return false
	}
	y := engine.G1.New()
	negNew := engine.G1.New()
	engine.G1.Neg(negNew, hNew)
	engine.G1.Add(y, hOld, negNew)
	contribution, err := engine.G1.FromBytes(share.Share)
	if err != nil {
		return false
	}
	return nitchVerifyAdaptation(engine, key, y, contribution, share.Proof)
}

func NITCHBLSCombine(pp *NITCHBLSPublicParameters, shares []*NITCHBLSAdaptationShare, threshold, numParties int, hashOutput *NITCHBLSHashOutput) ([]byte, error) {
	return nitchBLSCombineWithPoint(pp, shares, threshold, numParties, hashOutput, func(pp *NITCHBLSPublicParameters, numParties, index int) (*big.Int, error) {
		return nitchEvaluationPoint(pp.P, numParties, index)
	})
}

func NITCHBLSCombineEnclaveIndexed(pp *NITCHBLSPublicParameters, shares []*NITCHBLSAdaptationShare, threshold, numParties int, hashOutput *NITCHBLSHashOutput) ([]byte, error) {
	return nitchBLSCombineWithPoint(pp, shares, threshold, numParties, hashOutput, func(pp *NITCHBLSPublicParameters, numParties, index int) (*big.Int, error) {
		return nitchEvaluationPoint(pp.P, numParties, index)
	})
}

func nitchBLSCombineWithPoint(
	pp *NITCHBLSPublicParameters,
	shares []*NITCHBLSAdaptationShare,
	threshold, numParties int,
	hashOutput *NITCHBLSHashOutput,
	evaluationPoint func(*NITCHBLSPublicParameters, int, int) (*big.Int, error),
) ([]byte, error) {
	if pp == nil || pp.P == nil || hashOutput == nil {
		return nil, errors.New("invalid combine input")
	}
	if err := validateNITCHThreshold(threshold, numParties); err != nil {
		return nil, err
	}
	if len(shares) < threshold+1 {
		return nil, ErrNITCHBLSInsufficientShares
	}
	engine := bls12381.NewEngine()
	valid := shares[:threshold+1]
	points := make([]*big.Int, len(valid))
	seen := make(map[int]struct{}, len(valid))
	for i, share := range valid {
		if share == nil || share.Index < 1 || share.Index > numParties {
			return nil, fmt.Errorf("invalid share index")
		}
		if _, ok := seen[share.Index]; ok {
			return nil, fmt.Errorf("duplicate share index %d", share.Index)
		}
		seen[share.Index] = struct{}{}
		x, err := evaluationPoint(pp, numParties, share.Index)
		if err != nil {
			return nil, err
		}
		points[i] = x
	}
	coeffs, err := nitchLagrangeAtZero(points, pp.P)
	if err != nil {
		return nil, err
	}
	yAlpha := engine.G1.Zero()
	for i, share := range valid {
		point, err := engine.G1.FromBytes(share.Share)
		if err != nil {
			return nil, err
		}
		term := engine.G1.New()
		engine.G1.MulScalar(term, point, nitchBigIntToFr(coeffs[i]))
		engine.G1.Add(yAlpha, yAlpha, term)
	}
	rPoint, err := decodeNITCHBLSR(engine, hashOutput.CheckValue)
	if err != nil {
		return nil, err
	}
	rPrime := engine.G1.New()
	engine.G1.Add(rPrime, rPoint, yAlpha)
	return engine.G1.ToCompressed(rPrime), nil
}

func decodeNITCHBLSR(engine *bls12381.Engine, encoded []byte) (*bls12381.PointG1, error) {
	if engine == nil {
		return nil, errors.New("nil BLS12-381 engine")
	}
	if len(encoded) != nitchG1CompressedSize {
		return nil, fmt.Errorf("invalid NITCH BLS compressed r length: %d", len(encoded))
	}
	return engine.G1.FromCompressed(encoded)
}

func validateNITCHThreshold(threshold, numParties int) error {
	if threshold < 0 || numParties <= 0 || threshold >= numParties {
		return fmt.Errorf("invalid threshold/numParties: %d/%d", threshold, numParties)
	}
	if numParties&(numParties-1) != 0 {
		return fmt.Errorf("numParties must be a power of two")
	}
	return nil
}

func nitchGenerators(engine *bls12381.Engine, pp *NITCHBLSPublicParameters) (*bls12381.PointG1, *bls12381.PointG2, error) {
	if pp == nil || pp.P == nil {
		return nil, nil, errors.New("nil public parameters")
	}
	g1, err := engine.G1.FromBytes(pp.G1)
	if err != nil {
		return nil, nil, err
	}
	g2, err := engine.G2.FromBytes(pp.G2)
	if err != nil {
		return nil, nil, err
	}
	return g1, g2, nil
}

func nitchRandomScalar(modulus *big.Int) *big.Int {
	for {
		n, err := rand.Int(rand.Reader, modulus)
		if err != nil {
			panic(err)
		}
		if n.Sign() > 0 {
			return n
		}
	}
}

func nitchBigIntToFr(n *big.Int) *bls12381.Fr {
	fr := bls12381.NewFr()
	if n == nil {
		return fr
	}
	b := new(big.Int).Mod(new(big.Int).Set(n), nitchScalarOrder).Bytes()
	if len(b) < 48 {
		padded := make([]byte, 48)
		copy(padded[48-len(b):], b)
		b = padded
	}
	fr.FromBytes(b)
	return fr
}

func nitchHashToG1(engine *bls12381.Engine, message []byte) (*bls12381.PointG1, error) {
	return engine.G1.HashToCurve(message, []byte(nitchH1DST))
}

func nitchProveAdaptation(engine *bls12381.Engine, key *NITCHBLSKey, y, contribution *bls12381.PointG1) (*NITCHBLSProof, error) {
	g1, _, err := nitchGenerators(engine, key.PP)
	if err != nil {
		return nil, err
	}
	vk, err := engine.G1.FromBytes(key.VerificationKey)
	if err != nil {
		return nil, err
	}
	tau := nitchRandomScalar(key.PP.P)
	t1 := engine.G1.New()
	engine.G1.MulScalar(t1, g1, nitchBigIntToFr(tau))
	t2 := engine.G1.New()
	engine.G1.MulScalar(t2, y, nitchBigIntToFr(tau))
	challenge := nitchChallengeScalar(engine, key.PP.P, g1, y, vk, contribution, t1, t2)
	response := new(big.Int).Mul(challenge, key.SecretShare)
	response.Add(response, tau)
	response.Mod(response, key.PP.P)
	return &NITCHBLSProof{Challenge: challenge, Response: response}, nil
}

func nitchVerifyAdaptation(engine *bls12381.Engine, key *NITCHBLSKey, y, contribution *bls12381.PointG1, proof *NITCHBLSProof) bool {
	g1, _, err := nitchGenerators(engine, key.PP)
	if err != nil {
		return false
	}
	vk, err := engine.G1.FromBytes(key.VerificationKey)
	if err != nil {
		return false
	}
	g1z := engine.G1.New()
	engine.G1.MulScalar(g1z, g1, nitchBigIntToFr(proof.Response))
	vkC := engine.G1.New()
	engine.G1.MulScalar(vkC, vk, nitchBigIntToFr(proof.Challenge))
	t1 := engine.G1.New()
	engine.G1.Sub(t1, g1z, vkC)

	yz := engine.G1.New()
	engine.G1.MulScalar(yz, y, nitchBigIntToFr(proof.Response))
	contributionC := engine.G1.New()
	engine.G1.MulScalar(contributionC, contribution, nitchBigIntToFr(proof.Challenge))
	t2 := engine.G1.New()
	engine.G1.Sub(t2, yz, contributionC)

	expected := nitchChallengeScalar(engine, key.PP.P, g1, y, vk, contribution, t1, t2)
	return expected.Cmp(proof.Challenge) == 0
}

func nitchChallengeScalar(engine *bls12381.Engine, modulus *big.Int, points ...*bls12381.PointG1) *big.Int {
	h := sha256.New()
	for _, point := range points {
		h.Write(engine.G1.ToBytes(point))
	}
	return new(big.Int).Mod(new(big.Int).SetBytes(h.Sum(nil)), modulus)
}

func nitchEvaluationPoint(p *big.Int, numParties, index int) (*big.Int, error) {
	if index < 1 || index > numParties {
		return nil, fmt.Errorf("index must be in [1,%d]", numParties)
	}
	root, err := nitchPrimitiveRoot(p, numParties)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Exp(root, big.NewInt(int64(index)), p), nil
}

func nitchPrimitiveRoot(p *big.Int, n int) (*big.Int, error) {
	nitchRootCache.RLock()
	if root := nitchRootCache.roots[n]; root != nil {
		nitchRootCache.RUnlock()
		return new(big.Int).Set(root), nil
	}
	nitchRootCache.RUnlock()

	one := big.NewInt(1)
	pMinusOne := new(big.Int).Sub(p, one)
	nBig := big.NewInt(int64(n))
	if new(big.Int).Mod(pMinusOne, nBig).Sign() != 0 {
		return nil, fmt.Errorf("n does not divide scalar order minus one")
	}
	exp := new(big.Int).Div(pMinusOne, nBig)
	half := big.NewInt(int64(n / 2))
	for g := int64(2); g < 1<<20; g++ {
		candidate := new(big.Int).Exp(big.NewInt(g), exp, p)
		if new(big.Int).Exp(candidate, nBig, p).Cmp(one) != 0 {
			continue
		}
		if n > 1 && new(big.Int).Exp(candidate, half, p).Cmp(one) == 0 {
			continue
		}
		nitchRootCache.Lock()
		nitchRootCache.roots[n] = new(big.Int).Set(candidate)
		nitchRootCache.Unlock()
		return candidate, nil
	}
	return nil, fmt.Errorf("failed to find primitive %d-th root", n)
}

func nitchEvaluatePolynomial(coeffs []*big.Int, x, p *big.Int) *big.Int {
	result := new(big.Int)
	power := big.NewInt(1)
	for _, coeff := range coeffs {
		term := new(big.Int).Mul(coeff, power)
		result.Add(result, term)
		result.Mod(result, p)
		power.Mul(power, x)
		power.Mod(power, p)
	}
	return result
}

func nitchLagrangeAtZero(xs []*big.Int, p *big.Int) ([]*big.Int, error) {
	if len(xs) == 0 {
		return nil, errors.New("empty interpolation set")
	}
	out := make([]*big.Int, len(xs))
	for i, xi := range xs {
		if xi == nil || xi.Sign() == 0 {
			return nil, fmt.Errorf("invalid interpolation point %d", i)
		}
		num := big.NewInt(1)
		den := big.NewInt(1)
		for j, xj := range xs {
			if i == j {
				continue
			}
			if xi.Cmp(xj) == 0 {
				return nil, fmt.Errorf("duplicate interpolation point")
			}
			num.Mul(num, new(big.Int).Neg(xj))
			num.Mod(num, p)
			den.Mul(den, new(big.Int).Sub(xi, xj))
			den.Mod(den, p)
		}
		denInv := new(big.Int).ModInverse(den, p)
		if denInv == nil {
			return nil, fmt.Errorf("non-invertible interpolation denominator")
		}
		out[i] = new(big.Int).Mul(num, denInv)
		out[i].Mod(out[i], p)
	}
	return out, nil
}
