//go:build nitchcgo
// +build nitchcgo

// hashseam_cgo.go — cgo-backed implementations of the NITCH protocol seam.
//
// nitchcgo build requires: (cd chameleon/nitchcore && make)  — libnitchcore.a
// and c_api.h must exist before `go build -tags nitchcgo ./chameleon/`.
// There is no reliable compile-time probe for a stale archive, so a missing or
// out-of-date library surfaces as the link error for the nitch_* symbols.
//
// Each cgoXxx mirrors its pureXxx twin in hashseam_pure.go: the public wrapper
// in Chameleonhash.go has already validated the business rules (ranges, power
// of two, presence) and parsed t/n, so this layer only converts Go values to
// the C wire format, calls libnitchcore, and reassembles the Go result types.
// Conversion follows docs/nitchcore-c-api-contract.md: G1 = kilic 96B,
// G2 = kilic 192B (the C side owns the MIRACL 0x04 prefix), scalars = 32B
// big-endian, share/commitment lists = plain concatenation in QUAL/field order.
//
// Locking: the global kilic engine (pairing) is shared, so pairingMu is held
// ONLY around kilic FromBytes/ToBytes; it is never held across a C call. The
// C library keeps a per-thread context (thread_local nitch::MiraclBls12381,
// its own CSPRNG and FFT-domain cache) and needs no external locking.
package chameleon

/*
#cgo CXXFLAGS: -std=c++17 -O2
#cgo CFLAGS: -I${SRCDIR}/nitchcore
#cgo LDFLAGS: -L${SRCDIR}/nitchcore -lnitchcore -lstdc++
#include <stdlib.h>  // malloc/free for the Go-side C buffers
#include "c_api.h"
*/
import "C"

import (
	"fmt"
	"math/big"
	"unsafe"

	bls12381 "github.com/kilic/bls12-381"
)

// Wire sizes, kept in lockstep with c_api.h.
const (
	cgoScalarBytes = 32
	cgoG1Bytes     = 96
	cgoG2Bytes     = 192
)

// nitchError maps a NITCH_* return code to a Go error. Code 0 is the caller's
// responsibility (nil); every other code is always an error.
func nitchError(rc C.int) error {
	switch int(rc) {
	case 1:
		return ErrInvalidParameters
	case 2:
		return fmt.Errorf("nitchcore buffer too small")
	case 3:
		return fmt.Errorf("nitchcore crypto failure")
	case 4:
		return fmt.Errorf("nitchcore internal error")
	default:
		return fmt.Errorf("nitchcore unknown error code %d", int(rc))
	}
}

// cgoBuf wraps a C heap buffer freed on completion of the enclosing call.
type cgoBuf struct {
	ptr unsafe.Pointer
	len int
}

func cgoAlloc(n int) *cgoBuf {
	return &cgoBuf{ptr: C.malloc(C.size_t(n)), len: n}
}

func (b *cgoBuf) free() {
	if b != nil && b.ptr != nil {
		C.free(b.ptr)
		b.ptr = nil
	}
}

func (b *cgoBuf) bytes() []byte {
	// The C memory is only read after the call has returned and before free;
	// the slice is never stored or passed back to C.
	return (*[1 << 30]byte)(b.ptr)[:b.len:b.len]
}

func (b *cgoBuf) bytePtr() *C.uint8_t {
	return (*C.uint8_t)(b.ptr)
}

// cgoBytesToC returns a C copy of buf. Use for every C pointer that outlives
// the immediate expression, or simply always: it keeps the cgo rules trivially
// satisfied (no Go pointer is ever handed to C).
func cgoBytesToC(buf []byte) *C.uint8_t {
	if len(buf) == 0 {
		return nil
	}
	c := C.malloc(C.size_t(len(buf)))
	dst := (*[1 << 30]byte)(c)
	copy(dst[:len(buf)], buf)
	return (*C.uint8_t)(c)
}

func cgoFreeCBytes(p *C.uint8_t) {
	C.free(unsafe.Pointer(p))
}

// cgoScalarTo32 encodes s as exactly 32 big-endian bytes.
func cgoScalarTo32(s *big.Int) []byte {
	out := make([]byte, cgoScalarBytes)
	s.FillBytes(out)
	return out
}

// cgoScalarFrom32 decodes a 32-byte big-endian scalar; the wire value is the
// canonical residue mod r, which is what MIRACL's mod-order parse produces.
func cgoScalarFrom32(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

// cgoPointFromG1Bytes parses a 96B kilic point on the global engine. pairingMu
// is held only around the kilic call.
func cgoPointFromG1Bytes(b []byte) (*bls12381.PointG1, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairing.G1.FromBytes(b)
}

func cgoPointFromG2Bytes(b []byte) (*bls12381.PointG2, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairing.G2.FromBytes(b)
}

func cgoG1BytesFromPoint(p *bls12381.PointG1) []byte {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairing.G1.ToBytes(p)
}

func cgoG2BytesFromPoint(p *bls12381.PointG2) []byte {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairing.G2.ToBytes(p)
}

// ====================== DistKG (Protocol 1) cgo implementations ======================

// cgoCreateDistKGDeal is the cgo twin of pureCreateDistKGDeal. Randomness comes
// from the C side (the per-thread MIRACL CSPRNG), so the polynomial itself is
// not known to Go; Coeffs is therefore nil and Shares is reconstructed from the
// n*32B concatenation C returns. The public commitments are byte-identical to
// what the pure path would produce for the same polynomial.
func cgoCreateDistKGDeal(pp *PublicParameters, dealerIndex, t, n int) (*DistKGDeal, *DistKGDealPrivate, error) {
	if pp == nil {
		return nil, nil, ErrInvalidParameters
	}

	aLen := (t + 1) * cgoG1Bytes
	bufA := cgoAlloc(aLen)
	defer bufA.free()
	var outLenA C.uint32_t
	bufB0 := cgoAlloc(cgoG2Bytes)
	defer bufB0.free()
	var outLenB0 C.uint32_t
	sharesLen := n * cgoScalarBytes
	bufShares := cgoAlloc(sharesLen)
	defer bufShares.free()
	var outLenShares C.uint32_t

	rc := C.nitch_distkg_create_deal(
		C.int(dealerIndex), C.int(t), C.int(n),
		bufA.bytePtr(), C.uint32_t(aLen), &outLenA,
		bufB0.bytePtr(), C.uint32_t(cgoG2Bytes), &outLenB0,
		bufShares.bytePtr(), C.uint32_t(sharesLen), &outLenShares,
		nil, 0, nil) // out_p_hex: Go's pp is authoritative
	if rc != C.NITCH_OK {
		return nil, nil, nitchError(rc)
	}
	if int(outLenA) != aLen || int(outLenB0) != cgoG2Bytes || int(outLenShares) != sharesLen {
		return nil, nil, fmt.Errorf("nitchcore distkg create deal: unexpected output sizes a=%d b0=%d shares=%d", outLenA, outLenB0, outLenShares)
	}

	sharesRaw := bufShares.bytes()
	shares := make(map[int]*big.Int, n)
	for j := 1; j <= n; j++ {
		shares[j] = cgoScalarFrom32(sharesRaw[(j-1)*cgoScalarBytes : j*cgoScalarBytes])
	}

	// Coeffs: the C API deliberately never returns the polynomial, so Coeffs is
	// nil in general (the random coefficients exist only inside C). The one
	// exactly recoverable case is t=0, where the polynomial is the constant
	// a_0 = f(ω^j) for every j; populating it keeps the pure twin's documented
	// invariant (Coeffs[0] == self share) available on this path too.
	var coeffs []*big.Int
	if t == 0 {
		coeffs = []*big.Int{new(big.Int).Set(shares[1])}
	}

	// The public parameters travel with the deal exactly as in the pure twin:
	// all nodes must agree on the same (g1, g2, p).
	g1Bytes := cgoG1BytesFromPoint(pp.g1)
	g2Bytes := cgoG2BytesFromPoint(pp.g2)

	return &DistKGDeal{
			DealerIndex: big.NewInt(int64(dealerIndex)),
			Threshold:   big.NewInt(int64(t)),
			NumParties:  big.NewInt(int64(n)),
			A:           append([]byte(nil), bufA.bytes()...),
			B0:          append([]byte(nil), bufB0.bytes()...),
			G1Bytes:     g1Bytes,
			G2Bytes:     g2Bytes,
			P:           pp.p.Text(16),
		}, &DistKGDealPrivate{
			DealerIndex: dealerIndex,
			Coeffs:      coeffs,
			Shares:      shares,
		}, nil
}

// cgoVerifyDistKGShare is the cgo twin of pureVerifyDistKGShare.
func cgoVerifyDistKGShare(pp *PublicParameters, deal *DistKGDeal, shareSij *big.Int, receiverIndex, t, n int) (bool, error) {
	if deal == nil || shareSij == nil || len(deal.A) == 0 || len(deal.B0) == 0 {
		return false, ErrInvalidParameters
	}
	if len(deal.A) != (t+1)*g1PointByteSize {
		return false, fmt.Errorf("invalid deal A length")
	}

	cA := cgoBytesToC(deal.A)
	defer cgoFreeCBytes(cA)
	cB0 := cgoBytesToC(deal.B0)
	defer cgoFreeCBytes(cB0)
	cShare := cgoBytesToC(cgoScalarTo32(shareSij))
	defer cgoFreeCBytes(cShare)

	var valid C.int
	rc := C.nitch_distkg_verify_share(
		C.int(t), C.int(n), C.int(receiverIndex),
		cA, C.uint32_t(len(deal.A)),
		cB0,
		cShare,
		&valid)
	if rc != C.NITCH_OK {
		return false, nitchError(rc)
	}
	return valid != 0, nil
}

// cgoDistKGExtract is the cgo twin of pureDistKGExtract. The commitments are
// concatenated in QUAL order; the shares map is keyed by dealer index.
func cgoDistKGExtract(pp *PublicParameters, myIndex int, qual []int, deals map[int]*DistKGDeal, sharesToJ map[int]*big.Int, t, n int) (*NITCHKey, error) {
	if pp == nil || len(qual) == 0 {
		return nil, ErrInvalidParameters
	}
	for _, i := range qual {
		if deals[i] == nil || sharesToJ[i] == nil {
			return nil, ErrInvalidParameters
		}
	}

	b0s := make([]byte, 0, len(qual)*cgoG2Bytes)
	a0s := make([]byte, 0, len(qual)*cgoG1Bytes)
	shares := make([]byte, 0, len(qual)*cgoScalarBytes)
	for _, i := range qual {
		b0s = append(b0s, deals[i].B0...)
		a0s = append(a0s, deals[i].A[:g1PointByteSize]...) // A_0 = first 96B
		shares = append(shares, cgoScalarTo32(sharesToJ[i])...)
	}

	qualArr := make([]C.int32_t, len(qual))
	for idx, i := range qual {
		qualArr[idx] = C.int32_t(i)
	}

	cQual := (*C.int32_t)(unsafe.Pointer(&qualArr[0]))
	cB0s := cgoBytesToC(b0s)
	defer cgoFreeCBytes(cB0s)
	cA0s := cgoBytesToC(a0s)
	defer cgoFreeCBytes(cA0s)
	cShares := cgoBytesToC(shares)
	defer cgoFreeCBytes(cShares)

	bufPK := cgoAlloc(cgoG2Bytes)
	defer bufPK.free()
	var outLenPK C.uint32_t
	bufPKG1 := cgoAlloc(cgoG1Bytes)
	defer bufPKG1.free()
	var outLenPKG1 C.uint32_t
	bufVK := cgoAlloc(cgoG1Bytes)
	defer bufVK.free()
	var outLenVK C.uint32_t
	bufSK := cgoAlloc(cgoScalarBytes)
	defer bufSK.free()
	var outLenSK C.uint32_t

	rc := C.nitch_distkg_extract(
		C.int(myIndex), C.int(t), C.int(n),
		cQual, C.uint32_t(len(qualArr)),
		cB0s, cA0s, cShares,
		bufPK.bytePtr(), C.uint32_t(cgoG2Bytes), &outLenPK,
		bufPKG1.bytePtr(), C.uint32_t(cgoG1Bytes), &outLenPKG1,
		bufVK.bytePtr(), C.uint32_t(cgoG1Bytes), &outLenVK,
		bufSK.bytePtr(), C.uint32_t(cgoScalarBytes), &outLenSK)
	if rc != C.NITCH_OK {
		return nil, nitchError(rc)
	}
	if int(outLenPK) != cgoG2Bytes || int(outLenPKG1) != cgoG1Bytes ||
		int(outLenVK) != cgoG1Bytes || int(outLenSK) != cgoScalarBytes {
		return nil, fmt.Errorf("nitchcore distkg extract: unexpected output sizes")
	}

	pk, err := cgoPointFromG2Bytes(bufPK.bytes())
	if err != nil {
		return nil, fmt.Errorf("distkg extract pk: %v", err)
	}
	pkG1, err := cgoPointFromG1Bytes(bufPKG1.bytes())
	if err != nil {
		return nil, fmt.Errorf("distkg extract pkG1: %v", err)
	}
	vk, err := cgoPointFromG1Bytes(bufVK.bytes())
	if err != nil {
		return nil, fmt.Errorf("distkg extract vk: %v", err)
	}

	return &NITCHKey{
		PP:              pp,
		GlobalPK:        pk,
		GlobalPKG1:      pkG1,
		Threshold:       t,
		NumParties:      n,
		SecretShare:     cgoScalarFrom32(bufSK.bytes()),
		VerificationKey: vk,
		Index:           myIndex,
	}, nil
}

// ====================== DKRS (re-sharing) cgo implementations ======================

// cgoCreateDKRSDeal is the cgo twin of pureCreateDKRSDeal. As with the DistKG
// deal, the random higher coefficients stay inside C. coeff_0 is known to the
// caller (it IS oldSecretShare), so Coeffs is reconstructable exactly when
// newThreshold == 0 and is nil otherwise.
func cgoCreateDKRSDeal(pp *PublicParameters, dealerIndex int, oldSecretShare *big.Int, oldThreshold int, newThreshold int, newN int) (*DKRSDeal, *DKRSDealPrivate, error) {
	if pp == nil || oldSecretShare == nil {
		return nil, nil, ErrInvalidParameters
	}

	aLen := (newThreshold + 1) * cgoG1Bytes
	bufA := cgoAlloc(aLen)
	defer bufA.free()
	var outLenA C.uint32_t
	bufB0 := cgoAlloc(cgoG2Bytes)
	defer bufB0.free()
	var outLenB0 C.uint32_t
	sharesLen := newN * cgoScalarBytes
	bufShares := cgoAlloc(sharesLen)
	defer bufShares.free()
	var outLenShares C.uint32_t
	cOld := cgoBytesToC(cgoScalarTo32(oldSecretShare))
	defer cgoFreeCBytes(cOld)

	rc := C.nitch_dkrs_create_deal(
		C.int(dealerIndex), C.int(newThreshold), C.int(newN),
		cOld,
		bufA.bytePtr(), C.uint32_t(aLen), &outLenA,
		bufB0.bytePtr(), C.uint32_t(cgoG2Bytes), &outLenB0,
		bufShares.bytePtr(), C.uint32_t(sharesLen), &outLenShares)
	if rc != C.NITCH_OK {
		return nil, nil, nitchError(rc)
	}
	if int(outLenA) != aLen || int(outLenB0) != cgoG2Bytes || int(outLenShares) != sharesLen {
		return nil, nil, fmt.Errorf("nitchcore dkrs create deal: unexpected output sizes a=%d b0=%d shares=%d", outLenA, outLenB0, outLenShares)
	}

	sharesRaw := bufShares.bytes()
	shares := make(map[int]*big.Int, newN)
	for j := 1; j <= newN; j++ {
		shares[j] = cgoScalarFrom32(sharesRaw[(j-1)*cgoScalarBytes : j*cgoScalarBytes])
	}

	// Coeffs: nil in general (random terms never leave C). For newThreshold==0
	// the polynomial is the constant old_secret_share, which the caller already
	// holds, so it is restored verbatim.
	var coeffs []*big.Int
	if newThreshold == 0 {
		coeffs = []*big.Int{new(big.Int).Mod(new(big.Int).Set(oldSecretShare), pp.p)}
	}

	return &DKRSDeal{
			DealerIndex:   big.NewInt(int64(dealerIndex)),
			OldThreshold:  big.NewInt(int64(oldThreshold)),
			OldNumParties: big.NewInt(0),
			NewThreshold:  big.NewInt(int64(newThreshold)),
			NewNumParties: big.NewInt(int64(newN)),
			A:             append([]byte(nil), bufA.bytes()...),
			B0:            append([]byte(nil), bufB0.bytes()...),
		}, &DKRSDealPrivate{
			DealerIndex: dealerIndex,
			Coeffs:      coeffs,
			Shares:      shares,
		}, nil
}

// cgoVerifyDKRSShare is the cgo twin of pureVerifyDKRSShare.
func cgoVerifyDKRSShare(pp *PublicParameters, deal *DKRSDeal, shareSij *big.Int, receiverIndex, newT, newN int) (bool, error) {
	if deal == nil || shareSij == nil || len(deal.A) == 0 || len(deal.B0) != cgoG2Bytes {
		return false, ErrInvalidParameters
	}
	if len(deal.A) != (newT+1)*g1PointByteSize {
		return false, fmt.Errorf("invalid deal A length")
	}

	cA := cgoBytesToC(deal.A)
	defer cgoFreeCBytes(cA)
	cShare := cgoBytesToC(cgoScalarTo32(shareSij))
	defer cgoFreeCBytes(cShare)

	var valid C.int
	cB0 := cgoBytesToC(deal.B0)
	defer cgoFreeCBytes(cB0)
	rc := C.nitch_dkrs_verify_share(
		C.int(newT), C.int(newN), C.int(receiverIndex),
		cA, C.uint32_t(len(deal.A)),
		cB0, cShare,
		&valid)
	if rc != C.NITCH_OK {
		return false, nitchError(rc)
	}
	return valid != 0, nil
}

// cgoDKRSExtract is the cgo twin of pureDKRSExtract. Lagrange weighting happens
// over the old domain inside C; the global public key is inherited from
// currentKey (the C call does not recompute it), matching the pure twin.
func cgoDKRSExtract(pp *PublicParameters, currentKey *NITCHKey, myNewIndex int, oldQual []int, deals map[int]*DKRSDeal, sharesToMe map[int]*big.Int, newThreshold, newN, oldN int) (*NITCHKey, error) {
	if pp == nil || currentKey == nil || len(oldQual) == 0 {
		return nil, ErrInvalidParameters
	}
	for _, i := range oldQual {
		if i < 1 || i > oldN {
			return nil, fmt.Errorf("old qualifier index out of range: %d", i)
		}
		if deals[i] == nil {
			return nil, fmt.Errorf("missing deal for qualified member %d", i)
		}
		if sharesToMe[i] == nil {
			return nil, fmt.Errorf("missing share from %d for myself %d", i, myNewIndex)
		}
	}

	// b0s/a0s are accepted for signature parity and deliberately unused by the
	// C side (contract §2.3); they are still marshalled so the shape matches.
	b0s := make([]byte, 0, len(oldQual)*cgoG2Bytes)
	a0s := make([]byte, 0, len(oldQual)*cgoG1Bytes)
	shares := make([]byte, 0, len(oldQual)*cgoScalarBytes)
	for _, i := range oldQual {
		b0s = append(b0s, make([]byte, cgoG2Bytes)...)
		a0s = append(a0s, deals[i].A[:g1PointByteSize]...)
		shares = append(shares, cgoScalarTo32(sharesToMe[i])...)
	}

	qualArr := make([]C.int32_t, len(oldQual))
	for idx, i := range oldQual {
		qualArr[idx] = C.int32_t(i)
	}
	cQual := (*C.int32_t)(unsafe.Pointer(&qualArr[0]))
	cB0s := cgoBytesToC(b0s)
	defer cgoFreeCBytes(cB0s)
	cA0s := cgoBytesToC(a0s)
	defer cgoFreeCBytes(cA0s)
	cShares := cgoBytesToC(shares)
	defer cgoFreeCBytes(cShares)

	bufSK := cgoAlloc(cgoScalarBytes)
	defer bufSK.free()
	var outLenSK C.uint32_t
	bufVK := cgoAlloc(cgoG1Bytes)
	defer bufVK.free()
	var outLenVK C.uint32_t

	rc := C.nitch_dkrs_extract(
		C.int(myNewIndex), C.int(newThreshold), C.int(newN), C.int(oldN),
		cQual, C.uint32_t(len(qualArr)),
		cB0s, cA0s, cShares,
		bufSK.bytePtr(), C.uint32_t(cgoScalarBytes), &outLenSK,
		bufVK.bytePtr(), C.uint32_t(cgoG1Bytes), &outLenVK)
	if rc != C.NITCH_OK {
		return nil, nitchError(rc)
	}
	if int(outLenSK) != cgoScalarBytes || int(outLenVK) != cgoG1Bytes {
		return nil, fmt.Errorf("nitchcore dkrs extract: unexpected output sizes")
	}

	vk, err := cgoPointFromG1Bytes(bufVK.bytes())
	if err != nil {
		return nil, fmt.Errorf("dkrs extract vk: %v", err)
	}

	return &NITCHKey{
		PP:              pp,
		GlobalPK:        currentKey.GlobalPK,
		GlobalPKG1:      currentKey.GlobalPKG1,
		Threshold:       newThreshold,
		NumParties:      newN,
		SecretShare:     cgoScalarFrom32(bufSK.bytes()),
		VerificationKey: vk,
		Index:           myNewIndex,
	}, nil
}

// ====================== Adaptation cgo implementations ======================

// cgoPartialAdapt is the cgo twin of purePartialAdapt: c_i = y^{sk_i} plus the
// NIZK proof (challenge, response), with H1/H2 and the transcript built inside
// C. Only key.SecretShare, key.VerificationKey and key.Index are consumed; the
// messages pass through as raw bytes.
func cgoPartialAdapt(key *NITCHKey, m, mPrime Message) (*AdaptationShare, error) {
	if key == nil || key.SecretShare == nil || key.VerificationKey == nil {
		return nil, ErrInvalidParameters
	}

	vkBytes := cgoG1BytesFromPoint(key.VerificationKey)
	cVK := cgoBytesToC(vkBytes)
	defer cgoFreeCBytes(cVK)
	cSK := cgoBytesToC(cgoScalarTo32(key.SecretShare))
	defer cgoFreeCBytes(cSK)
	cSrc := cgoBytesToC(m)
	defer cgoFreeCBytes(cSrc)
	cTgt := cgoBytesToC(mPrime)
	defer cgoFreeCBytes(cTgt)

	bufC := cgoAlloc(cgoG1Bytes)
	defer bufC.free()
	var outLenC C.uint32_t
	bufOmega := cgoAlloc(cgoScalarBytes)
	defer bufOmega.free()
	var outLenOmega C.uint32_t
	bufZ := cgoAlloc(cgoScalarBytes)
	defer bufZ.free()
	var outLenZ C.uint32_t

	rc := C.nitch_partial_adapt(
		cSK, cVK, C.int(key.Index),
		cSrc, C.uint32_t(len(m)),
		cTgt, C.uint32_t(len(mPrime)),
		bufC.bytePtr(), C.uint32_t(cgoG1Bytes), &outLenC,
		bufOmega.bytePtr(), C.uint32_t(cgoScalarBytes), &outLenOmega,
		bufZ.bytePtr(), C.uint32_t(cgoScalarBytes), &outLenZ)
	if rc != C.NITCH_OK {
		return nil, nitchError(rc)
	}
	if int(outLenC) != cgoG1Bytes || int(outLenOmega) != cgoScalarBytes || int(outLenZ) != cgoScalarBytes {
		return nil, fmt.Errorf("nitchcore partial adapt: unexpected output sizes")
	}

	contribution, err := cgoPointFromG1Bytes(bufC.bytes())
	if err != nil {
		return nil, fmt.Errorf("partial adapt contribution: %v", err)
	}

	return &AdaptationShare{
		Index: big.NewInt(int64(key.Index)),
		Share: contribution,
		Proof: &NIZKProof{
			Omega: cgoScalarFrom32(bufOmega.bytes()),
			Z:     cgoScalarFrom32(bufZ.bytes()),
		},
	}, nil
}

// cgoPartialVerify is the cgo twin of purePartialVerify: it re-derives the NIZK
// challenge inside C and compares it with the proof's. As in the pure twin, any
// malformed input simply fails verification.
func cgoPartialVerify(key *NITCHKey, share *AdaptationShare, m, mPrime Message) bool {
	if key == nil || key.VerificationKey == nil || share == nil || share.Share == nil ||
		share.Proof == nil || share.Proof.Omega == nil || share.Proof.Z == nil {
		return false
	}

	vkBytes := cgoG1BytesFromPoint(key.VerificationKey)
	cVK := cgoBytesToC(vkBytes)
	defer cgoFreeCBytes(cVK)
	cC := cgoBytesToC(cgoG1BytesFromPoint(share.Share))
	defer cgoFreeCBytes(cC)
	cOmega := cgoBytesToC(cgoScalarTo32(share.Proof.Omega))
	defer cgoFreeCBytes(cOmega)
	cZ := cgoBytesToC(cgoScalarTo32(share.Proof.Z))
	defer cgoFreeCBytes(cZ)
	cSrc := cgoBytesToC(m)
	defer cgoFreeCBytes(cSrc)
	cTgt := cgoBytesToC(mPrime)
	defer cgoFreeCBytes(cTgt)

	var valid C.int
	rc := C.nitch_partial_verify(
		cVK,
		cSrc, C.uint32_t(len(m)),
		cTgt, C.uint32_t(len(mPrime)),
		cC, cOmega, cZ,
		&valid)
	return rc == C.NITCH_OK && valid != 0
}
