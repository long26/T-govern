//go:build nitchcgo
// +build nitchcgo

package chameleon

import (
	"math/big"
)

// seam dispatch: nitchcgo build routes the 8 protocol wrappers to the C++ core.
// The pure twins in hashseam_pure.go stay unexported and always compiled so
// A/B (pure vs cgo) differential tests can call them directly.
var (
	_ = pureCreateDistKGDeal // keep pure twin referenced for A/B tests
	_ = pureVerifyDistKGShare
	_ = pureDistKGExtract
	_ = pureCreateDKRSDeal
	_ = pureVerifyDKRSShare
	_ = pureDKRSExtract
	_ = purePartialAdapt
	_ = purePartialVerify
)

func seamCreateDistKGDeal(pp *PublicParameters, dealerIndex, t, n int) (*DistKGDeal, *DistKGDealPrivate, error) {
	return cgoCreateDistKGDeal(pp, dealerIndex, t, n)
}

func seamVerifyDistKGShare(pp *PublicParameters, deal *DistKGDeal, shareSij *big.Int, receiverIndex, t, n int) (bool, error) {
	return cgoVerifyDistKGShare(pp, deal, shareSij, receiverIndex, t, n)
}

func seamDistKGExtract(pp *PublicParameters, myIndex int, qual []int, deals map[int]*DistKGDeal, sharesToJ map[int]*big.Int, t, n int) (*NITCHKey, error) {
	return cgoDistKGExtract(pp, myIndex, qual, deals, sharesToJ, t, n)
}

func seamCreateDKRSDeal(pp *PublicParameters, dealerIndex int, oldSecretShare *big.Int, oldThreshold int, newThreshold int, newN int) (*DKRSDeal, *DKRSDealPrivate, error) {
	return cgoCreateDKRSDeal(pp, dealerIndex, oldSecretShare, oldThreshold, newThreshold, newN)
}

func seamVerifyDKRSShare(pp *PublicParameters, deal *DKRSDeal, shareSij *big.Int, receiverIndex, newT, newN int) (bool, error) {
	return cgoVerifyDKRSShare(pp, deal, shareSij, receiverIndex, newT, newN)
}

func seamDKRSExtract(pp *PublicParameters, currentKey *NITCHKey, myNewIndex int, oldQual []int, deals map[int]*DKRSDeal, sharesToMe map[int]*big.Int, newThreshold, newN, oldN int) (*NITCHKey, error) {
	return cgoDKRSExtract(pp, currentKey, myNewIndex, oldQual, deals, sharesToMe, newThreshold, newN, oldN)
}

func seamPartialAdapt(key *NITCHKey, m, mPrime Message) (*AdaptationShare, error) {
	return cgoPartialAdapt(key, m, mPrime)
}

func seamPartialVerify(key *NITCHKey, share *AdaptationShare, m, mPrime Message) bool {
	return cgoPartialVerify(key, share, m, mPrime)
}
