//go:build !nitchcgo
// +build !nitchcgo

package chameleon

import (
	"math/big"
)

// seam dispatch: default (pure) build routes the 8 protocol wrappers to the
// pure-Go implementations in hashseam_pure.go.
func seamCreateDistKGDeal(pp *PublicParameters, dealerIndex, t, n int) (*DistKGDeal, *DistKGDealPrivate, error) {
	return pureCreateDistKGDeal(pp, dealerIndex, t, n)
}

func seamVerifyDistKGShare(pp *PublicParameters, deal *DistKGDeal, shareSij *big.Int, receiverIndex, t, n int) (bool, error) {
	return pureVerifyDistKGShare(pp, deal, shareSij, receiverIndex, t, n)
}

func seamDistKGExtract(pp *PublicParameters, myIndex int, qual []int, deals map[int]*DistKGDeal, sharesToJ map[int]*big.Int, t, n int) (*NITCHKey, error) {
	return pureDistKGExtract(pp, myIndex, qual, deals, sharesToJ, t, n)
}

func seamCreateDKRSDeal(pp *PublicParameters, dealerIndex int, oldSecretShare *big.Int, oldThreshold int, newThreshold int, newN int) (*DKRSDeal, *DKRSDealPrivate, error) {
	return pureCreateDKRSDeal(pp, dealerIndex, oldSecretShare, oldThreshold, newThreshold, newN)
}

func seamVerifyDKRSShare(pp *PublicParameters, deal *DKRSDeal, shareSij *big.Int, receiverIndex, newT, newN int) (bool, error) {
	return pureVerifyDKRSShare(pp, deal, shareSij, receiverIndex, newT, newN)
}

func seamDKRSExtract(pp *PublicParameters, currentKey *NITCHKey, myNewIndex int, oldQual []int, deals map[int]*DKRSDeal, sharesToMe map[int]*big.Int, newThreshold, newN, oldN int) (*NITCHKey, error) {
	return pureDKRSExtract(pp, currentKey, myNewIndex, oldQual, deals, sharesToMe, newThreshold, newN, oldN)
}

func seamPartialAdapt(key *NITCHKey, m, mPrime Message) (*AdaptationShare, error) {
	return purePartialAdapt(key, m, mPrime)
}

func seamPartialVerify(key *NITCHKey, share *AdaptationShare, m, mPrime Message) bool {
	return purePartialVerify(key, share, m, mPrime)
}
