// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contracts

import (
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = abi.U256
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
)

// PermissionManagerV3ReqFields is an auto generated low-level Go binding around an user-defined struct.
type PermissionManagerV3ReqFields struct {
	Version    uint16
	Epoch      uint64
	ReqID      [32]byte
	BodyHashes [7][32]byte
}

// PermissionManagerV3TEERegistrationEvidence is an auto generated low-level Go binding around an user-defined struct.
type PermissionManagerV3TEERegistrationEvidence struct {
	SigPubKey        [32]byte
	VrfPubKey        [32]byte
	EnclavePK        [2][32]byte
	MrSigner         [32]byte
	QuoteHash        [32]byte
	RegisterCertHash [32]byte
	ChallengeHash    [32]byte
}

// ContractsABI is the input ABI used to generate the binding from.
const ContractsABI = "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"_roundBlocks\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"_roundTimeout\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"_minDeposit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"_maxValidators\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"_teeNum\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"activeSetHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32[]\",\"name\":\"hardwareIds\",\"type\":\"bytes32[]\"}],\"name\":\"ActiveSetCheckpointed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"activeSetHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"rotatedAt\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"finalityBlockNumber\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"finalityBlockHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"seed\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32[]\",\"name\":\"hardwareIds\",\"type\":\"bytes32[]\"}],\"name\":\"ActiveSetRotated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"submittedCount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"requiredCount\",\"type\":\"uint256\"}],\"name\":\"ActiveSetRotationDeferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256[4]\",\"name\":\"pubKey\",\"type\":\"uint256[4]\"}],\"name\":\"CommitteePubKeyRegistered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"pendingNonce\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"deposit\",\"type\":\"uint256\"}],\"name\":\"PendingTEECancelled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"version\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"pkCHHash\",\"type\":\"bytes32\"}],\"name\":\"PkCHSet\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"dReq\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"dPolicy\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"activeSetHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"pkCHHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"committeePubKeyVersion\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"expiresAt\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256[2]\",\"name\":\"signature\",\"type\":\"uint256[2]\"}],\"name\":\"RedactAuthorized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"rPrimeHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"proofHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"resultHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"piReqAHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"piReqA\",\"type\":\"bytes\"}],\"name\":\"RedactExecuted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"expiredAt\",\"type\":\"uint256\"}],\"name\":\"RedactExpired\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"dReq\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"dPolicy\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256[2]\",\"name\":\"signature\",\"type\":\"uint256[2]\"}],\"name\":\"RedactRejected\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"txID\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"mHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"mPrimeHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"posHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"metaHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"activeSetHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"pkCHHash\",\"type\":\"bytes32\"}],\"name\":\"RedactRequestPayloadRecorded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"HardwareId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"exitEpoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"deadlineBlock\",\"type\":\"uint256\"}],\"name\":\"TEEEraseProofDeadlineSet\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"HardwareId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"fromEpoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"nextEpoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"exitedAt\",\"type\":\"uint256\"}],\"name\":\"TEEExitPending\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"HardwareId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"eraseProofHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"exitedAt\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"refundedDeposit\",\"type\":\"uint256\"}],\"name\":\"TEEExited\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"MR_ENCLAVE\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"HardwareId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"enclavePKHash\",\"type\":\"bytes32\"}],\"name\":\"TEERegistered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"HardwareId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"vrfPubKey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"enclavePKHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"mrSigner\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"quoteHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"registerCertHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"challengeHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"registeredAt\",\"type\":\"uint256\"}],\"name\":\"TEERegistrationEvidenceRecorded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"HardwareId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"revokedAt\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"slashedAmount\",\"type\":\"uint256\"}],\"name\":\"TEERevoked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"currentEpoch\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"nextEpoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"deadlineBlock\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"candidateCount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"finalityBlockNumber\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"finalityBlockHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"seed\",\"type\":\"bytes32\"}],\"name\":\"TEERotationPreparationRequested\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"pendingNonce\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"deposit\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"challengeHash\",\"type\":\"bytes32\"}],\"name\":\"TEEStakePending\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"HardwareId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"vrfPubKey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"vrfResult\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"proofHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"submittedAtBlock\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"submittedAt\",\"type\":\"uint256\"}],\"name\":\"TEEVRFSubmitted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"mrEnclave\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"trusted\",\"type\":\"bool\"}],\"name\":\"TrustedMREnclaveSet\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"FINALITY_BLOCK_DELAY\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"MIN_DEPOSIT\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"REDACT_PROTOCOL_VERSION\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"ROTATION_PREPARE_BLOCKS\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"activeSetHashByEpoch\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"activeTEEs\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"cancelPendingTEE\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"committeePubKey\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"committeePubKeyVersion\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"dReq\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"dPolicy\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"pubKeyVersion\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"expiresAt\",\"type\":\"uint256\"}],\"name\":\"computeAuthorizationDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint16\",\"name\":\"version\",\"type\":\"uint16\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32[7]\",\"name\":\"bodyHashes\",\"type\":\"bytes32[7]\"}],\"internalType\":\"structPermissionManagerV3.ReqFields\",\"name\":\"req\",\"type\":\"tuple\"}],\"name\":\"computeDReq\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"teeOwner\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"eraseProofHash\",\"type\":\"bytes32\"}],\"name\":\"computeEraseProofDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"teeOwner\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"_pkCH\",\"type\":\"bytes\"}],\"name\":\"computePkCHDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"dReq\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"activeSetHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"piReqA\",\"type\":\"bytes\"}],\"name\":\"computeRedactionProofHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"dReq\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"dPolicy\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"pubKeyVersion\",\"type\":\"uint256\"}],\"name\":\"computeRejectionDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"slashAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"pubKeyVersion\",\"type\":\"uint256\"}],\"name\":\"computeRevocationDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"finalityBlockHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"computeSeed\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"dReq\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"sI\",\"type\":\"bytes\"}],\"name\":\"computeShareProofDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"vrfPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"seed\",\"type\":\"bytes32\"}],\"name\":\"computeVRFDigest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"currentEpoch\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"epochLength\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"eraseProofGraceBlocks\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"rPrimeHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"proofHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"resultHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"piReqA\",\"type\":\"bytes\"}],\"name\":\"executeRedaction\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"finalityBlockByEpoch\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"finalityBlockHashByEpoch\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"genesisTimestamp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"getActiveSet\",\"outputs\":[{\"internalType\":\"bytes32[]\",\"name\":\"\",\"type\":\"bytes32[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"getActiveSetHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getActiveTEEs\",\"outputs\":[{\"internalType\":\"bytes32[]\",\"name\":\"\",\"type\":\"bytes32[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getCandidateTEEs\",\"outputs\":[{\"internalType\":\"bytes32[]\",\"name\":\"\",\"type\":\"bytes32[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"}],\"name\":\"getEffectiveRedactStatus\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_owner\",\"type\":\"address\"}],\"name\":\"getPendingTEE\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getPkCHHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"}],\"name\":\"getRedactRequest\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"storedReqID\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"dReq\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"dPolicy\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"status\",\"type\":\"uint8\"},{\"internalType\":\"uint256\",\"name\":\"authorizedAt\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"executedAt\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"expiresAt\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"rejectReasonHash\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_owner\",\"type\":\"address\"}],\"name\":\"getRegistrationChallenge\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getSystemParams\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"committeeVersion\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"pkCHHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"currentPkCHVersion\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"configuredEpochLength\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"activeThreshold\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"activeLimit\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"}],\"name\":\"getTEEEnclavePK\",\"outputs\":[{\"internalType\":\"bytes32[2]\",\"name\":\"\",\"type\":\"bytes32[2]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"}],\"name\":\"getTEEExitInfo\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"pending\",\"type\":\"bool\"},{\"internalType\":\"address\",\"name\":\"teeOwner\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"exitEpoch\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"eraseProofDeadlineBlock\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"eraseProofHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"finalizedAt\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"}],\"name\":\"getTEEInfo\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"},{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"}],\"name\":\"getTEESelectionVRF\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"exists\",\"type\":\"bool\"},{\"internalType\":\"bytes32\",\"name\":\"vrfResult\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"proofHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"submittedAtBlock\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"submittedAt\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_owner\",\"type\":\"address\"}],\"name\":\"getTEEStatusByOwner\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"indexOfSigPubKey\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"}],\"name\":\"isEraseProofOverdue\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"hardwareId\",\"type\":\"bytes32\"}],\"name\":\"isHardwareIdRegistered\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"}],\"name\":\"isRedactionExecuted\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"isRotationPreparationPhase\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"isSigPubKeyRegistered\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"isSystemReady\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"lastRotationBlock\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"lastRotationTimestamp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"nT\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"nextPendingNonceOfOwner\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"pendingTEEOfOwner\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"deposit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"challengeHash\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pkCH\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pkCHVersion\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"prepareRotation\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256[4]\",\"name\":\"_committeePubKey\",\"type\":\"uint256[4]\"}],\"name\":\"registerCommitteePubKey\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_owner\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_pendingNonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"_MR_ENCLAVE\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"_HardwareId\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"vrfPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32[2]\",\"name\":\"enclavePK\",\"type\":\"bytes32[2]\"},{\"internalType\":\"bytes32\",\"name\":\"mrSigner\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"quoteHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"registerCertHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"challengeHash\",\"type\":\"bytes32\"}],\"internalType\":\"structPermissionManagerV3.TEERegistrationEvidence\",\"name\":\"evidence\",\"type\":\"tuple\"},{\"internalType\":\"uint256[2]\",\"name\":\"_signature\",\"type\":\"uint256[2]\"}],\"name\":\"registerTEE\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint16\",\"name\":\"version\",\"type\":\"uint16\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32[7]\",\"name\":\"bodyHashes\",\"type\":\"bytes32[7]\"}],\"internalType\":\"structPermissionManagerV3.ReqFields\",\"name\":\"req\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"dPolicy\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256[2]\",\"name\":\"signature\",\"type\":\"uint256[2]\"}],\"name\":\"rejectRedactRequest\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"reasonHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"slashAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256[2]\",\"name\":\"_signature\",\"type\":\"uint256[2]\"}],\"name\":\"revokeTEE\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"rotationDeadlineBlock\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"rotationPreparationRequested\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"seedByEpoch\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"_pkCH\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"name\":\"setPkCH\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"mrEnclave\",\"type\":\"bytes32\"},{\"internalType\":\"bool\",\"name\":\"trusted\",\"type\":\"bool\"}],\"name\":\"setTrustedMREnclave\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"sigPubKeyOfOwner\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"stakeTEE\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"pendingNonce\",\"type\":\"uint256\"}],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"steadyEpochLength\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"eraseProofHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"name\":\"submitEraseProof\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint16\",\"name\":\"version\",\"type\":\"uint16\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"reqID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32[7]\",\"name\":\"bodyHashes\",\"type\":\"bytes32[7]\"}],\"internalType\":\"structPermissionManagerV3.ReqFields\",\"name\":\"req\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"dPolicy\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"expiresAt\",\"type\":\"uint256\"},{\"internalType\":\"uint256[2]\",\"name\":\"signature\",\"type\":\"uint256[2]\"}],\"name\":\"submitRedactAuthorization\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"sigPubKey\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"vrfResult\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"vrfProof\",\"type\":\"bytes\"}],\"name\":\"submitTEESelectionVRF\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"tT\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"trustedMREnclave\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"trustedMREnclaveCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]"

// ContractsBin is the compiled bytecode used for deploying new contracts.
var ContractsBin = "0x6080604052600060055560006009556000600b553480156200002057600080fd5b50604051620107b3380380620107b383398181016040528101906200004691906200058f565b33600d60006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550826000819055508060018190555060008214620000a55781620000a8565b60015b6002819055508460038190555060008414620000c55783620000c7565b845b6004819055504260068190555042600781905550436008819055506032600c81905550620000fd6005546200010860201b60201c565b5050505050620008ec565b60006200011a6200018960201b60201c565b905060006200012f826200030c60201b60201c565b905080601a60008581526020019081526020016000208190555080837f9999a867bedf762df746f19073b4ffcfa489fc80ab3cbff37e142f2ead698ea6846040516200017c919062000724565b60405180910390a3505050565b6060600060198054905067ffffffffffffffff811115620001d3577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b604051908082528060200260200182016040528015620002025781602001602082028036833780820191505090505b50905060005b6019805490508110156200030457600e6019828154811062000253577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002001548154811062000296577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154828281518110620002e2577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010181815250508080620002fb906200084b565b91505062000208565b508091505090565b600080600190505b8251811015620004da5760008382815181106200035a577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151905060008290505b600081118015620003c95750818560018362000387919062000797565b81518110620003bf577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151115b156200047b5784600182620003df919062000797565b8151811062000417577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015185828151811062000459577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001018181525050808062000472906200081c565b9150506200036a565b81858281518110620004b6577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101818152505050508080620004d1906200084b565b91505062000314565b50606060005b835181101562000567578184828151811062000525577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015160405160200162000540929190620006f8565b604051602081830303815290604052915080806200055e906200084b565b915050620004e0565b508080519060200120915050919050565b6000815190506200058981620008d2565b92915050565b600080600080600060a08688031215620005a857600080fd5b6000620005b88882890162000578565b9550506020620005cb8882890162000578565b9450506040620005de8882890162000578565b9350506060620005f18882890162000578565b9250506080620006048882890162000578565b9150509295509295909350565b60006200061f838362000695565b60208301905092915050565b6000620006388262000758565b6200064481856200077b565b9350620006518362000748565b8060005b83811015620006885781516200066c888262000611565b975062000679836200076e565b92505060018101905062000655565b5085935050505092915050565b620006a081620007d2565b82525050565b620006bb620006b582620007d2565b62000899565b82525050565b6000620006ce8262000763565b620006da81856200078c565b9350620006ec818560208601620007e6565b80840191505092915050565b6000620007068285620006c1565b9150620007148284620006a6565b6020820191508190509392505050565b600060208201905081810360008301526200074081846200062b565b905092915050565b6000819050602082019050919050565b600081519050919050565b600081519050919050565b6000602082019050919050565b600082825260208201905092915050565b600081905092915050565b6000620007a482620007dc565b9150620007b183620007dc565b925082821015620007c757620007c6620008a3565b5b828203905092915050565b6000819050919050565b6000819050919050565b60005b8381101562000806578082015181840152602081019050620007e9565b8381111562000816576000848401525b50505050565b60006200082982620007dc565b9150600082141562000840576200083f620008a3565b5b600182039050919050565b60006200085882620007dc565b91507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff8214156200088e576200088d620008a3565b5b600182019050919050565b6000819050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b620008dd81620007dc565b8114620008e957600080fd5b50565b61feb780620008fc6000396000f3fe60806040526004361061043c5760003560e01c80637c48a1d911610234578063c898d32a1161012e578063e1e158a5116100b6578063ef64d1981161007a578063ef64d198146112f4578063f565ea5f14611331578063f9dd7ebb14611372578063fa7cfda0146113af578063fef06743146113ec5761043c565b8063e1e158a5146111e2578063e618c49e1461120d578063e728ef421461124a578063eb215f2814611287578063ec56dbb0146112b25761043c565b8063d046a7fa116100fd578063d046a7fa146110c3578063d1b6fddb146110ee578063d4b154621461112b578063d93e237e14611168578063df3e9516146111a55761043c565b8063c898d32a14611007578063ca5eb7bc14611044578063cab43ea81461106d578063cacf66ab146110985761043c565b8063ac8f553f116101bc578063c2dbb46f11610180578063c2dbb46f14610ed2578063c4436aa914610f13578063c490095014610f50578063c64aa5d614610f8d578063c82a63bc14610fca5761043c565b8063ac8f553f14610dc5578063b113a4cb14610df0578063b1f2dd9514610e1b578063b27174b314610e58578063be693f3e14610e955761043c565b8063896ae02611610203578063896ae02614610cdc5780638c4aad5114610d075780638c6df62214610d325780638da5cb5b14610d5d5780638ec6ef7414610d885761043c565b80637c48a1d914610bfa5780637c94ada414610c255780637cc27d5714610c625780638364c1a114610c9f5761043c565b8063403803481161034557806353dc03dd116102cd578063674d9ec411610291578063674d9ec414610aed5780636a495d1c14610b2a5780636f1e0ae414610b5557806372a4330e14610b925780637667180814610bcf5761043c565b806353dc03dd146109f257806357d775f814610a1d578063587d9d5f14610a485780635f2e476714610a8557806361cc926614610ab05761043c565b8063473368911161031457806347336891146108cb57806348655344146109085780634c6e9a0b146109455780634ea609321461098a57806351a4111b146109b55761043c565b8063403803481461080357806340b74edd1461083457806341d7aa2814610875578063448c4fc0146108a05761043c565b80632118700d116103c85780632fce7cf4116103975780632fce7cf4146106e457806338279581146107215780633c4986471461075e5780633d700f281461079b5780633e3328b4146107d85761043c565b80632118700d14610612578063285ebd4c146106515780632e9c3bb81461067c5780632f2c8165146106a75761043c565b80631af8267f1161040f5780631af8267f146104ff5780631d3e33f21461051d5780631ddb9ba01461055b5780631e5cccd8146105985780631ed1869c146105d55761043c565b806305e9719a14610441578063083e11101461047e578063093ebf74146104a95780630a3ab49e146104d4575b600080fd5b34801561044d57600080fd5b506104686004803603810190610463919061bf28565b611429565b604051610475919061e3ae565b60405180910390f35b34801561048a57600080fd5b50610493611448565b6040516104a0919061f6bc565b60405180910390f35b3480156104b557600080fd5b506104be611464565b6040516104cb919061e3ae565b60405180910390f35b3480156104e057600080fd5b506104e9611473565b6040516104f6919061f6bc565b60405180910390f35b610507611479565b604051610514919061f6bc565b60405180910390f35b34801561052957600080fd5b50610544600480360381019061053f919061bdb2565b61172f565b60405161055292919061f8f1565b60405180910390f35b34801561056757600080fd5b50610582600480360381019061057d919061c1f1565b611787565b60405161058f919061e4d0565b60405180910390f35b3480156105a457600080fd5b506105bf60048036038101906105ba919061c5c6565b6117e2565b6040516105cc919061e3ae565b60405180910390f35b3480156105e157600080fd5b506105fc60048036038101906105f7919061bf7a565b611cdd565b604051610609919061e4d0565b60405180910390f35b34801561061e57600080fd5b506106396004803603810190610634919061bdb2565b611d3c565b6040516106489392919061f7f2565b60405180910390f35b34801561065d57600080fd5b50610666611d66565b604051610673919061e3ae565b60405180910390f35b34801561068857600080fd5b50610691611daf565b60405161069e919061f6bc565b60405180910390f35b3480156106b357600080fd5b506106ce60048036038101906106c9919061c563565b611db5565b6040516106db919061e4d0565b60405180910390f35b3480156106f057600080fd5b5061070b6004803603810190610706919061c12b565b611e14565b604051610718919061e4d0565b60405180910390f35b34801561072d57600080fd5b506107486004803603810190610743919061bdb2565b611e6f565b604051610755919061e4d0565b60405180910390f35b34801561076a57600080fd5b506107856004803603810190610780919061c18e565b611ed4565b604051610792919061e3ae565b60405180910390f35b3480156107a757600080fd5b506107c260048036038101906107bd919061c4fe565b6124a9565b6040516107cf919061e3ae565b60405180910390f35b3480156107e457600080fd5b506107ed6124c9565b6040516107fa919061f6bc565b60405180910390f35b34801561080f57600080fd5b506108186124cf565b60405161082b979695949392919061f700565b60405180910390f35b34801561084057600080fd5b5061085b6004803603810190610856919061bdb2565b612510565b60405161086c95949392919061e47d565b60405180910390f35b34801561088157600080fd5b5061088a6125d2565b604051610897919061f6bc565b60405180910390f35b3480156108ac57600080fd5b506108b56125d8565b6040516108c2919061f6a1565b60405180910390f35b3480156108d757600080fd5b506108f260048036038101906108ed919061c4fe565b6125dd565b6040516108ff919061e4d0565b60405180910390f35b34801561091457600080fd5b5061092f600480360381019061092a919061c407565b6125f5565b60405161093c919061e3ae565b60405180910390f35b34801561095157600080fd5b5061096c6004803603810190610967919061bf28565b612c54565b6040516109819998979695949392919061eaf6565b60405180910390f35b34801561099657600080fd5b5061099f612d54565b6040516109ac919061e3ae565b60405180910390f35b3480156109c157600080fd5b506109dc60048036038101906109d7919061c46e565b612eea565b6040516109e9919061e3ae565b60405180910390f35b3480156109fe57600080fd5b50610a0761376e565b604051610a14919061e3ae565b60405180910390f35b348015610a2957600080fd5b50610a326137bd565b604051610a3f919061f6bc565b60405180910390f35b348015610a5457600080fd5b50610a6f6004803603810190610a6a919061bf28565b6137c3565b604051610a7c919061f8d6565b60405180910390f35b348015610a9157600080fd5b50610a9a6137e7565b604051610aa7919061f6bc565b60405180910390f35b348015610abc57600080fd5b50610ad76004803603810190610ad2919061c4fe565b6137ed565b604051610ae4919061e371565b60405180910390f35b348015610af957600080fd5b50610b146004803603810190610b0f919061bf28565b61387f565b604051610b21919061e3ae565b60405180910390f35b348015610b3657600080fd5b50610b3f613891565b604051610b4c919061f6bc565b60405180910390f35b348015610b6157600080fd5b50610b7c6004803603810190610b77919061c4fe565b613897565b604051610b89919061e4d0565b60405180910390f35b348015610b9e57600080fd5b50610bb96004803603810190610bb4919061c2e8565b6138af565b604051610bc6919061e3ae565b60405180910390f35b348015610bdb57600080fd5b50610be4613d47565b604051610bf1919061f6bc565b60405180910390f35b348015610c0657600080fd5b50610c0f613d4d565b604051610c1c919061f6bc565b60405180910390f35b348015610c3157600080fd5b50610c4c6004803603810190610c47919061c4fe565b613d53565b604051610c59919061e4d0565b60405180910390f35b348015610c6e57600080fd5b50610c896004803603810190610c84919061bdb2565b613d6b565b604051610c96919061e4d0565b60405180910390f35b348015610cab57600080fd5b50610cc66004803603810190610cc1919061bfdd565b613d83565b604051610cd3919061e3ae565b60405180910390f35b348015610ce857600080fd5b50610cf1613f53565b604051610cfe919061f6bc565b60405180910390f35b348015610d1357600080fd5b50610d1c613f58565b604051610d29919061ebff565b60405180910390f35b348015610d3e57600080fd5b50610d47613fe6565b604051610d54919061f6bc565b60405180910390f35b348015610d6957600080fd5b50610d72613fec565b604051610d7f919061e28f565b60405180910390f35b348015610d9457600080fd5b50610daf6004803603810190610daa919061c3dd565b614012565b604051610dbc919061e4d0565b60405180910390f35b348015610dd157600080fd5b50610dda614099565b604051610de7919061f6bc565b60405180910390f35b348015610dfc57600080fd5b50610e0561409e565b604051610e12919061e371565b60405180910390f35b348015610e2757600080fd5b50610e426004803603810190610e3d919061c4fe565b6143b7565b604051610e4f919061f6bc565b60405180910390f35b348015610e6457600080fd5b50610e7f6004803603810190610e7a919061bf28565b6143d2565b604051610e8c919061f6bc565b60405180910390f35b348015610ea157600080fd5b50610ebc6004803603810190610eb7919061c4fe565b6143ea565b604051610ec9919061f6bc565b60405180910390f35b348015610ede57600080fd5b50610ef96004803603810190610ef4919061c527565b614402565b604051610f0a95949392919061e42a565b60405180910390f35b348015610f1f57600080fd5b50610f3a6004803603810190610f35919061c0ab565b61446c565b604051610f47919061e4d0565b60405180910390f35b348015610f5c57600080fd5b50610f776004803603810190610f72919061c4fe565b6144a8565b604051610f84919061f6bc565b60405180910390f35b348015610f9957600080fd5b50610fb46004803603810190610faf919061c254565b6144cc565b604051610fc1919061e4d0565b60405180910390f35b348015610fd657600080fd5b50610ff16004803603810190610fec919061c019565b614502565b604051610ffe919061e3ae565b60405180910390f35b34801561101357600080fd5b5061102e6004803603810190611029919061bddb565b6148dc565b60405161103b919061e4d0565b60405180910390f35b34801561105057600080fd5b5061106b6004803603810190611066919061c368565b61494d565b005b34801561107957600080fd5b50611082614c33565b60405161108f919061e4d0565b60405180910390f35b3480156110a457600080fd5b506110ad614c51565b6040516110ba919061f6bc565b60405180910390f35b3480156110cf57600080fd5b506110d8614c57565b6040516110e5919061e371565b60405180910390f35b3480156110fa57600080fd5b506111156004803603810190611110919061bdb2565b614dd1565b604051611122919061f6bc565b60405180910390f35b34801561113757600080fd5b50611152600480360381019061114d919061bf28565b614de9565b60405161115f919061e3ae565b60405180910390f35b34801561117457600080fd5b5061118f600480360381019061118a919061c4fe565b614e89565b60405161119c919061e4d0565b60405180910390f35b3480156111b157600080fd5b506111cc60048036038101906111c7919061beff565b614ea6565b6040516111d9919061e3ae565b60405180910390f35b3480156111ee57600080fd5b506111f7615179565b604051611204919061f6bc565b60405180910390f35b34801561121957600080fd5b50611234600480360381019061122f919061be33565b61517f565b604051611241919061e3ae565b60405180910390f35b34801561125657600080fd5b50611271600480360381019061126c919061bf28565b615cd5565b60405161127e919061e3ae565b60405180910390f35b34801561129357600080fd5b5061129c615cf5565b6040516112a9919061f6bc565b60405180910390f35b3480156112be57600080fd5b506112d960048036038101906112d4919061bf28565b615cfb565b6040516112eb9695949392919061e3c9565b60405180910390f35b34801561130057600080fd5b5061131b6004803603810190611316919061c2ac565b615d7c565b604051611328919061e4d0565b60405180910390f35b34801561133d57600080fd5b506113586004803603810190611353919061bf28565b615db1565b60405161136995949392919061e514565b60405180910390f35b34801561137e57600080fd5b506113996004803603810190611394919061bf28565b616083565b6040516113a6919061e356565b60405180910390f35b3480156113bb57600080fd5b506113d660048036038101906113d1919061c1f1565b61618c565b6040516113e3919061e4d0565b60405180910390f35b3480156113f857600080fd5b50611413600480360381019061140e919061bf28565b6161eb565b604051611420919061e3ae565b60405180910390f35b6000806012600084815260200190815260200160002054119050919050565b600061145261620b565b60085461145f919061fa5a565b905090565b600061146e616237565b905090565b60045481565b6000611483616237565b5061148c6163a3565b611494616c94565b61149d336170aa565b156114dd576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016114d49061f161565b60405180910390fd5b60006114e8336170f9565b509050801561152c576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016115239061f2e1565b60405180910390fd5b600054341015611571576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016115689061ef41565b60405180910390fd5b6001601160003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020546115bd919061fa5a565b915081601160003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002081905550600080431461161f57600143611619919061fae7565b40611624565b6000801b5b9050600030338546434287604051602001611645979695949392919061df4c565b604051602081830303815290604052805190602001209050604051806060016040528034815260200185815260200182815250600f60003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082015181600001556020820151816001015560408201518160020155905050833373ffffffffffffffffffffffffffffffffffffffff167fad63e94b43db4b75a69838f521c653d3d7fbd9dcd64828a05c37fd3b96c5edaa348460405161172192919061f6d7565b60405180910390a350505090565b60008060008061173e8561719e565b9150915081600581111561177b577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b81935093505050915091565b60007fdd00ef461faaeb3a1c7b1a67dbd918b39c72ff9d63136114d5f81f76f4398dd7858585856040516020016117c295949392919061e806565b604051602081830303815290604052805190602001209050949350505050565b60006117ec616237565b5060016005546117fc919061fa5a565b861461183d576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016118349061ee81565b60405180910390fd5b6000801b601b6000888152602001908152602001600020541415611896576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161188d9061ef81565b60405180910390fd5b61189e61376e565b806118c257506118ac6173c1565b80156118c157506001546118bf876173f1565b105b5b611901576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016118f89061f021565b60405180910390fd5b6013600086815260200190815260200160002060009054906101000a900460ff16611961576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016119589061f501565b60405180910390fd5b61196a85617660565b6119a9576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016119a09061f481565b60405180910390fd5b6000801b8414156119ef576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016119e69061f601565b60405180910390fd5b60008383905011611a35576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401611a2c9061f3a1565b60405180910390fd5b601f6000878152602001908152602001600020600086815260200190815260200160002060000160009054906101000a900460ff1615611aaa576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401611aa19061f4a1565b60405180910390fd5b6000601460008781526020019081526020016000205490506000600e8281548110611afe577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201905060026005811115611b48577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff166005811115611b92577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b1480611c1f575060036005811115611bd3577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff166005811115611c1d577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b145b611c5e576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401611c559061f381565b60405180910390fd5b6000801b81600301541415611ca8576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401611c9f9061f121565b60405180910390fd5b6000611cbc898984600301548a8a8a61771b565b9050611cc66163a3565b611cce616c94565b80935050505095945050505050565b60007fa699531be5f4d4a960fddf6ec93bfabe9ebaf4f27799b223ed509283c2d72e95463087878787604051602001611d1c979695949392919061e8ac565b604051602081830303815290604052805190602001209050949350505050565b600f6020528060005260406000206000915090508060000154908060010154908060020154905083565b600080600a8054611d769061fc1a565b9050118015611d8757506000600254115b8015611d97575060025460015410155b8015611daa575060025460198054905010155b905090565b60085481565b60007f0bc88877d8ce37d169235b88025357830ed8635cead8bbac92dc0aead63abf56463087878787604051602001611df4979695949392919061e9fa565b604051602081830303815290604052805190602001209050949350505050565b60007f5ae3b162a1b9352df05edf4a51fc2f3b09ae1bdb362dbae517bee4cc384986ae85858585604051602001611e4f95949392919061e6f5565b604051602081830303815290604052805190602001209050949350505050565b6000611e7a826170aa565b611e89576000801b9050611ecf565b600f60008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206002015490505b919050565b6000611ede616237565b50611ee76163a3565b6013600086815260200190815260200160002060009054906101000a900460ff16611f47576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401611f3e9061f501565b60405180910390fd5b6000801b841415611f8d576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401611f849061ef21565b60405180910390fd5b6000611f9d86868660095461618c565b9050611fa981846178e5565b611fe8576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401611fdf9061ef61565b60405180910390fd5b6000601460008881526020019081526020016000205490506000600e828154811061203c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201905060026005811115612086577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff1660058111156120d0577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b148061215d575060036005811115612111577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff16600581111561215b577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b145b806121e957506004600581111561219d577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff1660058111156121e7577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b145b612228576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161221f9061f421565b60405180910390fd5b60046005811115612262577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff1660058111156122ac577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b14156122fb576122bb88617ba7565b6122fa576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016122f19061f141565b60405180910390fd5b5b8060070154861115612342576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016123399061ed81565b60405180910390fd5b61234b88617bf9565b5085816007015461235c919061fae7565b816007018190555060058160090160006101000a81548160ff021916908360058111156123b2577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b02179055506020600089815260200190815260200160002060000160009054906101000a900460ff1615612410576000602060008a815260200190815260200160002060000160006101000a81548160ff0219169083151502179055505b61241b600554617ee9565b8060060160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16887fb73664d1974bb21f331e31a47199c4e3a57edd81d26b1b05d3ea4a8429a25d4389428a60405161248a9392919061eb83565b60405180910390a361249a616c94565b60019350505050949350505050565b601e6020528060005260406000206000915054906101000a900460ff1681565b60025481565b60008060008060008060006009546124e5614c33565b600b546124f061620b565b600554600254600154965096509650965096509650965090919293949596565b6000806000806000612521866170aa565b61253e576000806000806000801b945094509450945094506125c9565b6000600f60008873ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020604051806060016040529081600082015481526020016001820154815260200160028201548152505090506001816000015182602001516000846040015195509550955095509550505b91939590929450565b60015481565b600181565b601b6020528060005260406000206000915090505481565b60006125ff616237565b506126086163a3565b612610616c94565b600161ffff16856000016020810190612629919061c4d5565b61ffff161461266d576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016126649061f4e1565b60405180910390fd5b6000801b856040013514156126b7576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016126ae9061f101565b60405180910390fd5b6000801b8314156126fd576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016126f49061ef21565b60405180910390fd5b60006004811115612737577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b602160008760400135815260200190815260200160002060030160009054906101000a900460ff166004811115612797577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b146127d7576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016127ce9061eca1565b60405180910390fd5b6005548560200160208101906127ed919061c646565b67ffffffffffffffff1614612837576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161282e9061ee81565b60405180910390fd5b600061284286614012565b90506000612854828787600954611e14565b905061286081856178e5565b61289f576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016128969061f461565b60405180910390fd5b6040518060a001604052808860200160208101906128bd919061c646565b67ffffffffffffffff1681526020018381526020018860600160056007811061290f577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020020135815260200160036004811115612953577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b81526020016000815250602160008960400135815260200190815260200160002060008201518160000155602082015181600101556040820151816002015560608201518160030160006101000a81548160ff021916908360048111156129e3577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b02179055506080820151816004015590505086604001357fcc41da18ef531d7ab13d9c2b6b27e7edec2ee91ba944920fce962ca335e6ca7988606001600060078110612a58577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002013589606001600160078110612a9a577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358a606001600260078110612adc577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358b606001600360078110612b1e577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358c606001600460078110612b60577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358d606001600560078110612ba2577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358e606001600660078110612be4577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020020135604051612bfc979695949392919061e5ac565b60405180910390a286604001357f3e8bb8f721df92ea6a48860bcaf3465b3d7bfb740cdb9387f08c2a72add5cf0283888888604051612c3e949392919061e567565b60405180910390a2600192505050949350505050565b600080600080600080600080600080602160008c815260200190815260200160002090506000806004811115612cb3577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8260030160009054906101000a900460ff166004811115612cfd577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b14612d08578b612d0d565b6000801b5b905080826000015483600101546000801b612d2786617f58565b60008088600401546000801b9a509a509a509a509a509a509a509a509a5050509193959799909294969850565b6000612d5e616237565b50612d676163a3565b612d6f616c94565b612d78336170aa565b612db7576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401612dae9061f301565b60405180910390fd5b6000600f60003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060405180606001604052908160008201548152602001600182015481526020016002820154815250509050600f60003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600080820160009055600182016000905560028201600090555050612e8b338260000151618096565b80602001513373ffffffffffffffffffffffffffffffffffffffff167f5463fb04a95f5fe5300ab0be9d3632d0f4b30b6d9d5d9eae3bf3e664612331bd8360000151604051612eda919061f6bc565b60405180910390a3600191505090565b6000612ef4616237565b50612efd6163a3565b612f05616c94565b612f0d611d66565b612f4c576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401612f439061f5c1565b60405180910390fd5b600161ffff16856000016020810190612f65919061c4d5565b61ffff1614612fa9576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401612fa09061f4e1565b60405180910390fd5b6000801b85604001351415612ff3576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401612fea9061f101565b60405180910390fd5b6000600481111561302d577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b602160008760400135815260200190815260200160002060030160009054906101000a900460ff16600481111561308d577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b146130cd576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016130c49061eca1565b60405180910390fd5b6005548560200160208101906130e3919061c646565b67ffffffffffffffff161461312d576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016131249061ee81565b60405180910390fd5b601a6000866020016020810190613144919061c646565b67ffffffffffffffff168152602001908152602001600020548560600160056007811061319a577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020020135146131df576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016131d69061ede1565b60405180910390fd5b6131e7614c33565b85606001600660078110613224577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002013514613269576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016132609061f5a1565b60405180910390fd5b4283116132ab576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016132a29061ed01565b60405180910390fd5b60006132b686614012565b905060006132c8828760095488611787565b90506132d481856178e5565b613313576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161330a9061f401565b60405180910390fd5b6040518060a00160405280886020016020810190613331919061c646565b67ffffffffffffffff16815260200183815260200188606001600560078110613383577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358152602001600160048111156133c7577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b815260200186815250602160008960400135815260200190815260200160002060008201518160000155602082015181600101556040820151816002015560608201518160030160006101000a81548160ff02191690836004811115613456577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b02179055506080820151816004015590505086604001357fcc41da18ef531d7ab13d9c2b6b27e7edec2ee91ba944920fce962ca335e6ca79886060016000600781106134cb577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358960600160016007811061350d577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358a60600160026007811061354f577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358b606001600360078110613591577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358c6060016004600781106135d3577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358d606001600560078110613615577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358e606001600660078110613657577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002013560405161366f979695949392919061e5ac565b60405180910390a286602001602081019061368a919061c646565b67ffffffffffffffff1687604001357fada59ac183a19b56e18e22bb2912fe051d8fcd2b8762ccfddbea3eb6d2b3665284898b6060016005600781106136f9577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201358c60600160066007811061373b577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201356009548c8c604051613758979695949392919061e748565b60405180910390a3600192505050949350505050565b60008061377961620b565b9050600081141561378e5760009150506137ba565b6000613798611448565b905080431080156137b55750806014436137b2919061fa5a565b10155b925050505b90565b60035481565b60006137e060216000848152602001908152602001600020617f58565b9050919050565b600c5481565b6060600554821461386f57600067ffffffffffffffff811115613839577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b6040519080825280602002602001820160405280156138675781602001602082028036833780820191505090505b50905061387a565b613877618156565b90505b919050565b600061388a82617ba7565b9050919050565b60095481565b601a6020528060005260406000206000915090505481565b60006138b9616237565b506138c26163a3565b6138ca616c94565b6013600087815260200190815260200160002060009054906101000a900460ff1661392a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016139219061f501565b60405180910390fd5b61393386617660565b613972576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016139699061ee61565b60405180910390fd5b6005548511156139b7576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016139ae9061eea1565b60405180910390fd5b6000801b8414156139fd576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016139f49061f361565b60405180910390fd5b6000601460008881526020019081526020016000205490506000600e8281548110613a51577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201905060026005811115613a9b577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff166005811115613ae5577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b1480613b72575060036005811115613b26577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff166005811115613b70577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b145b80613bfe575060046005811115613bb2577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160090160009054906101000a900460ff166005811115613bfc577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b145b613c3d576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401613c349061efe1565b60405180910390fd5b6000816007015411613c84576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401613c7b9061f1e1565b60405180910390fd5b613c8f8833896182d0565b613ced613c9e89338a8a611cdd565b826002015487878080601f016020809104026020016040519081016040528093929190818152602001838380828437600081840152601f19601f820116905080830192505050505050506183e2565b613d2c576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401613d239061ec61565b60405180910390fd5b613d38888389896185ec565b60019250505095945050505050565b60055481565b600b5481565b601d6020528060005260406000206000915090505481565b60106020528060005260406000206000915090505481565b6000600d60009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff1614613e15576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401613e0c9061f3c1565b60405180910390fd5b613e1d616237565b50613e266163a3565b613e2e616c94565b6000801b831415613e74576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401613e6b9061f261565b60405180910390fd5b8115156022600085815260200190815260200160002060009054906101000a900460ff1615151415613ea95760019050613f4d565b816022600085815260200190815260200160002060006101000a81548160ff0219169083151502179055508115613ef75760236000815480929190613eed9061fc4c565b9190505550613f10565b60236000815480929190613f0a9061fbf0565b91905055505b827f1b9179620a1bcffa69dfd36d35e5064af9eb1c5e8a1af027744c43b9f994d6b483604051613f40919061e3ae565b60405180910390a2600190505b92915050565b601481565b600a8054613f659061fc1a565b80601f0160208091040260200160405190810160405280929190818152602001828054613f919061fc1a565b8015613fde5780601f10613fb357610100808354040283529160200191613fde565b820191906000526020600020905b815481529060010190602001808311613fc157829003601f168201915b505050505081565b60235481565b600d60009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b60007fdf048c27bb08241e87db7e18ea633d9c18fd09a38bbe462809dacf852d10ce52463084600001602081019061404a919061c4d5565b85602001602081019061405d919061c646565b86604001358760600160405160200161407c979695949392919061e98a565b604051602081830303815290604052805190602001209050919050565b600681565b60606000805b600e805490508110156141a457600260058111156140eb577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e8281548110614125577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff16600581111561417c577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b141561419157818061418d9061fc4c565b9250505b808061419c9061fc4c565b9150506140a4565b5060008167ffffffffffffffff8111156141e7577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b6040519080825280602002602001820160405280156142155781602001602082028036833780820191505090505b5090506000805b600e805490508110156143ad5760026005811115614263577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e828154811061429d577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff1660058111156142f4577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b141561439a57600e8181548110614334577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a02016002015483838151811061437f577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101818152505081806143969061fc4c565b9250505b80806143a59061fc4c565b91505061421c565b5081935050505090565b601581600481106143c757600080fd5b016000915090505481565b60146020528060005260406000206000915090505481565b601c6020528060005260406000206000915090505481565b600080600080600080601f6000898152602001908152602001600020600088815260200190815260200160002090508060000160009054906101000a900460ff16816001015482600201548360030154846004015495509550955095509550509295509295909350565b6000858585858560405160200161448795949392919061e7b8565b60405160208183030381529060405280519060200120905095945050505050565b601981815481106144b857600080fd5b906000526020600020016000915090505481565b60008383836040516020016144e39392919061e069565b6040516020818303038152906040528051906020012090509392505050565b600061450c616237565b506145156163a3565b61451d616c94565b60006021600089815260200190815260200160002090506001600481111561456e577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8160030160009054906101000a900460ff1660048111156145b8577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b146145f8576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016145ef9061f281565b60405180910390fd5b806004015442111561469d5760048160030160006101000a81548160ff02191690836004811115614652577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b0217905550877f8b87668059bc287b70db8f03625d588e3311c769a5d3cba6f6089d494126c44b826004015460405161468b919061f6bc565b60405180910390a260009150506148d2565b6000801b8714156146e3576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016146da9061f441565b60405180910390fd5b6000801b861415614729576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016147209061ee21565b60405180910390fd5b6000801b85141561476f576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016147669061f0e1565b60405180910390fd5b600084849050116147b5576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016147ac9061ed21565b60405180910390fd5b6147c481600101548585618780565b6147d98882600101548360020154878761446c565b861461481a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016148119061f1c1565b60405180910390fd5b6000848460405161482c92919061e0f8565b6040518091039020905060028260030160006101000a81548160ff02191690836004811115614884577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b0217905550887f59d6f0fc082eb7202ec82de15ba92b56013698937bb5266c48d824e56377fd8c898989858a8a6040516148c39695949392919061e699565b60405180910390a26001925050505b9695505050505050565b60007fe81aa909161afcf430ff586fc7da33aa2d9388a2112d5fcb265702d973fc6999463086868660405161491292919061e0f8565b604051809103902060405160200161492e95949392919061e859565b6040516020818303038152906040528051906020012090509392505050565b614955616237565b5061495e6163a3565b614966616c94565b61496f33618bcb565b6149ae576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016149a59061f2c1565b60405180910390fd5b600084849050116149f4576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016149eb9061f0a1565b60405180910390fd5b6000600a8054614a039061fc1a565b905014614a45576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401614a3c9061f041565b60405180910390fd5b6000600254118015614a5e575060025460198054905010155b614a9d576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401614a949061ec21565b60405180910390fd5b6000601060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205490506000600e601460008481526020019081526020016000205481548110614b30577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020190506000614b4d3388886148dc565b9050614ba281836002015487878080601f016020809104026020016040519081016040528093929190818152602001838380828437600081840152601f19601f820116905080830192505050505050506183e2565b614be1576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401614bd89061ecc1565b60405180910390fd5b8686600a9190614bf292919061b8eb565b50614bfb614c33565b600b547ffdb5ca5200719124ffc6cfbdfb877d825b7c50e80612d700f92cddd1358416e460405160405180910390a350505050505050565b6000600a604051614c44919061e150565b6040518091039020905090565b60065481565b6060600060198054905067ffffffffffffffff811115614ca0577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b604051908082528060200260200182016040528015614cce5781602001602082028036833780820191505090505b50905060005b601980549050811015614dc957600e60198281548110614d1d577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b906000526020600020015481548110614d5f577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154828281518110614daa577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010181815250508080614dc19061fc4c565b915050614cd4565b508091505090565b60116020528060005260406000206000915090505481565b600060026004811115614e25577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b6021600084815260200190815260200160002060030160009054906101000a900460ff166004811115614e81577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b149050919050565b6000601a6000838152602001908152602001600020549050919050565b6000614eb0616237565b50614eb96163a3565b614ec1616c94565b600d60009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff1614614f51576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401614f489061eec1565b60405180910390fd5b614f59618d36565b614f98576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401614f8f9061f581565b60405180910390fd5b600082600060048110614fd4577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020020135141580615025575060008260016004811061501d577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002013514155b8061506f5750600082600260048110615067577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002013514155b806150b957506000826003600481106150b1577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002013514155b6150f8576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016150ef9061f4c1565b60405180910390fd5b81601590600461510992919061b971565b506009600081548092919061511d9061fc4c565b91905055503373ffffffffffffffffffffffffffffffffffffffff167f310ee78c937867a3d1525109c5ef1d5d0ad367492ab561fdc0df6f6e2983d78683604051615168919061e393565b60405180910390a260019050919050565b60005481565b6000615189616237565b506151926163a3565b61519a616c94565b600073ffffffffffffffffffffffffffffffffffffffff168773ffffffffffffffffffffffffffffffffffffffff16141561520a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016152019061ec81565b60405180910390fd5b6000801b851415615250576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016152479061f261565b60405180910390fd5b6000801b841415615296576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161528d9061ee41565b60405180910390fd5b6000801b836000013514156152e0576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016152d79061ece1565b60405180910390fd5b6000801b8360200135141561532a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016153219061eda1565b60405180910390fd5b61536d836040016002806020026040519081016040528092919082600260200280828437600081840152601f19601f820116905080830192505050505050618e54565b6153ac576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016153a39061f221565b60405180910390fd5b6000801b8360a0013514156153f6576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016153ed9061f201565b60405180910390fd5b6000801b8360c001351415615440576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016154379061ec41565b60405180910390fd5b6000801b8360e00135141561548a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016154819061f0c1565b60405180910390fd5b73ffffffffffffffffffffffffffffffffffffffff8016836000013560001c11156154ea576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016154e19061f1a1565b60405180910390fd5b73ffffffffffffffffffffffffffffffffffffffff8016836020013560001c111561554a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016155419061edc1565b60405180910390fd5b615553876170aa565b615592576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016155899061f301565b60405180910390fd5b600060235414806155c057506022600086815260200190815260200160002060009054906101000a900460ff165b6155ff576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016155f69061f241565b60405180910390fd5b6000600f60008973ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060405180606001604052908160008201548152602001600182015481526020016002820154815250509050868160200151146156b1576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016156a89061f621565b60405180910390fd5b8360e001358160400151146156fb576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016156f29061f181565b60405180910390fd5b6000615706896170f9565b509050801561574a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016157419061f2e1565b60405180910390fd5b60008989898989600001358a602001358b6040018c608001358d60a001358e60c001358f60e0013560405160200161578c9b9a9998979695949392919061e2aa565b6040516020818303038152906040528051906020012090506157ae81866178e5565b6157ed576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016157e49061ef61565b60405180910390fd5b601360008760000135815260200190815260200160002060009054906101000a900460ff1615615852576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016158499061f341565b60405180910390fd5b600e6040518061012001604052808981526020018a81526020018860000135815260200188602001358152602001886040016002806020026040519081016040528092919082600260200280828437600081840152601f19601f82011690508083019250505050505081526020018c73ffffffffffffffffffffffffffffffffffffffff168152602001856000015181526020016000815260200160026005811115615927577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b81525090806001815401808255809150506001900390600052602060002090600a02016000909190919091506000820151816000015560208201518160010155604082015181600201556060820151816003015560808201518160040190600261599292919061b9b1565b5060a08201518160060160006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555060c0820151816007015560e082015181600801556101008201518160090160006101000a81548160ff02191690836005811115615a40577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b021790555050506001601360008860000135815260200190815260200160002060006101000a81548160ff021916908315150217905550601260008881526020019081526020016000206000815480929190615a9b9061fc4c565b91905055506001600e80549050615ab2919061fae7565b6014600088600001358152602001908152602001600020819055508560000135601060008c73ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002081905550600f60008b73ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600080820160009055600182016000905560028201600090555050600086604001600060028110615bae577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002013587604001600160028110615bf0577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020020135604051602001615c0692919061dfcd565b6040516020818303038152906040528051906020012090508660000135887fd3cc4b05f4560945fdc6ba29306ed6c17d19e415bd6292bc6551864866b46efa8b84604051615c5592919061e4eb565b60405180910390a3877ff111be0ac7f1d16057b0eee19d9ebc647d2ad634c119b3dfb8e56be2de1da56d88600001358960200135848b608001358c60a001358d60c001358e60e0013542604051615cb398979695949392919061e61b565b60405180910390a2615cc3618eee565b60019450505050509695505050505050565b60226020528060005260406000206000915054906101000a900460ff1681565b60075481565b60008060008060008060006020600089815260200190815260200160002090508060000160009054906101000a900460ff168160000160019054906101000a900473ffffffffffffffffffffffffffffffffffffffff1682600101548360020154846003015485600401549650965096509650965096505091939550919395565b6000828230604051602001615d939392919061e0bb565b60405160208183030381529060405280519060200120905092915050565b60008060008060006013600087815260200190815260200160002060009054906101000a900460ff16615e19576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401615e109061f501565b60405180910390fd5b6000600e601460008981526020019081526020016000205481548110615e68577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201604051806101200160405290816000820154815260200160018201548152602001600282015481526020016003820154815260200160048201600280602002604051908101604052809291908260028015615ee6576020028201915b815481526020019060010190808311615ed2575b505050505081526020016006820160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200160078201548152602001600882015481526020016009820160009054906101000a900460ff166005811115615fa4577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b6005811115615fdc577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b815250509050806020015181604001518260a001518360c0015160036005811115616030577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b856101000151600581111561606e577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b14955095509550955095505091939590929450565b61608b61b9f1565b6013600083815260200190815260200160002060009054906101000a900460ff166160eb576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016160e29061f501565b60405180910390fd5b600e601460008481526020019081526020016000205481548110616138577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201600401600280602002604051908101604052809291908260028015616180576020028201915b81548152602001906001019080831161616c575b50505050509050919050565b60007f0b6a1d56187c6120b5bd91b9b49e26adc7a35e92ab475be01f0168090c2408ca4630878787876040516020016161cb979695949392919061e91b565b604051602081830303815290604052805190602001209050949350505050565b60136020528060005260406000206000915054906101000a900460ff1681565b600080600554148061621f57506000600454145b1561622e576003549050616234565b60045490505b90565b6000616241611d66565b61624e57600090506163a0565b61625661376e565b61626357600090506163a0565b60006001600554616274919061fa5a565b9050601e600082815260200190815260200160002060009054906101000a900460ff16156162a65760019150506163a0565b60008060006162b48461922e565b9250925092506000801b8214156162d25760009450505050506163a0565b6001601e600086815260200190815260200160002060006101000a81548160ff02191690831515021790555082601c60008681526020019081526020016000208190555081601d60008681526020019081526020016000208190555080601b600086815260200190815260200160002081905550836005547fe9c66affb346975c2b029e0eb46058348b639e25286df5dc8449372f3bde871b616373619277565b61637b619293565b87878760405161638f95949392919061f883565b60405180910390a360019450505050505b90565b602460009054906101000a900460ff161580156163c557506000601980549050145b156163cf57616c92565b6163d76173c1565b6163e057616c92565b60006163ea61620b565b9050600060016005546163fd919061fa5a565b90506000601b60008381526020019081526020016000205490506000801b81141561642a57505050616c92565b6000616435836173f1565b905060015481101561648657827f545257ed6a2739c2e37a334fe69415ce2d5367209b0bdba09752b18ab2dc5e678260015460405161647592919061f7c9565b60405180910390a250505050616c92565b83600854616494919061fa5a565b6008819055504260078190555060006005549050600060198054905067ffffffffffffffff8111156164ef577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b60405190808252806020026020018201604052801561651d5781602001602082028036833780820191505090505b50905060005b6019805490508110156165cd576019818154811061656a577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002001548282815181106165ae577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101818152505080806165c59061fc4c565b915050616523565b506165d6619472565b60006165e186619541565b90506001548114616627576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161661e9061f321565b60405180910390fd5b60005b818110156167795760006019828154811061666e577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b906000526020600020015490506003600e82815481106166b7577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160006101000a81548160ff02191690836005811115616710577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b021790555081600e8281548110616750577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201600801819055505080806167719061fc4c565b91505061662a565b50600081111561679f576001602460006101000a81548160ff0219169083151502179055505b856005819055506167b1600554617ee9565b601a60006005548152602001908152602001600020546005547ff84454c8aef448f966078ee6ef92f10a2079fcc9bf32f042aef6bb38458344ae42601c6000600554815260200190815260200160002054601d60006005548152602001908152602001600020548a616821618156565b60405161683295949392919061f829565b60405180910390a360005b8251811015616c89576000838281518110616881577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101519050600360058111156168c5577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e82815481106168ff577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115616956577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b1415806169695750616968818461a26f565b5b156169745750616c76565b6004600e82815481106169b0577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160006101000a81548160ff02191690836005811115616a09577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b02179055506000600c5443616a1e919061fa5a565b90506000600e8381548110616a5c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a02016002015490506000600e8481548110616aac577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160060160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690506040518060c001604052806001151581526020018273ffffffffffffffffffffffffffffffffffffffff1681526020018981526020018481526020016000801b815260200160008152506020600084815260200190815260200160002060008201518160000160006101000a81548160ff02191690831515021790555060208201518160000160016101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555060408201518160010155606082015181600201556080820151816003015560a08201518160040155905050878173ffffffffffffffffffffffffffffffffffffffff16837f889a967a8b6706921f0ff6eae756673b3a45720e8ebe168e5d156a2f37214c5b8e42604051616c1992919061f7c9565b60405180910390a4878173ffffffffffffffffffffffffffffffffffffffff16837fd89759ab8b2be4f199931242fe85754f7fa5512fc67c88a0cc5766b2b6fa279d86604051616c69919061f6bc565b60405180910390a4505050505b8080616c819061fc4c565b91505061683d565b50505050505050505b565b6000805b600e8054905081108015616cac5750600882105b156170a6576000600e8281548110616ced577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154905060046005811115616d3b577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e8381548110616d75577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115616dcc577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b148015616e2457506000600e8381548110616e10577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160070154115b8015616e355750616e3481617ba7565b5b15617092576000600e8381548110616e76577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a02016007015490506000600e8481548110616ec6577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201600701819055506005600e8481548110616f17577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160006101000a81548160ff02191690836005811115616f70577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b021790555060006020600084815260200190815260200160002060000160006101000a81548160ff021916908315150217905550600e8381548110616fde577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160060160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16827fb73664d1974bb21f331e31a47199c4e3a57edd81d26b1b05d3ea4a8429a25d437f9fb95a7b89a645130f93fb31e91aef697c7b8cefb38e89b6b5710f29803e079e428560405161707a9392919061eb83565b60405180910390a3838061708d9061fc4c565b945050505b50808061709e9061fc4c565b915050616c98565b5050565b600080600f60008473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206001015414159050919050565b6000806000601060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205490506000801b81141561715b5760008060001b9250925050617199565b6013600082815260200190815260200160002060009054906101000a900460ff166171905760008060001b9250925050617199565b60018192509250505b915091565b6000806171aa836170aa565b156171be5760016000801b915091506173bc565b6000806171ca856170f9565b91509150816171e45760008060001b9350935050506173bc565b6000600e601460008481526020019081526020016000205481548110617233577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a02016040518061012001604052908160008201548152602001600182015481526020016002820154815260200160038201548152602001600482016002806020026040519081016040528092919082600280156172b1576020028201915b81548152602001906001019080831161729d575b505050505081526020016006820160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200160078201548152602001600882015481526020016009820160009054906101000a900460ff16600581111561736f577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b60058111156173a7577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b81525050905080610100015182945094505050505b915091565b6000806173cc61620b565b90506000811180156173eb5750806008546173e7919061fa5a565b4310155b91505090565b600080600090505b600e8054905081101561765a5760026005811115617440577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e828154811061747a577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff1660058111156174d1577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b141580156175a8575060036005811115617514577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e828154811061754e577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff1660058111156175a5577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b14155b156175b257617647565b601f60008481526020019081526020016000206000600e8381548110617601577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154815260200190815260200160002060000160009054906101000a900460ff16156176465781806176429061fc4c565b9250505b5b80806176529061fc4c565b9150506173f9565b50919050565b600080601460008481526020019081526020016000205490503373ffffffffffffffffffffffffffffffffffffffff16600e82815481106176ca577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160060160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1614915050919050565b600080601b6000898152602001908152602001600020549050600061774489898985898961a2f4565b9050808614617788576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161777f9061f5e1565b60405180910390fd5b60007f0bc88877d8ce37d169235b88025357830ed8635cead8bbac92dc0aead63abf5646308c8c8c888d8d8d6040516177c292919061e0f8565b60405180910390206040516020016177e29998979695949392919061ea69565b6040516020818303038152906040528051906020012090506040518060a0016040528060011515815260200188815260200182815260200143815260200142815250601f60008c815260200190815260200160002060008b815260200190815260200160002060008201518160000160006101000a81548160ff02191690831515021790555060208201518160010155604082015181600201556060820151816003015560808201518160040155905050888a7f5a72afe2e027e5572d382952b5dd59e74de4ddf4c7e9a608feaa0c8c8c3601ed8a8a8543426040516178cc95949392919061e806565b60405180910390a3600193505050509695505050505050565b60006178ef618d36565b1561792f576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016179269061ed61565b60405180910390fd5b60008260006002811061796b577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201351415806179bc57506000826001600281106179b4577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002013514155b6179fb576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016179f29061ef61565b60405180910390fd5b6000617a068461a5f7565b90506000604051806040016040528083600060028110617a4f577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518152602001600084600160028110617a95577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002015114617b0e5783600160028110617ad9577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201517f30644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd47617b09919061fae7565b617b11565b60005b8152509050617b9d846002806020026040519081016040528092919082600260200280828437600081840152601f19601f820116905080830192505050505050617b5961a70d565b836015600480602002604051908101604052809291908260048015617b93576020028201915b815481526020019060010190808311617b7f575b505050505061a7bb565b9250505092915050565b6000806020600084815260200190815260200160002090508060000160009054906101000a900460ff168015617be257506000816002015414155b8015617bf15750806002015443115b915050919050565b6000806014600084815260200190815260200160002054905060036005811115617c4c577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e8281548110617c86577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115617cdd577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b14617cec576000915050617ee4565b6000600e8281548110617d28577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160080154905060006001601980549050617d50919061fae7565b9050808214617e3d57600060198281548110617d95577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b906000526020600020015490508060198481548110617ddd577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b906000526020600020018190555082600e8281548110617e26577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160080181905550505b6019805480617e75577f4e487b7100000000000000000000000000000000000000000000000000000000600052603160045260246000fd5b600190038181906000526020600020016000905590556000600e8481548110617ec7577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160080181905550600193505050505b919050565b6000617ef3618156565b90506000617f008261ab50565b905080601a60008581526020019081526020016000208190555080837f9999a867bedf762df746f19073b4ffcfa489fc80ab3cbff37e142f2ead698ea684604051617f4b919061e371565b60405180910390a3505050565b600060016004811115617f94577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b8260030160009054906101000a900460ff166004811115617fde577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b148015617ff057506000826004015414155b8015617fff5750816004015442115b156180445760048081111561803d577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b9050618091565b8160030160009054906101000a900460ff16600481111561808e577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b90505b919050565b60008114156180a457618152565b60008273ffffffffffffffffffffffffffffffffffffffff16826040516180ca9061e167565b60006040518083038185875af1925050503d8060008114618107576040519150601f19603f3d011682016040523d82523d6000602084013e61810c565b606091505b5050905080618150576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016181479061f2a1565b60405180910390fd5b505b5050565b6060600060198054905067ffffffffffffffff81111561819f577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b6040519080825280602002602001820160405280156181cd5781602001602082028036833780820191505090505b50905060005b6019805490508110156182c857600e6019828154811061821c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002001548154811061825e577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201600201548282815181106182a9577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101818152505080806182c09061fc4c565b9150506181d3565b508091505090565b60006020600085815260200190815260200160002090508060000160009054906101000a900460ff1661830357506183dd565b8273ffffffffffffffffffffffffffffffffffffffff168160000160019054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1614618395576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161838c9061efa1565b60405180910390fd5b818160010154146183db576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016183d29061efc1565b60405180910390fd5b505b505050565b60006041825114618428576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161841f9061f561565b60405180910390fd5b60008060006020850151925060408501519150606085015160001a9050601b8160ff16101561846157601b8161845e919061fab0565b90505b601b8160ff1614806184765750601c8160ff16145b6184b5576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016184ac9061eee1565b60405180910390fd5b7f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a08260001c111561851b576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016185129061f3e1565b60405180910390fd5b600060018883868660405160008152602001604052604051618540949392919061ebba565b6020604051602081039080840390855afa158015618562573d6000803e3d6000fd5b50505060206040510351905060008760001c9050600073ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff16141580156185de57508073ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff16145b955050505050509392505050565b60006185f785617bf9565b90506000600e8581548110618635577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201905060008160070154905060008260060160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1690506000602060008a81526020019081526020016000206002015490506000846007018190555060048460090160006101000a81548160ff021916908360058111156186ea577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b02179055506186fc898389848a61ada3565b841561870e5761870d600554617ee9565b5b6187178861ae95565b6187218284618096565b868273ffffffffffffffffffffffffffffffffffffffff168a7f25ecc55592b6da6238c7ec72c0e7d31ed5e95088032d55d8fb237805c55c8b4189428860405161876d9392919061eb83565b60405180910390a4505050505050505050565b60008282810190618791919061bebe565b9050600254815110156187d9576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016187d09061f661565b60405180910390fd5b60005b8151811015618bc4576000801b828281518110618822577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015160000151141561886f576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016188669061f001565b60405180910390fd5b60008282815181106188aa577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101516020015151116188f7576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016188ee9061f681565b60405180910390fd5b618944828281518110618933577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101516000015161b63e565b618983576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161897a9061ed41565b60405180910390fd5b60005b81811015618a6c578282815181106189c7577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015160000151838281518110618a0c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151600001511415618a59576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401618a509061ee01565b60405180910390fd5b8080618a649061fc4c565b915050618986565b50600085838381518110618aa9577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015160200151604051602001618ac692919061e093565b604051602081830303815290604052805190602001209050618b7181848481518110618b1b577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015160000151858581518110618b60577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151604001516183e2565b618bb0576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401618ba79061f081565b60405180910390fd5b508080618bbc9061fc4c565b9150506187dc565b5050505050565b600080601060008473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205490506000801b811480618c3f57506013600082815260200190815260200160002060009054906101000a900460ff16155b15618c4e576000915050618d31565b60036005811115618c88577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e601460008481526020019081526020016000205481548110618cd5577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115618d2c577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b149150505b919050565b6000806015600060048110618d74577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b0154148015618dbf575060006015600160048110618dbb577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b0154145b8015618e07575060006015600260048110618e03577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b0154145b8015618e4f575060006015600360048110618e4b577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b0154145b905090565b60008060001b82600060028110618e94577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020020151141580618ee757506000801b82600160028110618edf577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002015114155b9050919050565b602460009054906101000a900460ff1680618f0f5750600060198054905014155b80618f1c57506000600154145b80618f2957506000600254145b80618f375750600254600154105b15618f415761922c565b600154618f4c619293565b1015618f575761922c565b6000805b600e8054905081108015618f70575060015482105b156191845760026005811115618faf577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e8281548110618fe9577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115619040577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b1461904a57619171565b6003600e8281548110619086577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160006101000a81548160ff021916908360058111156190df577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b0217905550601980549050600e8281548110619124577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201600801819055506019819080600181540180825580915050600190039060005260206000200160009091909190915055818061916d9061fc4c565b9250505b808061917c9061fc4c565b915050618f5b565b5060015481141561922a576001602460006101000a81548160ff02191690831515021790555042600781905550436008819055506191c3600554617ee9565b601a60006005548152602001908152602001600020546005547ff84454c8aef448f966078ee6ef92f10a2079fcc9bf32f042aef6bb38458344ae4260008060001b6000801b619210618156565b60405161922195949392919061f76f565b60405180910390a35b505b565b6000806000600643116192505760008060001b6000801b925092509250619270565b60064361925d919061fae7565b92508240915061926d8285615d7c565b90505b9193909250565b600061928161620b565b60085461928e919061fa5a565b905090565b600080600090505b600e8054905081101561946e57600260058111156192e2577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e828154811061931c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115619373577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b14806194475750600360058111156193b4577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e82815481106193ee577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115619445577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b145b1561945b5781806194579061fc4c565b9250505b80806194669061fc4c565b91505061929b565b5090565b60005b601980549050811015619530576000601982815481106194be577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b906000526020600020015490506000600e8281548110619507577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a0201600801819055505080806195289061fc4c565b915050619475565b506019600061953f919061ba13565b565b6000806001541415619556576000905061a26a565b600060015467ffffffffffffffff81111561959a577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b6040519080825280602002602001820160405280156195c85781602001602082028036833780820191505090505b509050600060015467ffffffffffffffff81111561960f577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b60405190808252806020026020018201604052801561963d5781602001602082028036833780820191505090505b509050600060015467ffffffffffffffff811115619684577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b6040519080825280602002602001820160405280156196b25781602001602082028036833780820191505090505b50905060005b600e80549050811015619d9157600260058111156196ff577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e8281548110619739577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115619790577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b141580156198675750600360058111156197d3577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600e828154811061980d577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160090160009054906101000a900460ff166005811115619864577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b14155b1561987157619d7e565b6000601f60008881526020019081526020016000206000600e84815481106198c2577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154815260200190815260200160002090508060000160009054906101000a900460ff166199005750619d7e565b6000816001015460001c9050600154871015619a465782868881518110619950577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101818152505080858881518110619996577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001018181525050600e83815481106199dc577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154848881518110619a27577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010181815250508680619a3e9061fc4c565b975050619d7b565b600080600190505b88811015619b7e57619b62878281518110619a92577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151878381518110619ad3577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151898581518110619b14577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151898681518110619b55577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015161b712565b15619b6b578091505b8080619b769061fc4c565b915050619a4e565b50619c5682600e8681548110619bbd577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154888481518110619c08577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151888581518110619c49577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015161b736565b15619d795783878281518110619c95577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101818152505081868281518110619cdb577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001018181525050600e8481548110619d21577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154858281518110619d6c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010181815250505b505b50505b8080619d899061fc4c565b9150506196b8565b506000600190505b8481101561a1dd576000848281518110619ddc577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015190506000848381518110619e21577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015190506000848481518110619e66577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151905060008490505b600081118015619f265750619f25838389600185619e94919061fae7565b81518110619ecb577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015189600186619ee1919061fae7565b81518110619f18577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015161b736565b5b1561a0f45787600182619f39919061fae7565b81518110619f70577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151888281518110619fb1577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101818152505086600182619fcb919061fae7565b8151811061a002577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015187828151811061a043577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010181815250508560018261a05d919061fae7565b8151811061a094577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015186828151811061a0d5577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001018181525050808061a0ec9061fbf0565b915050619e76565b8388828151811061a12e577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010181815250508287828151811061a174577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010181815250508186828151811061a1ba577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101818152505050505050808061a1d59061fc4c565b915050619d99565b5060005b8481101561a26557601984828151811061a224577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200260200101519080600181540180825580915050600190039060005260206000200160009091909190915055808061a25d9061fc4c565b91505061a1e1565b505050505b919050565b600080600090505b8281101561a2e857836019828154811061a2ba577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b9060005260206000200154141561a2d557600191505061a2ee565b808061a2e09061fc4c565b91505061a277565b50600090505b92915050565b60006041838390501461a33c576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161a3339061f521565b60405180910390fd5b73ffffffffffffffffffffffffffffffffffffffff80168560001c111561a398576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161a38f9061edc1565b60405180910390fd5b60008060008535925060208601359150604086013560001a9050601b8160ff16101561a3ce57601b8161a3cb919061fab0565b90505b601b8160ff16148061a3e35750601c8160ff16145b61a422576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161a4199061ef01565b60405180910390fd5b7f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a08260001c111561a488576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161a47f9061f541565b60405180910390fd5b600061a4968b8b8b8b611db5565b905060006001828487876040516000815260200160405260405161a4bd949392919061ebba565b6020604051602081039080840390855afa15801561a4df573d6000803e3d6000fd5b505050602060405103519050600073ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff161415801561a55657508960001c73ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff16145b61a595576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161a58c9061f061565b60405180910390fd5b7f0bc88877d8ce37d169235b88025357830ed8635cead8bbac92dc0aead63abf56898b87878760405160200161a5d09695949392919061dff9565b60405160208183030381529060405280519060200120955050505050509695505050505050565b61a5ff61ba34565b60007f30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f00000016002846040518060400160405280601481526020017f636f6d6d69747465652d7369672f6d76702f763100000000000000000000000081525060405160200161a66c92919061e093565b60405160208183030381529060405260405161a688919061e111565b602060405180830381855afa15801561a6a5573d6000803e3d6000fd5b5050506040513d601f19601f8201168201806040525081019061a6c8919061bf51565b60001c61a6d5919061fcdf565b9050600081141561a6e557600190505b61a70560405180604001604052806001815260200160028152508261b75a565b915050919050565b61a71561ba56565b60405180608001604052807f198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c281526020017f1800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed81526020017f090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b81526020017f12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa815250905090565b6000808560006002811061a7f8577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518660016002811061a837577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518660006004811061a876577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518760016004811061a8b5577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518860026004811061a8f4577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518960036004811061a933577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518960006002811061a972577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518a60016002811061a9b1577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518a60006004811061a9f0577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518b60016004811061aa2f577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518c60026004811061aa6e577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518d60036004811061aaad577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002015160405160200161aacd9c9b9a9998979695949392919061e1b9565b604051602081830303815290604052905061aae661ba78565b60006020826101806020860160085afa905080801561ab43575060018260006001811061ab3c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020020151145b9350505050949350505050565b600080600190505b825181101561ad0c57600083828151811061ab9c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151905060008290505b60008111801561ac075750818560018361abc6919061fae7565b8151811061abfd577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010151115b1561acb1578460018261ac1a919061fae7565b8151811061ac51577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015185828151811061ac92577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001018181525050808061aca99061fbf0565b91505061abac565b8185828151811061aceb577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6020026020010181815250505050808061ad049061fc4c565b91505061ab58565b50606060005b835181101561ad92578184828151811061ad55577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b602002602001015160405160200161ad6e92919061e128565b6040516020818303038152906040529150808061ad8a9061fc4c565b91505061ad12565b508080519060200120915050919050565b6040518060c001604052806000151581526020018573ffffffffffffffffffffffffffffffffffffffff168152602001848152602001838152602001828152602001428152506020600087815260200190815260200160002060008201518160000160006101000a81548160ff02191690831515021790555060208201518160000160016101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555060408201518160010155606082015181600201556080820151816003015560a082015181600401559050505050505050565b6000600e828154811061aed1577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160405180610120016040529081600082015481526020016001820154815260200160028201548152602001600382015481526020016004820160028060200260405190810160405280929190826002801561af4f576020028201915b81548152602001906001019080831161af3b575b505050505081526020016006820160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200160078201548152602001600882015481526020016009820160009054906101000a900460ff16600581111561b00d577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600581111561b045577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b815250509050601360008260400151815260200190815260200160002060006101000a81549060ff02191690556000601260008360000151815260200190815260200160002054111561b0c0576012600082600001518152602001908152602001600020600081548092919061b0ba9061fbf0565b91905055505b6014600082604001518152602001908152602001600020600090558060400151601060008360a0015173ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054141561b17257601060008260a0015173ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600090555b60006001600e8054905061b186919061fae7565b905080831461b56d576000600e828154811061b1cb577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160405180610120016040529081600082015481526020016001820154815260200160028201548152602001600382015481526020016004820160028060200260405190810160405280929190826002801561b249576020028201915b81548152602001906001019080831161b235575b505050505081526020016006820160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200160078201548152602001600882015481526020016009820160009054906101000a900460ff16600581111561b307577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600581111561b33f577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b81525050905080600e858154811061b380577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a02016000820151816000015560208201518160010155604082015181600201556060820151816003015560808201518160040190600261b3cd92919061b9b1565b5060a08201518160060160006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555060c0820151816007015560e082015181600801556101008201518160090160006101000a81548160ff0219169083600581111561b47b577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b0217905550905050836014600083604001518152602001908152602001600020819055506003600581111561b4d9577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b816101000151600581111561b517577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b141561b56b578360198260e001518154811061b55c577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002001819055505b505b600e80548061b5a5577f4e487b7100000000000000000000000000000000000000000000000000000000600052603160045260246000fd5b60019003818190600052602060002090600a020160008082016000905560018201600090556002820160009055600382016000905560048201600061b5ea919061ba9a565b6006820160006101000a81549073ffffffffffffffffffffffffffffffffffffffff0219169055600782016000905560088201600090556009820160006101000a81549060ff021916905550509055505050565b600080600090505b60198054905081101561b7075782600e6019838154811061b690577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002001548154811061b6d2577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b90600052602060002090600a020160020154141561b6f457600191505061b70d565b808061b6ff9061fc4c565b91505061b646565b50600090505b919050565b60008285108061b72c5750828514801561b72b57508184105b5b9050949350505050565b60008285118061b7505750828514801561b74f57508184115b5b9050949350505050565b61b76261ba34565b60008360006002811061b79e577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518460016002811061b7dd577f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b60200201518460405160200161b7f59392919061e17c565b6040516020818303038152906040529050600080604067ffffffffffffffff81111561b84a577f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b6040519080825280601f01601f19166020018201604052801561b87c5781602001600182028036833780820191505090505b50905060406020820160606020860160075afa91508161b8d1576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161b8c89061f641565b60405180910390fd5b602081015184526040810151602085015250505092915050565b82805461b8f79061fc1a565b90600052602060002090601f01602090048101928261b919576000855561b960565b82601f1061b93257803560ff191683800117855561b960565b8280016001018555821561b960579182015b8281111561b95f57823582559160200191906001019061b944565b5b50905061b96d919061baa8565b5090565b826004810192821561b9a0579160200282015b8281111561b99f57823582559160200191906001019061b984565b5b50905061b9ad919061baa8565b5090565b826002810192821561b9e0579160200282015b8281111561b9df57825182559160200191906001019061b9c4565b5b50905061b9ed919061bac5565b5090565b6040518060400160405280600290602082028036833780820191505090505090565b508054600082559060005260206000209081019061ba31919061baa8565b50565b6040518060400160405280600290602082028036833780820191505090505090565b6040518060800160405280600490602082028036833780820191505090505090565b6040518060200160405280600190602082028036833780820191505090505090565b506000815560010160009055565b5b8082111561bac157600081600090555060010161baa9565b5090565b5b8082111561bade57600081600090555060010161bac6565b5090565b600061baf561baf08461f94b565b61f91a565b9050808382526020820190508260005b8581101561bb35578135850161bb1b888261bcc7565b84526020840193506020830192505060018101905061bb05565b5050509392505050565b600061bb5261bb4d8461f977565b61f91a565b90508281526020810184848401111561bb6a57600080fd5b61bb7584828561fbae565b509392505050565b60008135905061bb8c8161fdf7565b92915050565b600082601f83011261bba357600080fd5b813561bbb384826020860161bae2565b91505092915050565b60008190508260206002028201111561bbd457600080fd5b92915050565b60008190508260206004028201111561bbf257600080fd5b92915050565b60008135905061bc078161fe0e565b92915050565b60008135905061bc1c8161fe25565b92915050565b60008151905061bc318161fe25565b92915050565b60008083601f84011261bc4957600080fd5b8235905067ffffffffffffffff81111561bc6257600080fd5b60208301915083600182028301111561bc7a57600080fd5b9250929050565b600082601f83011261bc9257600080fd5b813561bca284826020860161bb3f565b91505092915050565b6000610140828403121561bcbe57600080fd5b81905092915050565b60006060828403121561bcd957600080fd5b61bce3606061f91a565b9050600061bcf38482850161bc0d565b600083015250602082013567ffffffffffffffff81111561bd1357600080fd5b61bd1f8482850161bc81565b602083015250604082013567ffffffffffffffff81111561bd3f57600080fd5b61bd4b8482850161bc81565b60408301525092915050565b6000610100828403121561bd6a57600080fd5b81905092915050565b60008135905061bd828161fe3c565b92915050565b60008135905061bd978161fe53565b92915050565b60008135905061bdac8161fe6a565b92915050565b60006020828403121561bdc457600080fd5b600061bdd28482850161bb7d565b91505092915050565b60008060006040848603121561bdf057600080fd5b600061bdfe8682870161bb7d565b935050602084013567ffffffffffffffff81111561be1b57600080fd5b61be278682870161bc37565b92509250509250925092565b6000806000806000806101c0878903121561be4d57600080fd5b600061be5b89828a0161bb7d565b965050602061be6c89828a0161bd88565b955050604061be7d89828a0161bc0d565b945050606061be8e89828a0161bc0d565b935050608061be9f89828a0161bd57565b92505061018061beb189828a0161bbbc565b9150509295509295509295565b60006020828403121561bed057600080fd5b600082013567ffffffffffffffff81111561beea57600080fd5b61bef68482850161bb92565b91505092915050565b60006080828403121561bf1157600080fd5b600061bf1f8482850161bbda565b91505092915050565b60006020828403121561bf3a57600080fd5b600061bf488482850161bc0d565b91505092915050565b60006020828403121561bf6357600080fd5b600061bf718482850161bc22565b91505092915050565b6000806000806080858703121561bf9057600080fd5b600061bf9e8782880161bc0d565b945050602061bfaf8782880161bb7d565b935050604061bfc08782880161bd88565b925050606061bfd18782880161bc0d565b91505092959194509250565b6000806040838503121561bff057600080fd5b600061bffe8582860161bc0d565b925050602061c00f8582860161bbf8565b9150509250929050565b60008060008060008060a0878903121561c03257600080fd5b600061c04089828a0161bc0d565b965050602061c05189828a0161bc0d565b955050604061c06289828a0161bc0d565b945050606061c07389828a0161bc0d565b935050608087013567ffffffffffffffff81111561c09057600080fd5b61c09c89828a0161bc37565b92509250509295509295509295565b60008060008060006080868803121561c0c357600080fd5b600061c0d18882890161bc0d565b955050602061c0e28882890161bc0d565b945050604061c0f38882890161bc0d565b935050606086013567ffffffffffffffff81111561c11057600080fd5b61c11c8882890161bc37565b92509250509295509295909350565b6000806000806080858703121561c14157600080fd5b600061c14f8782880161bc0d565b945050602061c1608782880161bc0d565b935050604061c1718782880161bc0d565b925050606061c1828782880161bd88565b91505092959194509250565b60008060008060a0858703121561c1a457600080fd5b600061c1b28782880161bc0d565b945050602061c1c38782880161bc0d565b935050604061c1d48782880161bd88565b925050606061c1e58782880161bbbc565b91505092959194509250565b6000806000806080858703121561c20757600080fd5b600061c2158782880161bc0d565b945050602061c2268782880161bc0d565b935050604061c2378782880161bd88565b925050606061c2488782880161bd88565b91505092959194509250565b60008060006040848603121561c26957600080fd5b600061c2778682870161bc0d565b935050602084013567ffffffffffffffff81111561c29457600080fd5b61c2a08682870161bc37565b92509250509250925092565b6000806040838503121561c2bf57600080fd5b600061c2cd8582860161bc0d565b925050602061c2de8582860161bd88565b9150509250929050565b60008060008060006080868803121561c30057600080fd5b600061c30e8882890161bc0d565b955050602061c31f8882890161bd88565b945050604061c3308882890161bc0d565b935050606086013567ffffffffffffffff81111561c34d57600080fd5b61c3598882890161bc37565b92509250509295509295909350565b6000806000806040858703121561c37e57600080fd5b600085013567ffffffffffffffff81111561c39857600080fd5b61c3a48782880161bc37565b9450945050602085013567ffffffffffffffff81111561c3c357600080fd5b61c3cf8782880161bc37565b925092505092959194509250565b6000610140828403121561c3f057600080fd5b600061c3fe8482850161bcab565b91505092915050565b6000806000806101c0858703121561c41e57600080fd5b600061c42c8782880161bcab565b94505061014061c43e8782880161bc0d565b93505061016061c4508782880161bc0d565b92505061018061c4628782880161bbbc565b91505092959194509250565b6000806000806101c0858703121561c48557600080fd5b600061c4938782880161bcab565b94505061014061c4a58782880161bc0d565b93505061016061c4b78782880161bd88565b92505061018061c4c98782880161bbbc565b91505092959194509250565b60006020828403121561c4e757600080fd5b600061c4f58482850161bd73565b91505092915050565b60006020828403121561c51057600080fd5b600061c51e8482850161bd88565b91505092915050565b6000806040838503121561c53a57600080fd5b600061c5488582860161bd88565b925050602061c5598582860161bc0d565b9150509250929050565b6000806000806080858703121561c57957600080fd5b600061c5878782880161bd88565b945050602061c5988782880161bc0d565b935050604061c5a98782880161bc0d565b925050606061c5ba8782880161bc0d565b91505092959194509250565b60008060008060006080868803121561c5de57600080fd5b600061c5ec8882890161bd88565b955050602061c5fd8882890161bc0d565b945050604061c60e8882890161bc0d565b935050606086013567ffffffffffffffff81111561c62b57600080fd5b61c6378882890161bc37565b92509250509295509295909350565b60006020828403121561c65857600080fd5b600061c6668482850161bd9d565b91505092915050565b600061c67b838361c7b1565b60208301905092915050565b61c6908161fb1b565b82525050565b61c6a761c6a28261fb1b565b61fc95565b82525050565b61c6b96040838361fbae565b5050565b61c6c68161f9d6565b61c6d0818461fa11565b925061c6db8261f9a7565b8060005b8381101561c70c57815161c6f3878261c66f565b965061c6fe8361f9f7565b92505060018101905061c6df565b505050505050565b61c72060e0838361fbae565b5050565b600061c72f8261f9e1565b61c739818561fa1c565b935061c7448361f9b1565b8060005b8381101561c77557815161c75c888261c66f565b975061c7678361fa04565b92505060018101905061c748565b5085935050505092915050565b61c78e6040838361fbae565b5050565b61c79e6080838361fbae565b5050565b61c7ab8161fb2d565b82525050565b61c7ba8161fb39565b82525050565b61c7c98161fb39565b82525050565b61c7e061c7db8261fb39565b61fca7565b82525050565b600061c7f2838561fa2d565b935061c7ff83858461fbae565b61c8088361fdcc565b840190509392505050565b600061c81f838561fa3e565b935061c82c83858461fbae565b82840190509392505050565b600061c8438261f9ec565b61c84d818561fa2d565b935061c85d81856020860161fbbd565b61c8668161fdcc565b840191505092915050565b600061c87c8261f9ec565b61c886818561fa3e565b935061c89681856020860161fbbd565b80840191505092915050565b6000815461c8af8161fc1a565b61c8b9818661fa3e565b9450600182166000811461c8d4576001811461c8e55761c918565b60ff1983168652818601935061c918565b61c8ee8561f9c1565b60005b8381101561c9105781548189015260018201915060208101905061c8f1565b838801955050505b50505092915050565b61c92a8161fb9c565b82525050565b600061c93d601c8361fa49565b91507f41637469766520544545207468726573686f6c64206e6f74206d6574000000006000830152602082019050919050565b600061c97d601b8361fa49565b91507f52656769737465722063657274206861736820726571756972656400000000006000830152602082019050919050565b600061c9bd601d8361fa49565b91507f496e76616c69642065726173652070726f6f66207369676e61747572650000006000830152602082019050919050565b600061c9fd600d8361fa49565b91507f496e76616c6964206f776e6572000000000000000000000000000000000000006000830152602082019050919050565b600061ca3d60158361fa49565b91507f52656461637420726571756573742065786973747300000000000000000000006000830152602082019050919050565b600061ca7d60168361fa49565b91507f496e76616c696420706b4348207369676e6174757265000000000000000000006000830152602082019050919050565b600061cabd601d8361fa49565b91507f5369676e6174757265207075626c6963206b65792072657175697265640000006000830152602082019050919050565b600061cafd60158361fa49565b91507f417574686f72697a6174696f6e206578706972656400000000000000000000006000830152602082019050919050565b600061cb3d60118361fa49565b91507f70695f7265715f412072657175697265640000000000000000000000000000006000830152602082019050919050565b600061cb7d60198361fa49565b91507f54454520736967207075626b6579206e6f7420616374697665000000000000006000830152602082019050919050565b600061cbbd60188361fa49565b91507f436f6d6d6974746565207075626b6579206e6f742073657400000000000000006000830152602082019050919050565b600061cbfd60158361fa49565b91507f536c6173682065786365656473206465706f73697400000000000000000000006000830152602082019050919050565b600061cc3d60178361fa49565b91507f565246207075626c6963206b65792072657175697265640000000000000000006000830152602082019050919050565b600061cc7d601e8361fa49565b91507f565246207075626c6963206b6579206d757374206265206164647265737300006000830152602082019050919050565b600061ccbd60188361fa49565b91507f416374697665207365742068617368206d69736d6174636800000000000000006000830152602082019050919050565b600061ccfd60188361fa49565b91507f4475706c69636174652054454520736967207075626b657900000000000000006000830152602082019050919050565b600061cd3d60138361fa49565b91507f50726f6f662068617368207265717569726564000000000000000000000000006000830152602082019050919050565b600061cd7d60148361fa49565b91507f48617264776172652049442072657175697265640000000000000000000000006000830152602082019050919050565b600061cdbd60178361fa49565b91507f4f6e6c7920544545206f776e65722063616e20657869740000000000000000006000830152602082019050919050565b600061cdfd600e8361fa49565b91507f45706f6368206d69736d617463680000000000000000000000000000000000006000830152602082019050919050565b600061ce3d600f8361fa49565b91507f45706f636820696e2066757475726500000000000000000000000000000000006000830152602082019050919050565b600061ce7d60178361fa49565b91507f4f6e6c79206f776e65722063616e2072656769737465720000000000000000006000830152602082019050919050565b600061cebd60178361fa49565b91507f496e76616c696420544545207265636f766572792069640000000000000000006000830152602082019050919050565b600061cefd60178361fa49565b91507f496e76616c696420565246207265636f766572792069640000000000000000006000830152602082019050919050565b600061cf3d60148361fa49565b91507f526561736f6e20686173682072657175697265640000000000000000000000006000830152602082019050919050565b600061cf7d60108361fa49565b91507f4465706f736974207265717569726564000000000000000000000000000000006000830152602082019050919050565b600061cfbd60118361fa49565b91507f496e76616c6964207369676e61747572650000000000000000000000000000006000830152602082019050919050565b600061cffd60178361fa49565b91507f526f746174696f6e2073656564206e6f742072656164790000000000000000006000830152602082019050919050565b600061d03d60138361fa49565b91507f45786974206f776e6572206d69736d61746368000000000000000000000000006000830152602082019050919050565b600061d07d60138361fa49565b91507f457869742065706f6368206d69736d61746368000000000000000000000000006000830152602082019050919050565b600061d0bd601d8361fa49565b91507f5445452063616e6e6f74207375626d69742065726173652070726f6f660000006000830152602082019050919050565b600061d0fd60178361fa49565b91507f54454520736967207075626b65792072657175697265640000000000000000006000830152602082019050919050565b600061d13d601a8361fa49565b91507f4e6f7420616363657074696e6720726f746174696f6e205652460000000000006000830152602082019050919050565b600061d17d60188361fa49565b91507f706b434820616c726561647920696e697469616c697a656400000000000000006000830152602082019050919050565b600061d1bd60118361fa49565b91507f496e76616c6964205652462070726f6f660000000000000000000000000000006000830152602082019050919050565b600061d1fd601b8361fa49565b91507f496e76616c696420544545207368617265207369676e617475726500000000006000830152602082019050919050565b600061d23d600d8361fa49565b91507f706b4348207265717569726564000000000000000000000000000000000000006000830152602082019050919050565b600061d27d60178361fa49565b91507f4368616c6c656e676520686173682072657175697265640000000000000000006000830152602082019050919050565b600061d2bd60148361fa49565b91507f526573756c7420686173682072657175697265640000000000000000000000006000830152602082019050919050565b600061d2fd600e8361fa49565b91507f72657149442072657175697265640000000000000000000000000000000000006000830152602082019050919050565b600061d33d60168361fa49565b91507f565246207075626c6963206b6579206d697373696e67000000000000000000006000830152602082019050919050565b600061d37d601f8361fa49565b91507f45726173652070726f6f6620646561646c696e65206e6f7420706173736564006000830152602082019050919050565b600061d3bd601a8361fa49565b91507f50656e64696e672054454520616c7265616479206578697374730000000000006000830152602082019050919050565b600061d3fd60128361fa49565b91507f4368616c6c656e6765206d69736d6174636800000000000000000000000000006000830152602082019050919050565b600061d43d60248361fa49565b91507f5369676e6174757265207075626c6963206b6579206d7573742062652061646460008301527f72657373000000000000000000000000000000000000000000000000000000006020830152604082019050919050565b600061d4a360138361fa49565b91507f50726f6f662068617368206d69736d61746368000000000000000000000000006000830152602082019050919050565b600061d4e360158361fa49565b91507f54454520616c72656164792066696e616c697a656400000000000000000000006000830152602082019050919050565b600061d52360138361fa49565b91507f51756f74652068617368207265717569726564000000000000000000000000006000830152602082019050919050565b600061d563601b8361fa49565b91507f456e636c617665207075626c6963206b657920726571756972656400000000006000830152602082019050919050565b600061d5a360168361fa49565b91507f4d525f454e434c415645206e6f742074727573746564000000000000000000006000830152602082019050919050565b600061d5e360138361fa49565b91507f4d525f454e434c415645207265717569726564000000000000000000000000006000830152602082019050919050565b600061d623601d8361fa49565b91507f5265646163742072657175657374206e6f7420617574686f72697a65640000006000830152602082019050919050565b600061d663600d8361fa49565b91507f526566756e64206661696c6564000000000000000000000000000000000000006000830152602082019050919050565b600061d6a360208361fa49565b91507f43616c6c6572206973206e6f742063757272656e7420616374697665205445456000830152602082019050919050565b600061d6e360208361fa49565b91507f54454520616c7265616479207265676973746572656420666f72206f776e65726000830152602082019050919050565b600061d72360158361fa49565b91507f50656e64696e6720544545206e6f7420666f756e6400000000000000000000006000830152602082019050919050565b600061d76360168361fa49565b91507f4163746976652073657420756e64657266696c6c6564000000000000000000006000830152602082019050919050565b600061d7a360278361fa49565b91507f5369676e6174757265207075626c6963206b657920616c72656164792072656760008301527f69737465726564000000000000000000000000000000000000000000000000006020830152604082019050919050565b600061d80960148361fa49565b91507f45726173652070726f6f662072657175697265640000000000000000000000006000830152602082019050919050565b600061d84960108361fa49565b91507f544545206e6f7420656c696769626c65000000000000000000000000000000006000830152602082019050919050565b600061d88960128361fa49565b91507f5652462070726f6f6620726571756972656400000000000000000000000000006000830152602082019050919050565b600061d8c960128361fa49565b91507f4e6f7420636f6e7472616374206f776e657200000000000000000000000000006000830152602082019050919050565b600061d90960158361fa49565b91507f496e76616c696420544545207369676e617475726500000000000000000000006000830152602082019050919050565b600061d949601f8361fa49565b91507f496e76616c696420617574686f72697a6174696f6e207369676e6174757265006000830152602082019050919050565b600061d98960158361fa49565b91507f5445452063616e6e6f74206265207265766f6b656400000000000000000000006000830152602082019050919050565b600061d9c960148361fa49565b91507f725072696d6520686173682072657175697265640000000000000000000000006000830152602082019050919050565b600061da0960008361fa3e565b9150600082019050919050565b600061da23601b8361fa49565b91507f496e76616c69642072656a656374696f6e207369676e617475726500000000006000830152602082019050919050565b600061da63601d8361fa49565b91507f4f6e6c7920544545206f776e65722063616e207375626d6974205652460000006000830152602082019050919050565b600061daa360158361fa49565b91507f56524620616c7265616479207375626d697474656400000000000000000000006000830152602082019050919050565b600061dae360188361fa49565b91507f496e76616c696420636f6d6d6974746565207075626b657900000000000000006000830152602082019050919050565b600061db23601b8361fa49565b91507f556e737570706f72746564207265646163742070726f746f636f6c00000000006000830152602082019050919050565b600061db6360238361fa49565b91507f5369676e6174757265207075626c6963206b6579206e6f74207265676973746560008301527f72656400000000000000000000000000000000000000000000000000000000006020830152604082019050919050565b600061dbc960188361fa49565b91507f496e76616c6964205652462070726f6f66206c656e67746800000000000000006000830152602082019050919050565b600061dc0960158361fa49565b91507f496e76616c696420565246207369676e617475726500000000000000000000006000830152602082019050919050565b600061dc49601c8361fa49565b91507f496e76616c696420544545207369676e6174757265206c656e677468000000006000830152602082019050919050565b600061dc8960238361fa49565b91507f436f6d6d6974746565207075626b657920616c7265616479207265676973746560008301527f72656400000000000000000000000000000000000000000000000000000000006020830152604082019050919050565b600061dcef60128361fa49565b91507f706b43482068617368206d69736d6174636800000000000000000000000000006000830152602082019050919050565b600061dd2f60108361fa49565b91507f53797374656d206e6f74207265616479000000000000000000000000000000006000830152602082019050919050565b600061dd6f60138361fa49565b91507f56524620726573756c74206d69736d61746368000000000000000000000000006000830152602082019050919050565b600061ddaf60138361fa49565b91507f56524620726573756c74207265717569726564000000000000000000000000006000830152602082019050919050565b600061ddef60168361fa49565b91507f50656e64696e67206e6f6e6365206d69736d61746368000000000000000000006000830152602082019050919050565b600061de2f60178361fa49565b91507f626e323536207363616c6172206d756c206661696c65640000000000000000006000830152602082019050919050565b600061de6f601b8361fa49565b91507f5445452070726f6f66207468726573686f6c64206e6f74206d657400000000006000830152602082019050919050565b600061deaf60128361fa49565b91507f54454520736861726520726571756972656400000000000000000000000000006000830152602082019050919050565b61deeb8161fb43565b82525050565b61defa8161fb71565b82525050565b61df1161df0c8261fb71565b61fcc3565b82525050565b61df208161fb7b565b82525050565b61df2f8161fb8f565b82525050565b61df4661df418261fb8f565b61fccd565b82525050565b600061df58828a61c696565b60148201915061df68828961c696565b60148201915061df78828861df00565b60208201915061df88828761df00565b60208201915061df98828661df00565b60208201915061dfa8828561df00565b60208201915061dfb8828461c7cf565b60208201915081905098975050505050505050565b600061dfd9828561c7cf565b60208201915061dfe9828461c7cf565b6020820191508190509392505050565b600061e005828961c7cf565b60208201915061e015828861c7cf565b60208201915061e025828761c7cf565b60208201915061e035828661c7cf565b60208201915061e045828561c7cf565b60208201915061e055828461df35565b600182019150819050979650505050505050565b600061e075828661c7cf565b60208201915061e08682848661c813565b9150819050949350505050565b600061e09f828561c7cf565b60208201915061e0af828461c871565b91508190509392505050565b600061e0c7828661c7cf565b60208201915061e0d7828561df00565b60208201915061e0e7828461c696565b601482019150819050949350505050565b600061e10582848661c813565b91508190509392505050565b600061e11d828461c871565b915081905092915050565b600061e134828561c871565b915061e140828461c7cf565b6020820191508190509392505050565b600061e15c828461c8a2565b915081905092915050565b600061e1728261d9fc565b9150819050919050565b600061e188828661df00565b60208201915061e198828561df00565b60208201915061e1a8828461df00565b602082019150819050949350505050565b600061e1c5828f61df00565b60208201915061e1d5828e61df00565b60208201915061e1e5828d61df00565b60208201915061e1f5828c61df00565b60208201915061e205828b61df00565b60208201915061e215828a61df00565b60208201915061e225828961df00565b60208201915061e235828861df00565b60208201915061e245828761df00565b60208201915061e255828661df00565b60208201915061e265828561df00565b60208201915061e275828461df00565b6020820191508190509d9c50505050505050505050505050565b600060208201905061e2a4600083018461c687565b92915050565b60006101808201905061e2c0600083018e61c687565b61e2cd602083018d61def1565b61e2da604083018c61c7c0565b61e2e7606083018b61c7c0565b61e2f4608083018a61c7c0565b61e30160a083018961c7c0565b61e30e60c083018861c6ad565b61e31c61010083018761c7c0565b61e32a61012083018661c7c0565b61e33861014083018561c7c0565b61e34661016083018461c7c0565b9c9b505050505050505050505050565b600060408201905061e36b600083018461c6bd565b92915050565b6000602082019050818103600083015261e38b818461c724565b905092915050565b600060808201905061e3a8600083018461c792565b92915050565b600060208201905061e3c3600083018461c7a2565b92915050565b600060c08201905061e3de600083018961c7a2565b61e3eb602083018861c687565b61e3f8604083018761def1565b61e405606083018661def1565b61e412608083018561c7c0565b61e41f60a083018461def1565b979650505050505050565b600060a08201905061e43f600083018861c7a2565b61e44c602083018761c7c0565b61e459604083018661c7c0565b61e466606083018561def1565b61e473608083018461def1565b9695505050505050565b600060a08201905061e492600083018861c7a2565b61e49f602083018761def1565b61e4ac604083018661def1565b61e4b9606083018561def1565b61e4c6608083018461c7c0565b9695505050505050565b600060208201905061e4e5600083018461c7c0565b92915050565b600060408201905061e500600083018561c7c0565b61e50d602083018461c7c0565b9392505050565b600060a08201905061e529600083018861c7c0565b61e536602083018761c7c0565b61e543604083018661c687565b61e550606083018561def1565b61e55d608083018461c7a2565b9695505050505050565b600060a08201905061e57c600083018761c7c0565b61e589602083018661c7c0565b61e596604083018561c7c0565b61e5a3606083018461c782565b95945050505050565b600060e08201905061e5c1600083018a61c7c0565b61e5ce602083018961c7c0565b61e5db604083018861c7c0565b61e5e8606083018761c7c0565b61e5f5608083018661c7c0565b61e60260a083018561c7c0565b61e60f60c083018461c7c0565b98975050505050505050565b60006101008201905061e631600083018b61c7c0565b61e63e602083018a61c7c0565b61e64b604083018961c7c0565b61e658606083018861c7c0565b61e665608083018761c7c0565b61e67260a083018661c7c0565b61e67f60c083018561c7c0565b61e68c60e083018461def1565b9998505050505050505050565b600060a08201905061e6ae600083018961c7c0565b61e6bb602083018861c7c0565b61e6c8604083018761c7c0565b61e6d5606083018661c7c0565b818103608083015261e6e881848661c7e6565b9050979650505050505050565b600060a08201905061e70a600083018861c7c0565b61e717602083018761c7c0565b61e724604083018661c7c0565b61e731606083018561c7c0565b61e73e608083018461def1565b9695505050505050565b60006101008201905061e75e600083018a61c7c0565b61e76b602083018961c7c0565b61e778604083018861c7c0565b61e785606083018761c7c0565b61e792608083018661def1565b61e79f60a083018561def1565b61e7ac60c083018461c782565b98975050505050505050565b600060808201905061e7cd600083018861c7c0565b61e7da602083018761c7c0565b61e7e7604083018661c7c0565b818103606083015261e7fa81848661c7e6565b90509695505050505050565b600060a08201905061e81b600083018861c7c0565b61e828602083018761c7c0565b61e835604083018661c7c0565b61e842606083018561def1565b61e84f608083018461def1565b9695505050505050565b600060a08201905061e86e600083018861c7c0565b61e87b602083018761def1565b61e888604083018661c687565b61e895606083018561c687565b61e8a2608083018461c7c0565b9695505050505050565b600060e08201905061e8c1600083018a61c7c0565b61e8ce602083018961def1565b61e8db604083018861c687565b61e8e8606083018761c7c0565b61e8f5608083018661c687565b61e90260a083018561def1565b61e90f60c083018461c7c0565b98975050505050505050565b600060e08201905061e930600083018a61c7c0565b61e93d602083018961def1565b61e94a604083018861c687565b61e957606083018761c7c0565b61e964608083018661c7c0565b61e97160a083018561def1565b61e97e60c083018461def1565b98975050505050505050565b60006101a08201905061e9a0600083018a61c7c0565b61e9ad602083018961def1565b61e9ba604083018861c687565b61e9c7606083018761dee2565b61e9d4608083018661df17565b61e9e160a083018561c7c0565b61e9ee60c083018461c714565b98975050505050505050565b600060e08201905061ea0f600083018a61c7c0565b61ea1c602083018961def1565b61ea29604083018861c687565b61ea36606083018761def1565b61ea43608083018661c7c0565b61ea5060a083018561c7c0565b61ea5d60c083018461c7c0565b98975050505050505050565b60006101208201905061ea7f600083018c61c7c0565b61ea8c602083018b61def1565b61ea99604083018a61c687565b61eaa6606083018961def1565b61eab3608083018861c7c0565b61eac060a083018761c7c0565b61eacd60c083018661c7c0565b61eada60e083018561c7c0565b61eae861010083018461c7c0565b9a9950505050505050505050565b60006101208201905061eb0c600083018c61c7c0565b61eb19602083018b61def1565b61eb26604083018a61c7c0565b61eb33606083018961c7c0565b61eb40608083018861df26565b61eb4d60a083018761def1565b61eb5a60c083018661def1565b61eb6760e083018561def1565b61eb7561010083018461c7c0565b9a9950505050505050505050565b600060608201905061eb98600083018661c7c0565b61eba5602083018561def1565b61ebb2604083018461def1565b949350505050565b600060808201905061ebcf600083018761c7c0565b61ebdc602083018661df26565b61ebe9604083018561c7c0565b61ebf6606083018461c7c0565b95945050505050565b6000602082019050818103600083015261ec19818461c838565b905092915050565b6000602082019050818103600083015261ec3a8161c930565b9050919050565b6000602082019050818103600083015261ec5a8161c970565b9050919050565b6000602082019050818103600083015261ec7a8161c9b0565b9050919050565b6000602082019050818103600083015261ec9a8161c9f0565b9050919050565b6000602082019050818103600083015261ecba8161ca30565b9050919050565b6000602082019050818103600083015261ecda8161ca70565b9050919050565b6000602082019050818103600083015261ecfa8161cab0565b9050919050565b6000602082019050818103600083015261ed1a8161caf0565b9050919050565b6000602082019050818103600083015261ed3a8161cb30565b9050919050565b6000602082019050818103600083015261ed5a8161cb70565b9050919050565b6000602082019050818103600083015261ed7a8161cbb0565b9050919050565b6000602082019050818103600083015261ed9a8161cbf0565b9050919050565b6000602082019050818103600083015261edba8161cc30565b9050919050565b6000602082019050818103600083015261edda8161cc70565b9050919050565b6000602082019050818103600083015261edfa8161ccb0565b9050919050565b6000602082019050818103600083015261ee1a8161ccf0565b9050919050565b6000602082019050818103600083015261ee3a8161cd30565b9050919050565b6000602082019050818103600083015261ee5a8161cd70565b9050919050565b6000602082019050818103600083015261ee7a8161cdb0565b9050919050565b6000602082019050818103600083015261ee9a8161cdf0565b9050919050565b6000602082019050818103600083015261eeba8161ce30565b9050919050565b6000602082019050818103600083015261eeda8161ce70565b9050919050565b6000602082019050818103600083015261eefa8161ceb0565b9050919050565b6000602082019050818103600083015261ef1a8161cef0565b9050919050565b6000602082019050818103600083015261ef3a8161cf30565b9050919050565b6000602082019050818103600083015261ef5a8161cf70565b9050919050565b6000602082019050818103600083015261ef7a8161cfb0565b9050919050565b6000602082019050818103600083015261ef9a8161cff0565b9050919050565b6000602082019050818103600083015261efba8161d030565b9050919050565b6000602082019050818103600083015261efda8161d070565b9050919050565b6000602082019050818103600083015261effa8161d0b0565b9050919050565b6000602082019050818103600083015261f01a8161d0f0565b9050919050565b6000602082019050818103600083015261f03a8161d130565b9050919050565b6000602082019050818103600083015261f05a8161d170565b9050919050565b6000602082019050818103600083015261f07a8161d1b0565b9050919050565b6000602082019050818103600083015261f09a8161d1f0565b9050919050565b6000602082019050818103600083015261f0ba8161d230565b9050919050565b6000602082019050818103600083015261f0da8161d270565b9050919050565b6000602082019050818103600083015261f0fa8161d2b0565b9050919050565b6000602082019050818103600083015261f11a8161d2f0565b9050919050565b6000602082019050818103600083015261f13a8161d330565b9050919050565b6000602082019050818103600083015261f15a8161d370565b9050919050565b6000602082019050818103600083015261f17a8161d3b0565b9050919050565b6000602082019050818103600083015261f19a8161d3f0565b9050919050565b6000602082019050818103600083015261f1ba8161d430565b9050919050565b6000602082019050818103600083015261f1da8161d496565b9050919050565b6000602082019050818103600083015261f1fa8161d4d6565b9050919050565b6000602082019050818103600083015261f21a8161d516565b9050919050565b6000602082019050818103600083015261f23a8161d556565b9050919050565b6000602082019050818103600083015261f25a8161d596565b9050919050565b6000602082019050818103600083015261f27a8161d5d6565b9050919050565b6000602082019050818103600083015261f29a8161d616565b9050919050565b6000602082019050818103600083015261f2ba8161d656565b9050919050565b6000602082019050818103600083015261f2da8161d696565b9050919050565b6000602082019050818103600083015261f2fa8161d6d6565b9050919050565b6000602082019050818103600083015261f31a8161d716565b9050919050565b6000602082019050818103600083015261f33a8161d756565b9050919050565b6000602082019050818103600083015261f35a8161d796565b9050919050565b6000602082019050818103600083015261f37a8161d7fc565b9050919050565b6000602082019050818103600083015261f39a8161d83c565b9050919050565b6000602082019050818103600083015261f3ba8161d87c565b9050919050565b6000602082019050818103600083015261f3da8161d8bc565b9050919050565b6000602082019050818103600083015261f3fa8161d8fc565b9050919050565b6000602082019050818103600083015261f41a8161d93c565b9050919050565b6000602082019050818103600083015261f43a8161d97c565b9050919050565b6000602082019050818103600083015261f45a8161d9bc565b9050919050565b6000602082019050818103600083015261f47a8161da16565b9050919050565b6000602082019050818103600083015261f49a8161da56565b9050919050565b6000602082019050818103600083015261f4ba8161da96565b9050919050565b6000602082019050818103600083015261f4da8161dad6565b9050919050565b6000602082019050818103600083015261f4fa8161db16565b9050919050565b6000602082019050818103600083015261f51a8161db56565b9050919050565b6000602082019050818103600083015261f53a8161dbbc565b9050919050565b6000602082019050818103600083015261f55a8161dbfc565b9050919050565b6000602082019050818103600083015261f57a8161dc3c565b9050919050565b6000602082019050818103600083015261f59a8161dc7c565b9050919050565b6000602082019050818103600083015261f5ba8161dce2565b9050919050565b6000602082019050818103600083015261f5da8161dd22565b9050919050565b6000602082019050818103600083015261f5fa8161dd62565b9050919050565b6000602082019050818103600083015261f61a8161dda2565b9050919050565b6000602082019050818103600083015261f63a8161dde2565b9050919050565b6000602082019050818103600083015261f65a8161de22565b9050919050565b6000602082019050818103600083015261f67a8161de62565b9050919050565b6000602082019050818103600083015261f69a8161dea2565b9050919050565b600060208201905061f6b6600083018461dee2565b92915050565b600060208201905061f6d1600083018461def1565b92915050565b600060408201905061f6ec600083018561def1565b61f6f9602083018461c7c0565b9392505050565b600060e08201905061f715600083018a61def1565b61f722602083018961c7c0565b61f72f604083018861def1565b61f73c606083018761def1565b61f749608083018661def1565b61f75660a083018561def1565b61f76360c083018461def1565b98975050505050505050565b600060a08201905061f784600083018861def1565b61f791602083018761c921565b61f79e604083018661c7c0565b61f7ab606083018561c7c0565b818103608083015261f7bd818461c724565b90509695505050505050565b600060408201905061f7de600083018561def1565b61f7eb602083018461def1565b9392505050565b600060608201905061f807600083018661def1565b61f814602083018561def1565b61f821604083018461c7c0565b949350505050565b600060a08201905061f83e600083018861def1565b61f84b602083018761def1565b61f858604083018661c7c0565b61f865606083018561c7c0565b818103608083015261f877818461c724565b90509695505050505050565b600060a08201905061f898600083018861def1565b61f8a5602083018761def1565b61f8b2604083018661def1565b61f8bf606083018561c7c0565b61f8cc608083018461c7c0565b9695505050505050565b600060208201905061f8eb600083018461df26565b92915050565b600060408201905061f906600083018561df26565b61f913602083018461c7c0565b9392505050565b6000604051905081810181811067ffffffffffffffff8211171561f9415761f94061fd9d565b5b8060405250919050565b600067ffffffffffffffff82111561f9665761f96561fd9d565b5b602082029050602081019050919050565b600067ffffffffffffffff82111561f9925761f99161fd9d565b5b601f19601f8301169050602081019050919050565b6000819050919050565b6000819050602082019050919050565b60008190508160005260206000209050919050565b600060029050919050565b600081519050919050565b600081519050919050565b6000602082019050919050565b6000602082019050919050565b600081905092915050565b600082825260208201905092915050565b600082825260208201905092915050565b600081905092915050565b600082825260208201905092915050565b600061fa658261fb71565b915061fa708361fb71565b9250827fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff0382111561faa55761faa461fd10565b5b828201905092915050565b600061fabb8261fb8f565b915061fac68361fb8f565b92508260ff0382111561fadc5761fadb61fd10565b5b828201905092915050565b600061faf28261fb71565b915061fafd8361fb71565b92508282101561fb105761fb0f61fd10565b5b828203905092915050565b600061fb268261fb51565b9050919050565b60008115159050919050565b6000819050919050565b600061ffff82169050919050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000819050919050565b600067ffffffffffffffff82169050919050565b600060ff82169050919050565b600061fba78261fb71565b9050919050565b82818337600083830152505050565b60005b8381101561fbdb57808201518184015260208101905061fbc0565b8381111561fbea576000848401525b50505050565b600061fbfb8261fb71565b9150600082141561fc0f5761fc0e61fd10565b5b600182039050919050565b6000600282049050600182168061fc3257607f821691505b6020821081141561fc465761fc4561fd6e565b5b50919050565b600061fc578261fb71565b91507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff82141561fc8a5761fc8961fd10565b5b600182019050919050565b600061fca08261fcb1565b9050919050565b6000819050919050565b600061fcbc8261fdea565b9050919050565b6000819050919050565b600061fcd88261fddd565b9050919050565b600061fcea8261fb71565b915061fcf58361fb71565b92508261fd055761fd0461fd3f565b5b828206905092915050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601260045260246000fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052602260045260246000fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b6000601f19601f8301169050919050565b60008160f81b9050919050565b60008160601b9050919050565b61fe008161fb1b565b811461fe0b57600080fd5b50565b61fe178161fb2d565b811461fe2257600080fd5b50565b61fe2e8161fb39565b811461fe3957600080fd5b50565b61fe458161fb43565b811461fe5057600080fd5b50565b61fe5c8161fb71565b811461fe6757600080fd5b50565b61fe738161fb7b565b811461fe7e57600080fd5b5056fea264697066735822122076368cf76ab536a0be7c7cce6e4e3ad274ae593a95725c81bf2e77b209abb47864736f6c63430008000033"

// DeployContracts deploys a new Ethereum contract, binding an instance of Contracts to it.
func DeployContracts(auth *bind.TransactOpts, backend bind.ContractBackend, _roundBlocks *big.Int, _roundTimeout *big.Int, _minDeposit *big.Int, _maxValidators *big.Int, _teeNum *big.Int) (common.Address, *types.Transaction, *Contracts, error) {
	parsed, err := abi.JSON(strings.NewReader(ContractsABI))
	if err != nil {
		return common.Address{}, nil, nil, err
	}

	address, tx, contract, err := bind.DeployContract(auth, parsed, common.FromHex(ContractsBin), backend, _roundBlocks, _roundTimeout, _minDeposit, _maxValidators, _teeNum)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &Contracts{ContractsCaller: ContractsCaller{contract: contract}, ContractsTransactor: ContractsTransactor{contract: contract}, ContractsFilterer: ContractsFilterer{contract: contract}}, nil
}

// Contracts is an auto generated Go binding around an Ethereum contract.
type Contracts struct {
	ContractsCaller     // Read-only binding to the contract
	ContractsTransactor // Write-only binding to the contract
	ContractsFilterer   // Log filterer for contract events
}

// ContractsCaller is an auto generated read-only Go binding around an Ethereum contract.
type ContractsCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractsTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ContractsTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractsFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ContractsFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractsSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ContractsSession struct {
	Contract     *Contracts        // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ContractsCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ContractsCallerSession struct {
	Contract *ContractsCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts    // Call options to use throughout this session
}

// ContractsTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ContractsTransactorSession struct {
	Contract     *ContractsTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// ContractsRaw is an auto generated low-level Go binding around an Ethereum contract.
type ContractsRaw struct {
	Contract *Contracts // Generic contract binding to access the raw methods on
}

// ContractsCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ContractsCallerRaw struct {
	Contract *ContractsCaller // Generic read-only contract binding to access the raw methods on
}

// ContractsTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ContractsTransactorRaw struct {
	Contract *ContractsTransactor // Generic write-only contract binding to access the raw methods on
}

// NewContracts creates a new instance of Contracts, bound to a specific deployed contract.
func NewContracts(address common.Address, backend bind.ContractBackend) (*Contracts, error) {
	contract, err := bindContracts(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Contracts{ContractsCaller: ContractsCaller{contract: contract}, ContractsTransactor: ContractsTransactor{contract: contract}, ContractsFilterer: ContractsFilterer{contract: contract}}, nil
}

// NewContractsCaller creates a new read-only instance of Contracts, bound to a specific deployed contract.
func NewContractsCaller(address common.Address, caller bind.ContractCaller) (*ContractsCaller, error) {
	contract, err := bindContracts(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ContractsCaller{contract: contract}, nil
}

// NewContractsTransactor creates a new write-only instance of Contracts, bound to a specific deployed contract.
func NewContractsTransactor(address common.Address, transactor bind.ContractTransactor) (*ContractsTransactor, error) {
	contract, err := bindContracts(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ContractsTransactor{contract: contract}, nil
}

// NewContractsFilterer creates a new log filterer instance of Contracts, bound to a specific deployed contract.
func NewContractsFilterer(address common.Address, filterer bind.ContractFilterer) (*ContractsFilterer, error) {
	contract, err := bindContracts(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ContractsFilterer{contract: contract}, nil
}

// bindContracts binds a generic wrapper to an already deployed contract.
func bindContracts(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(ContractsABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Contracts *ContractsRaw) Call(opts *bind.CallOpts, result interface{}, method string, params ...interface{}) error {
	return _Contracts.Contract.ContractsCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Contracts *ContractsRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contracts.Contract.ContractsTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Contracts *ContractsRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Contracts.Contract.ContractsTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Contracts *ContractsCallerRaw) Call(opts *bind.CallOpts, result interface{}, method string, params ...interface{}) error {
	return _Contracts.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Contracts *ContractsTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contracts.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Contracts *ContractsTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Contracts.Contract.contract.Transact(opts, method, params...)
}

// FINALITYBLOCKDELAY is a free data retrieval call binding the contract method 0xac8f553f.
//
// Solidity: function FINALITY_BLOCK_DELAY() constant returns(uint256)
func (_Contracts *ContractsCaller) FINALITYBLOCKDELAY(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "FINALITY_BLOCK_DELAY")
	return *ret0, err
}

// FINALITYBLOCKDELAY is a free data retrieval call binding the contract method 0xac8f553f.
//
// Solidity: function FINALITY_BLOCK_DELAY() constant returns(uint256)
func (_Contracts *ContractsSession) FINALITYBLOCKDELAY() (*big.Int, error) {
	return _Contracts.Contract.FINALITYBLOCKDELAY(&_Contracts.CallOpts)
}

// FINALITYBLOCKDELAY is a free data retrieval call binding the contract method 0xac8f553f.
//
// Solidity: function FINALITY_BLOCK_DELAY() constant returns(uint256)
func (_Contracts *ContractsCallerSession) FINALITYBLOCKDELAY() (*big.Int, error) {
	return _Contracts.Contract.FINALITYBLOCKDELAY(&_Contracts.CallOpts)
}

// MINDEPOSIT is a free data retrieval call binding the contract method 0xe1e158a5.
//
// Solidity: function MIN_DEPOSIT() constant returns(uint256)
func (_Contracts *ContractsCaller) MINDEPOSIT(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "MIN_DEPOSIT")
	return *ret0, err
}

// MINDEPOSIT is a free data retrieval call binding the contract method 0xe1e158a5.
//
// Solidity: function MIN_DEPOSIT() constant returns(uint256)
func (_Contracts *ContractsSession) MINDEPOSIT() (*big.Int, error) {
	return _Contracts.Contract.MINDEPOSIT(&_Contracts.CallOpts)
}

// MINDEPOSIT is a free data retrieval call binding the contract method 0xe1e158a5.
//
// Solidity: function MIN_DEPOSIT() constant returns(uint256)
func (_Contracts *ContractsCallerSession) MINDEPOSIT() (*big.Int, error) {
	return _Contracts.Contract.MINDEPOSIT(&_Contracts.CallOpts)
}

// REDACTPROTOCOLVERSION is a free data retrieval call binding the contract method 0x448c4fc0.
//
// Solidity: function REDACT_PROTOCOL_VERSION() constant returns(uint16)
func (_Contracts *ContractsCaller) REDACTPROTOCOLVERSION(opts *bind.CallOpts) (uint16, error) {
	var (
		ret0 = new(uint16)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "REDACT_PROTOCOL_VERSION")
	return *ret0, err
}

// REDACTPROTOCOLVERSION is a free data retrieval call binding the contract method 0x448c4fc0.
//
// Solidity: function REDACT_PROTOCOL_VERSION() constant returns(uint16)
func (_Contracts *ContractsSession) REDACTPROTOCOLVERSION() (uint16, error) {
	return _Contracts.Contract.REDACTPROTOCOLVERSION(&_Contracts.CallOpts)
}

// REDACTPROTOCOLVERSION is a free data retrieval call binding the contract method 0x448c4fc0.
//
// Solidity: function REDACT_PROTOCOL_VERSION() constant returns(uint16)
func (_Contracts *ContractsCallerSession) REDACTPROTOCOLVERSION() (uint16, error) {
	return _Contracts.Contract.REDACTPROTOCOLVERSION(&_Contracts.CallOpts)
}

// ROTATIONPREPAREBLOCKS is a free data retrieval call binding the contract method 0x896ae026.
//
// Solidity: function ROTATION_PREPARE_BLOCKS() constant returns(uint256)
func (_Contracts *ContractsCaller) ROTATIONPREPAREBLOCKS(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "ROTATION_PREPARE_BLOCKS")
	return *ret0, err
}

// ROTATIONPREPAREBLOCKS is a free data retrieval call binding the contract method 0x896ae026.
//
// Solidity: function ROTATION_PREPARE_BLOCKS() constant returns(uint256)
func (_Contracts *ContractsSession) ROTATIONPREPAREBLOCKS() (*big.Int, error) {
	return _Contracts.Contract.ROTATIONPREPAREBLOCKS(&_Contracts.CallOpts)
}

// ROTATIONPREPAREBLOCKS is a free data retrieval call binding the contract method 0x896ae026.
//
// Solidity: function ROTATION_PREPARE_BLOCKS() constant returns(uint256)
func (_Contracts *ContractsCallerSession) ROTATIONPREPAREBLOCKS() (*big.Int, error) {
	return _Contracts.Contract.ROTATIONPREPAREBLOCKS(&_Contracts.CallOpts)
}

// ActiveSetHashByEpoch is a free data retrieval call binding the contract method 0x6f1e0ae4.
//
// Solidity: function activeSetHashByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsCaller) ActiveSetHashByEpoch(opts *bind.CallOpts, arg0 *big.Int) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "activeSetHashByEpoch", arg0)
	return *ret0, err
}

// ActiveSetHashByEpoch is a free data retrieval call binding the contract method 0x6f1e0ae4.
//
// Solidity: function activeSetHashByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsSession) ActiveSetHashByEpoch(arg0 *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ActiveSetHashByEpoch(&_Contracts.CallOpts, arg0)
}

// ActiveSetHashByEpoch is a free data retrieval call binding the contract method 0x6f1e0ae4.
//
// Solidity: function activeSetHashByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ActiveSetHashByEpoch(arg0 *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ActiveSetHashByEpoch(&_Contracts.CallOpts, arg0)
}

// ActiveTEEs is a free data retrieval call binding the contract method 0xc4900950.
//
// Solidity: function activeTEEs(uint256 ) constant returns(uint256)
func (_Contracts *ContractsCaller) ActiveTEEs(opts *bind.CallOpts, arg0 *big.Int) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "activeTEEs", arg0)
	return *ret0, err
}

// ActiveTEEs is a free data retrieval call binding the contract method 0xc4900950.
//
// Solidity: function activeTEEs(uint256 ) constant returns(uint256)
func (_Contracts *ContractsSession) ActiveTEEs(arg0 *big.Int) (*big.Int, error) {
	return _Contracts.Contract.ActiveTEEs(&_Contracts.CallOpts, arg0)
}

// ActiveTEEs is a free data retrieval call binding the contract method 0xc4900950.
//
// Solidity: function activeTEEs(uint256 ) constant returns(uint256)
func (_Contracts *ContractsCallerSession) ActiveTEEs(arg0 *big.Int) (*big.Int, error) {
	return _Contracts.Contract.ActiveTEEs(&_Contracts.CallOpts, arg0)
}

// CommitteePubKey is a free data retrieval call binding the contract method 0xb1f2dd95.
//
// Solidity: function committeePubKey(uint256 ) constant returns(uint256)
func (_Contracts *ContractsCaller) CommitteePubKey(opts *bind.CallOpts, arg0 *big.Int) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "committeePubKey", arg0)
	return *ret0, err
}

// CommitteePubKey is a free data retrieval call binding the contract method 0xb1f2dd95.
//
// Solidity: function committeePubKey(uint256 ) constant returns(uint256)
func (_Contracts *ContractsSession) CommitteePubKey(arg0 *big.Int) (*big.Int, error) {
	return _Contracts.Contract.CommitteePubKey(&_Contracts.CallOpts, arg0)
}

// CommitteePubKey is a free data retrieval call binding the contract method 0xb1f2dd95.
//
// Solidity: function committeePubKey(uint256 ) constant returns(uint256)
func (_Contracts *ContractsCallerSession) CommitteePubKey(arg0 *big.Int) (*big.Int, error) {
	return _Contracts.Contract.CommitteePubKey(&_Contracts.CallOpts, arg0)
}

// CommitteePubKeyVersion is a free data retrieval call binding the contract method 0x6a495d1c.
//
// Solidity: function committeePubKeyVersion() constant returns(uint256)
func (_Contracts *ContractsCaller) CommitteePubKeyVersion(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "committeePubKeyVersion")
	return *ret0, err
}

// CommitteePubKeyVersion is a free data retrieval call binding the contract method 0x6a495d1c.
//
// Solidity: function committeePubKeyVersion() constant returns(uint256)
func (_Contracts *ContractsSession) CommitteePubKeyVersion() (*big.Int, error) {
	return _Contracts.Contract.CommitteePubKeyVersion(&_Contracts.CallOpts)
}

// CommitteePubKeyVersion is a free data retrieval call binding the contract method 0x6a495d1c.
//
// Solidity: function committeePubKeyVersion() constant returns(uint256)
func (_Contracts *ContractsCallerSession) CommitteePubKeyVersion() (*big.Int, error) {
	return _Contracts.Contract.CommitteePubKeyVersion(&_Contracts.CallOpts)
}

// ComputeAuthorizationDigest is a free data retrieval call binding the contract method 0x1ddb9ba0.
//
// Solidity: function computeAuthorizationDigest(bytes32 dReq, bytes32 dPolicy, uint256 pubKeyVersion, uint256 expiresAt) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeAuthorizationDigest(opts *bind.CallOpts, dReq [32]byte, dPolicy [32]byte, pubKeyVersion *big.Int, expiresAt *big.Int) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeAuthorizationDigest", dReq, dPolicy, pubKeyVersion, expiresAt)
	return *ret0, err
}

// ComputeAuthorizationDigest is a free data retrieval call binding the contract method 0x1ddb9ba0.
//
// Solidity: function computeAuthorizationDigest(bytes32 dReq, bytes32 dPolicy, uint256 pubKeyVersion, uint256 expiresAt) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeAuthorizationDigest(dReq [32]byte, dPolicy [32]byte, pubKeyVersion *big.Int, expiresAt *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ComputeAuthorizationDigest(&_Contracts.CallOpts, dReq, dPolicy, pubKeyVersion, expiresAt)
}

// ComputeAuthorizationDigest is a free data retrieval call binding the contract method 0x1ddb9ba0.
//
// Solidity: function computeAuthorizationDigest(bytes32 dReq, bytes32 dPolicy, uint256 pubKeyVersion, uint256 expiresAt) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeAuthorizationDigest(dReq [32]byte, dPolicy [32]byte, pubKeyVersion *big.Int, expiresAt *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ComputeAuthorizationDigest(&_Contracts.CallOpts, dReq, dPolicy, pubKeyVersion, expiresAt)
}

// ComputeDReq is a free data retrieval call binding the contract method 0x8ec6ef74.
//
// Solidity: function computeDReq(PermissionManagerV3ReqFields req) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeDReq(opts *bind.CallOpts, req PermissionManagerV3ReqFields) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeDReq", req)
	return *ret0, err
}

// ComputeDReq is a free data retrieval call binding the contract method 0x8ec6ef74.
//
// Solidity: function computeDReq(PermissionManagerV3ReqFields req) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeDReq(req PermissionManagerV3ReqFields) ([32]byte, error) {
	return _Contracts.Contract.ComputeDReq(&_Contracts.CallOpts, req)
}

// ComputeDReq is a free data retrieval call binding the contract method 0x8ec6ef74.
//
// Solidity: function computeDReq(PermissionManagerV3ReqFields req) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeDReq(req PermissionManagerV3ReqFields) ([32]byte, error) {
	return _Contracts.Contract.ComputeDReq(&_Contracts.CallOpts, req)
}

// ComputeEraseProofDigest is a free data retrieval call binding the contract method 0x1ed1869c.
//
// Solidity: function computeEraseProofDigest(bytes32 sigPubKey, address teeOwner, uint256 epoch, bytes32 eraseProofHash) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeEraseProofDigest(opts *bind.CallOpts, sigPubKey [32]byte, teeOwner common.Address, epoch *big.Int, eraseProofHash [32]byte) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeEraseProofDigest", sigPubKey, teeOwner, epoch, eraseProofHash)
	return *ret0, err
}

// ComputeEraseProofDigest is a free data retrieval call binding the contract method 0x1ed1869c.
//
// Solidity: function computeEraseProofDigest(bytes32 sigPubKey, address teeOwner, uint256 epoch, bytes32 eraseProofHash) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeEraseProofDigest(sigPubKey [32]byte, teeOwner common.Address, epoch *big.Int, eraseProofHash [32]byte) ([32]byte, error) {
	return _Contracts.Contract.ComputeEraseProofDigest(&_Contracts.CallOpts, sigPubKey, teeOwner, epoch, eraseProofHash)
}

// ComputeEraseProofDigest is a free data retrieval call binding the contract method 0x1ed1869c.
//
// Solidity: function computeEraseProofDigest(bytes32 sigPubKey, address teeOwner, uint256 epoch, bytes32 eraseProofHash) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeEraseProofDigest(sigPubKey [32]byte, teeOwner common.Address, epoch *big.Int, eraseProofHash [32]byte) ([32]byte, error) {
	return _Contracts.Contract.ComputeEraseProofDigest(&_Contracts.CallOpts, sigPubKey, teeOwner, epoch, eraseProofHash)
}

// ComputePkCHDigest is a free data retrieval call binding the contract method 0xc898d32a.
//
// Solidity: function computePkCHDigest(address teeOwner, bytes _pkCH) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputePkCHDigest(opts *bind.CallOpts, teeOwner common.Address, _pkCH []byte) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computePkCHDigest", teeOwner, _pkCH)
	return *ret0, err
}

// ComputePkCHDigest is a free data retrieval call binding the contract method 0xc898d32a.
//
// Solidity: function computePkCHDigest(address teeOwner, bytes _pkCH) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputePkCHDigest(teeOwner common.Address, _pkCH []byte) ([32]byte, error) {
	return _Contracts.Contract.ComputePkCHDigest(&_Contracts.CallOpts, teeOwner, _pkCH)
}

// ComputePkCHDigest is a free data retrieval call binding the contract method 0xc898d32a.
//
// Solidity: function computePkCHDigest(address teeOwner, bytes _pkCH) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputePkCHDigest(teeOwner common.Address, _pkCH []byte) ([32]byte, error) {
	return _Contracts.Contract.ComputePkCHDigest(&_Contracts.CallOpts, teeOwner, _pkCH)
}

// ComputeRedactionProofHash is a free data retrieval call binding the contract method 0xc4436aa9.
//
// Solidity: function computeRedactionProofHash(bytes32 reqID, bytes32 dReq, bytes32 activeSetHash, bytes piReqA) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeRedactionProofHash(opts *bind.CallOpts, reqID [32]byte, dReq [32]byte, activeSetHash [32]byte, piReqA []byte) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeRedactionProofHash", reqID, dReq, activeSetHash, piReqA)
	return *ret0, err
}

// ComputeRedactionProofHash is a free data retrieval call binding the contract method 0xc4436aa9.
//
// Solidity: function computeRedactionProofHash(bytes32 reqID, bytes32 dReq, bytes32 activeSetHash, bytes piReqA) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeRedactionProofHash(reqID [32]byte, dReq [32]byte, activeSetHash [32]byte, piReqA []byte) ([32]byte, error) {
	return _Contracts.Contract.ComputeRedactionProofHash(&_Contracts.CallOpts, reqID, dReq, activeSetHash, piReqA)
}

// ComputeRedactionProofHash is a free data retrieval call binding the contract method 0xc4436aa9.
//
// Solidity: function computeRedactionProofHash(bytes32 reqID, bytes32 dReq, bytes32 activeSetHash, bytes piReqA) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeRedactionProofHash(reqID [32]byte, dReq [32]byte, activeSetHash [32]byte, piReqA []byte) ([32]byte, error) {
	return _Contracts.Contract.ComputeRedactionProofHash(&_Contracts.CallOpts, reqID, dReq, activeSetHash, piReqA)
}

// ComputeRejectionDigest is a free data retrieval call binding the contract method 0x2fce7cf4.
//
// Solidity: function computeRejectionDigest(bytes32 dReq, bytes32 dPolicy, bytes32 reasonHash, uint256 pubKeyVersion) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeRejectionDigest(opts *bind.CallOpts, dReq [32]byte, dPolicy [32]byte, reasonHash [32]byte, pubKeyVersion *big.Int) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeRejectionDigest", dReq, dPolicy, reasonHash, pubKeyVersion)
	return *ret0, err
}

// ComputeRejectionDigest is a free data retrieval call binding the contract method 0x2fce7cf4.
//
// Solidity: function computeRejectionDigest(bytes32 dReq, bytes32 dPolicy, bytes32 reasonHash, uint256 pubKeyVersion) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeRejectionDigest(dReq [32]byte, dPolicy [32]byte, reasonHash [32]byte, pubKeyVersion *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ComputeRejectionDigest(&_Contracts.CallOpts, dReq, dPolicy, reasonHash, pubKeyVersion)
}

// ComputeRejectionDigest is a free data retrieval call binding the contract method 0x2fce7cf4.
//
// Solidity: function computeRejectionDigest(bytes32 dReq, bytes32 dPolicy, bytes32 reasonHash, uint256 pubKeyVersion) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeRejectionDigest(dReq [32]byte, dPolicy [32]byte, reasonHash [32]byte, pubKeyVersion *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ComputeRejectionDigest(&_Contracts.CallOpts, dReq, dPolicy, reasonHash, pubKeyVersion)
}

// ComputeRevocationDigest is a free data retrieval call binding the contract method 0xfa7cfda0.
//
// Solidity: function computeRevocationDigest(bytes32 sigPubKey, bytes32 reasonHash, uint256 slashAmount, uint256 pubKeyVersion) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeRevocationDigest(opts *bind.CallOpts, sigPubKey [32]byte, reasonHash [32]byte, slashAmount *big.Int, pubKeyVersion *big.Int) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeRevocationDigest", sigPubKey, reasonHash, slashAmount, pubKeyVersion)
	return *ret0, err
}

// ComputeRevocationDigest is a free data retrieval call binding the contract method 0xfa7cfda0.
//
// Solidity: function computeRevocationDigest(bytes32 sigPubKey, bytes32 reasonHash, uint256 slashAmount, uint256 pubKeyVersion) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeRevocationDigest(sigPubKey [32]byte, reasonHash [32]byte, slashAmount *big.Int, pubKeyVersion *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ComputeRevocationDigest(&_Contracts.CallOpts, sigPubKey, reasonHash, slashAmount, pubKeyVersion)
}

// ComputeRevocationDigest is a free data retrieval call binding the contract method 0xfa7cfda0.
//
// Solidity: function computeRevocationDigest(bytes32 sigPubKey, bytes32 reasonHash, uint256 slashAmount, uint256 pubKeyVersion) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeRevocationDigest(sigPubKey [32]byte, reasonHash [32]byte, slashAmount *big.Int, pubKeyVersion *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ComputeRevocationDigest(&_Contracts.CallOpts, sigPubKey, reasonHash, slashAmount, pubKeyVersion)
}

// ComputeSeed is a free data retrieval call binding the contract method 0xef64d198.
//
// Solidity: function computeSeed(bytes32 finalityBlockHash, uint256 epoch) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeSeed(opts *bind.CallOpts, finalityBlockHash [32]byte, epoch *big.Int) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeSeed", finalityBlockHash, epoch)
	return *ret0, err
}

// ComputeSeed is a free data retrieval call binding the contract method 0xef64d198.
//
// Solidity: function computeSeed(bytes32 finalityBlockHash, uint256 epoch) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeSeed(finalityBlockHash [32]byte, epoch *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ComputeSeed(&_Contracts.CallOpts, finalityBlockHash, epoch)
}

// ComputeSeed is a free data retrieval call binding the contract method 0xef64d198.
//
// Solidity: function computeSeed(bytes32 finalityBlockHash, uint256 epoch) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeSeed(finalityBlockHash [32]byte, epoch *big.Int) ([32]byte, error) {
	return _Contracts.Contract.ComputeSeed(&_Contracts.CallOpts, finalityBlockHash, epoch)
}

// ComputeShareProofDigest is a free data retrieval call binding the contract method 0xc64aa5d6.
//
// Solidity: function computeShareProofDigest(bytes32 dReq, bytes sI) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeShareProofDigest(opts *bind.CallOpts, dReq [32]byte, sI []byte) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeShareProofDigest", dReq, sI)
	return *ret0, err
}

// ComputeShareProofDigest is a free data retrieval call binding the contract method 0xc64aa5d6.
//
// Solidity: function computeShareProofDigest(bytes32 dReq, bytes sI) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeShareProofDigest(dReq [32]byte, sI []byte) ([32]byte, error) {
	return _Contracts.Contract.ComputeShareProofDigest(&_Contracts.CallOpts, dReq, sI)
}

// ComputeShareProofDigest is a free data retrieval call binding the contract method 0xc64aa5d6.
//
// Solidity: function computeShareProofDigest(bytes32 dReq, bytes sI) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeShareProofDigest(dReq [32]byte, sI []byte) ([32]byte, error) {
	return _Contracts.Contract.ComputeShareProofDigest(&_Contracts.CallOpts, dReq, sI)
}

// ComputeVRFDigest is a free data retrieval call binding the contract method 0x2f2c8165.
//
// Solidity: function computeVRFDigest(uint256 epoch, bytes32 sigPubKey, bytes32 vrfPubKey, bytes32 seed) constant returns(bytes32)
func (_Contracts *ContractsCaller) ComputeVRFDigest(opts *bind.CallOpts, epoch *big.Int, sigPubKey [32]byte, vrfPubKey [32]byte, seed [32]byte) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "computeVRFDigest", epoch, sigPubKey, vrfPubKey, seed)
	return *ret0, err
}

// ComputeVRFDigest is a free data retrieval call binding the contract method 0x2f2c8165.
//
// Solidity: function computeVRFDigest(uint256 epoch, bytes32 sigPubKey, bytes32 vrfPubKey, bytes32 seed) constant returns(bytes32)
func (_Contracts *ContractsSession) ComputeVRFDigest(epoch *big.Int, sigPubKey [32]byte, vrfPubKey [32]byte, seed [32]byte) ([32]byte, error) {
	return _Contracts.Contract.ComputeVRFDigest(&_Contracts.CallOpts, epoch, sigPubKey, vrfPubKey, seed)
}

// ComputeVRFDigest is a free data retrieval call binding the contract method 0x2f2c8165.
//
// Solidity: function computeVRFDigest(uint256 epoch, bytes32 sigPubKey, bytes32 vrfPubKey, bytes32 seed) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) ComputeVRFDigest(epoch *big.Int, sigPubKey [32]byte, vrfPubKey [32]byte, seed [32]byte) ([32]byte, error) {
	return _Contracts.Contract.ComputeVRFDigest(&_Contracts.CallOpts, epoch, sigPubKey, vrfPubKey, seed)
}

// CurrentEpoch is a free data retrieval call binding the contract method 0x76671808.
//
// Solidity: function currentEpoch() constant returns(uint256)
func (_Contracts *ContractsCaller) CurrentEpoch(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "currentEpoch")
	return *ret0, err
}

// CurrentEpoch is a free data retrieval call binding the contract method 0x76671808.
//
// Solidity: function currentEpoch() constant returns(uint256)
func (_Contracts *ContractsSession) CurrentEpoch() (*big.Int, error) {
	return _Contracts.Contract.CurrentEpoch(&_Contracts.CallOpts)
}

// CurrentEpoch is a free data retrieval call binding the contract method 0x76671808.
//
// Solidity: function currentEpoch() constant returns(uint256)
func (_Contracts *ContractsCallerSession) CurrentEpoch() (*big.Int, error) {
	return _Contracts.Contract.CurrentEpoch(&_Contracts.CallOpts)
}

// EpochLength is a free data retrieval call binding the contract method 0x57d775f8.
//
// Solidity: function epochLength() constant returns(uint256)
func (_Contracts *ContractsCaller) EpochLength(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "epochLength")
	return *ret0, err
}

// EpochLength is a free data retrieval call binding the contract method 0x57d775f8.
//
// Solidity: function epochLength() constant returns(uint256)
func (_Contracts *ContractsSession) EpochLength() (*big.Int, error) {
	return _Contracts.Contract.EpochLength(&_Contracts.CallOpts)
}

// EpochLength is a free data retrieval call binding the contract method 0x57d775f8.
//
// Solidity: function epochLength() constant returns(uint256)
func (_Contracts *ContractsCallerSession) EpochLength() (*big.Int, error) {
	return _Contracts.Contract.EpochLength(&_Contracts.CallOpts)
}

// EraseProofGraceBlocks is a free data retrieval call binding the contract method 0x5f2e4767.
//
// Solidity: function eraseProofGraceBlocks() constant returns(uint256)
func (_Contracts *ContractsCaller) EraseProofGraceBlocks(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "eraseProofGraceBlocks")
	return *ret0, err
}

// EraseProofGraceBlocks is a free data retrieval call binding the contract method 0x5f2e4767.
//
// Solidity: function eraseProofGraceBlocks() constant returns(uint256)
func (_Contracts *ContractsSession) EraseProofGraceBlocks() (*big.Int, error) {
	return _Contracts.Contract.EraseProofGraceBlocks(&_Contracts.CallOpts)
}

// EraseProofGraceBlocks is a free data retrieval call binding the contract method 0x5f2e4767.
//
// Solidity: function eraseProofGraceBlocks() constant returns(uint256)
func (_Contracts *ContractsCallerSession) EraseProofGraceBlocks() (*big.Int, error) {
	return _Contracts.Contract.EraseProofGraceBlocks(&_Contracts.CallOpts)
}

// FinalityBlockByEpoch is a free data retrieval call binding the contract method 0xbe693f3e.
//
// Solidity: function finalityBlockByEpoch(uint256 ) constant returns(uint256)
func (_Contracts *ContractsCaller) FinalityBlockByEpoch(opts *bind.CallOpts, arg0 *big.Int) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "finalityBlockByEpoch", arg0)
	return *ret0, err
}

// FinalityBlockByEpoch is a free data retrieval call binding the contract method 0xbe693f3e.
//
// Solidity: function finalityBlockByEpoch(uint256 ) constant returns(uint256)
func (_Contracts *ContractsSession) FinalityBlockByEpoch(arg0 *big.Int) (*big.Int, error) {
	return _Contracts.Contract.FinalityBlockByEpoch(&_Contracts.CallOpts, arg0)
}

// FinalityBlockByEpoch is a free data retrieval call binding the contract method 0xbe693f3e.
//
// Solidity: function finalityBlockByEpoch(uint256 ) constant returns(uint256)
func (_Contracts *ContractsCallerSession) FinalityBlockByEpoch(arg0 *big.Int) (*big.Int, error) {
	return _Contracts.Contract.FinalityBlockByEpoch(&_Contracts.CallOpts, arg0)
}

// FinalityBlockHashByEpoch is a free data retrieval call binding the contract method 0x7c94ada4.
//
// Solidity: function finalityBlockHashByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsCaller) FinalityBlockHashByEpoch(opts *bind.CallOpts, arg0 *big.Int) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "finalityBlockHashByEpoch", arg0)
	return *ret0, err
}

// FinalityBlockHashByEpoch is a free data retrieval call binding the contract method 0x7c94ada4.
//
// Solidity: function finalityBlockHashByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsSession) FinalityBlockHashByEpoch(arg0 *big.Int) ([32]byte, error) {
	return _Contracts.Contract.FinalityBlockHashByEpoch(&_Contracts.CallOpts, arg0)
}

// FinalityBlockHashByEpoch is a free data retrieval call binding the contract method 0x7c94ada4.
//
// Solidity: function finalityBlockHashByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) FinalityBlockHashByEpoch(arg0 *big.Int) ([32]byte, error) {
	return _Contracts.Contract.FinalityBlockHashByEpoch(&_Contracts.CallOpts, arg0)
}

// GenesisTimestamp is a free data retrieval call binding the contract method 0xcacf66ab.
//
// Solidity: function genesisTimestamp() constant returns(uint256)
func (_Contracts *ContractsCaller) GenesisTimestamp(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "genesisTimestamp")
	return *ret0, err
}

// GenesisTimestamp is a free data retrieval call binding the contract method 0xcacf66ab.
//
// Solidity: function genesisTimestamp() constant returns(uint256)
func (_Contracts *ContractsSession) GenesisTimestamp() (*big.Int, error) {
	return _Contracts.Contract.GenesisTimestamp(&_Contracts.CallOpts)
}

// GenesisTimestamp is a free data retrieval call binding the contract method 0xcacf66ab.
//
// Solidity: function genesisTimestamp() constant returns(uint256)
func (_Contracts *ContractsCallerSession) GenesisTimestamp() (*big.Int, error) {
	return _Contracts.Contract.GenesisTimestamp(&_Contracts.CallOpts)
}

// GetActiveSet is a free data retrieval call binding the contract method 0x61cc9266.
//
// Solidity: function getActiveSet(uint256 epoch) constant returns(bytes32[])
func (_Contracts *ContractsCaller) GetActiveSet(opts *bind.CallOpts, epoch *big.Int) ([][32]byte, error) {
	var (
		ret0 = new([][32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "getActiveSet", epoch)
	return *ret0, err
}

// GetActiveSet is a free data retrieval call binding the contract method 0x61cc9266.
//
// Solidity: function getActiveSet(uint256 epoch) constant returns(bytes32[])
func (_Contracts *ContractsSession) GetActiveSet(epoch *big.Int) ([][32]byte, error) {
	return _Contracts.Contract.GetActiveSet(&_Contracts.CallOpts, epoch)
}

// GetActiveSet is a free data retrieval call binding the contract method 0x61cc9266.
//
// Solidity: function getActiveSet(uint256 epoch) constant returns(bytes32[])
func (_Contracts *ContractsCallerSession) GetActiveSet(epoch *big.Int) ([][32]byte, error) {
	return _Contracts.Contract.GetActiveSet(&_Contracts.CallOpts, epoch)
}

// GetActiveSetHash is a free data retrieval call binding the contract method 0xd93e237e.
//
// Solidity: function getActiveSetHash(uint256 epoch) constant returns(bytes32)
func (_Contracts *ContractsCaller) GetActiveSetHash(opts *bind.CallOpts, epoch *big.Int) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "getActiveSetHash", epoch)
	return *ret0, err
}

// GetActiveSetHash is a free data retrieval call binding the contract method 0xd93e237e.
//
// Solidity: function getActiveSetHash(uint256 epoch) constant returns(bytes32)
func (_Contracts *ContractsSession) GetActiveSetHash(epoch *big.Int) ([32]byte, error) {
	return _Contracts.Contract.GetActiveSetHash(&_Contracts.CallOpts, epoch)
}

// GetActiveSetHash is a free data retrieval call binding the contract method 0xd93e237e.
//
// Solidity: function getActiveSetHash(uint256 epoch) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) GetActiveSetHash(epoch *big.Int) ([32]byte, error) {
	return _Contracts.Contract.GetActiveSetHash(&_Contracts.CallOpts, epoch)
}

// GetActiveTEEs is a free data retrieval call binding the contract method 0xd046a7fa.
//
// Solidity: function getActiveTEEs() constant returns(bytes32[])
func (_Contracts *ContractsCaller) GetActiveTEEs(opts *bind.CallOpts) ([][32]byte, error) {
	var (
		ret0 = new([][32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "getActiveTEEs")
	return *ret0, err
}

// GetActiveTEEs is a free data retrieval call binding the contract method 0xd046a7fa.
//
// Solidity: function getActiveTEEs() constant returns(bytes32[])
func (_Contracts *ContractsSession) GetActiveTEEs() ([][32]byte, error) {
	return _Contracts.Contract.GetActiveTEEs(&_Contracts.CallOpts)
}

// GetActiveTEEs is a free data retrieval call binding the contract method 0xd046a7fa.
//
// Solidity: function getActiveTEEs() constant returns(bytes32[])
func (_Contracts *ContractsCallerSession) GetActiveTEEs() ([][32]byte, error) {
	return _Contracts.Contract.GetActiveTEEs(&_Contracts.CallOpts)
}

// GetCandidateTEEs is a free data retrieval call binding the contract method 0xb113a4cb.
//
// Solidity: function getCandidateTEEs() constant returns(bytes32[])
func (_Contracts *ContractsCaller) GetCandidateTEEs(opts *bind.CallOpts) ([][32]byte, error) {
	var (
		ret0 = new([][32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "getCandidateTEEs")
	return *ret0, err
}

// GetCandidateTEEs is a free data retrieval call binding the contract method 0xb113a4cb.
//
// Solidity: function getCandidateTEEs() constant returns(bytes32[])
func (_Contracts *ContractsSession) GetCandidateTEEs() ([][32]byte, error) {
	return _Contracts.Contract.GetCandidateTEEs(&_Contracts.CallOpts)
}

// GetCandidateTEEs is a free data retrieval call binding the contract method 0xb113a4cb.
//
// Solidity: function getCandidateTEEs() constant returns(bytes32[])
func (_Contracts *ContractsCallerSession) GetCandidateTEEs() ([][32]byte, error) {
	return _Contracts.Contract.GetCandidateTEEs(&_Contracts.CallOpts)
}

// GetEffectiveRedactStatus is a free data retrieval call binding the contract method 0x587d9d5f.
//
// Solidity: function getEffectiveRedactStatus(bytes32 reqID) constant returns(uint8)
func (_Contracts *ContractsCaller) GetEffectiveRedactStatus(opts *bind.CallOpts, reqID [32]byte) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "getEffectiveRedactStatus", reqID)
	return *ret0, err
}

// GetEffectiveRedactStatus is a free data retrieval call binding the contract method 0x587d9d5f.
//
// Solidity: function getEffectiveRedactStatus(bytes32 reqID) constant returns(uint8)
func (_Contracts *ContractsSession) GetEffectiveRedactStatus(reqID [32]byte) (uint8, error) {
	return _Contracts.Contract.GetEffectiveRedactStatus(&_Contracts.CallOpts, reqID)
}

// GetEffectiveRedactStatus is a free data retrieval call binding the contract method 0x587d9d5f.
//
// Solidity: function getEffectiveRedactStatus(bytes32 reqID) constant returns(uint8)
func (_Contracts *ContractsCallerSession) GetEffectiveRedactStatus(reqID [32]byte) (uint8, error) {
	return _Contracts.Contract.GetEffectiveRedactStatus(&_Contracts.CallOpts, reqID)
}

// GetPendingTEE is a free data retrieval call binding the contract method 0x40b74edd.
//
// Solidity: function getPendingTEE(address _owner) constant returns(bool, uint256, uint256, uint256, bytes32)
func (_Contracts *ContractsCaller) GetPendingTEE(opts *bind.CallOpts, _owner common.Address) (bool, *big.Int, *big.Int, *big.Int, [32]byte, error) {
	var (
		ret0 = new(bool)
		ret1 = new(*big.Int)
		ret2 = new(*big.Int)
		ret3 = new(*big.Int)
		ret4 = new([32]byte)
	)
	out := &[]interface{}{
		ret0,
		ret1,
		ret2,
		ret3,
		ret4,
	}
	err := _Contracts.contract.Call(opts, out, "getPendingTEE", _owner)
	return *ret0, *ret1, *ret2, *ret3, *ret4, err
}

// GetPendingTEE is a free data retrieval call binding the contract method 0x40b74edd.
//
// Solidity: function getPendingTEE(address _owner) constant returns(bool, uint256, uint256, uint256, bytes32)
func (_Contracts *ContractsSession) GetPendingTEE(_owner common.Address) (bool, *big.Int, *big.Int, *big.Int, [32]byte, error) {
	return _Contracts.Contract.GetPendingTEE(&_Contracts.CallOpts, _owner)
}

// GetPendingTEE is a free data retrieval call binding the contract method 0x40b74edd.
//
// Solidity: function getPendingTEE(address _owner) constant returns(bool, uint256, uint256, uint256, bytes32)
func (_Contracts *ContractsCallerSession) GetPendingTEE(_owner common.Address) (bool, *big.Int, *big.Int, *big.Int, [32]byte, error) {
	return _Contracts.Contract.GetPendingTEE(&_Contracts.CallOpts, _owner)
}

// GetPkCHHash is a free data retrieval call binding the contract method 0xcab43ea8.
//
// Solidity: function getPkCHHash() constant returns(bytes32)
func (_Contracts *ContractsCaller) GetPkCHHash(opts *bind.CallOpts) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "getPkCHHash")
	return *ret0, err
}

// GetPkCHHash is a free data retrieval call binding the contract method 0xcab43ea8.
//
// Solidity: function getPkCHHash() constant returns(bytes32)
func (_Contracts *ContractsSession) GetPkCHHash() ([32]byte, error) {
	return _Contracts.Contract.GetPkCHHash(&_Contracts.CallOpts)
}

// GetPkCHHash is a free data retrieval call binding the contract method 0xcab43ea8.
//
// Solidity: function getPkCHHash() constant returns(bytes32)
func (_Contracts *ContractsCallerSession) GetPkCHHash() ([32]byte, error) {
	return _Contracts.Contract.GetPkCHHash(&_Contracts.CallOpts)
}

// GetRedactRequest is a free data retrieval call binding the contract method 0x4c6e9a0b.
//
// Solidity: function getRedactRequest(bytes32 reqID) constant returns(bytes32 storedReqID, uint256 epoch, bytes32 dReq, bytes32 dPolicy, uint8 status, uint256 authorizedAt, uint256 executedAt, uint256 expiresAt, bytes32 rejectReasonHash)
func (_Contracts *ContractsCaller) GetRedactRequest(opts *bind.CallOpts, reqID [32]byte) (struct {
	StoredReqID      [32]byte
	Epoch            *big.Int
	DReq             [32]byte
	DPolicy          [32]byte
	Status           uint8
	AuthorizedAt     *big.Int
	ExecutedAt       *big.Int
	ExpiresAt        *big.Int
	RejectReasonHash [32]byte
}, error) {
	ret := new(struct {
		StoredReqID      [32]byte
		Epoch            *big.Int
		DReq             [32]byte
		DPolicy          [32]byte
		Status           uint8
		AuthorizedAt     *big.Int
		ExecutedAt       *big.Int
		ExpiresAt        *big.Int
		RejectReasonHash [32]byte
	})
	out := ret
	err := _Contracts.contract.Call(opts, out, "getRedactRequest", reqID)
	return *ret, err
}

// GetRedactRequest is a free data retrieval call binding the contract method 0x4c6e9a0b.
//
// Solidity: function getRedactRequest(bytes32 reqID) constant returns(bytes32 storedReqID, uint256 epoch, bytes32 dReq, bytes32 dPolicy, uint8 status, uint256 authorizedAt, uint256 executedAt, uint256 expiresAt, bytes32 rejectReasonHash)
func (_Contracts *ContractsSession) GetRedactRequest(reqID [32]byte) (struct {
	StoredReqID      [32]byte
	Epoch            *big.Int
	DReq             [32]byte
	DPolicy          [32]byte
	Status           uint8
	AuthorizedAt     *big.Int
	ExecutedAt       *big.Int
	ExpiresAt        *big.Int
	RejectReasonHash [32]byte
}, error) {
	return _Contracts.Contract.GetRedactRequest(&_Contracts.CallOpts, reqID)
}

// GetRedactRequest is a free data retrieval call binding the contract method 0x4c6e9a0b.
//
// Solidity: function getRedactRequest(bytes32 reqID) constant returns(bytes32 storedReqID, uint256 epoch, bytes32 dReq, bytes32 dPolicy, uint8 status, uint256 authorizedAt, uint256 executedAt, uint256 expiresAt, bytes32 rejectReasonHash)
func (_Contracts *ContractsCallerSession) GetRedactRequest(reqID [32]byte) (struct {
	StoredReqID      [32]byte
	Epoch            *big.Int
	DReq             [32]byte
	DPolicy          [32]byte
	Status           uint8
	AuthorizedAt     *big.Int
	ExecutedAt       *big.Int
	ExpiresAt        *big.Int
	RejectReasonHash [32]byte
}, error) {
	return _Contracts.Contract.GetRedactRequest(&_Contracts.CallOpts, reqID)
}

// GetRegistrationChallenge is a free data retrieval call binding the contract method 0x38279581.
//
// Solidity: function getRegistrationChallenge(address _owner) constant returns(bytes32)
func (_Contracts *ContractsCaller) GetRegistrationChallenge(opts *bind.CallOpts, _owner common.Address) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "getRegistrationChallenge", _owner)
	return *ret0, err
}

// GetRegistrationChallenge is a free data retrieval call binding the contract method 0x38279581.
//
// Solidity: function getRegistrationChallenge(address _owner) constant returns(bytes32)
func (_Contracts *ContractsSession) GetRegistrationChallenge(_owner common.Address) ([32]byte, error) {
	return _Contracts.Contract.GetRegistrationChallenge(&_Contracts.CallOpts, _owner)
}

// GetRegistrationChallenge is a free data retrieval call binding the contract method 0x38279581.
//
// Solidity: function getRegistrationChallenge(address _owner) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) GetRegistrationChallenge(_owner common.Address) ([32]byte, error) {
	return _Contracts.Contract.GetRegistrationChallenge(&_Contracts.CallOpts, _owner)
}

// GetSystemParams is a free data retrieval call binding the contract method 0x40380348.
//
// Solidity: function getSystemParams() constant returns(uint256 committeeVersion, bytes32 pkCHHash, uint256 currentPkCHVersion, uint256 configuredEpochLength, uint256 epoch, uint256 activeThreshold, uint256 activeLimit)
func (_Contracts *ContractsCaller) GetSystemParams(opts *bind.CallOpts) (struct {
	CommitteeVersion      *big.Int
	PkCHHash              [32]byte
	CurrentPkCHVersion    *big.Int
	ConfiguredEpochLength *big.Int
	Epoch                 *big.Int
	ActiveThreshold       *big.Int
	ActiveLimit           *big.Int
}, error) {
	ret := new(struct {
		CommitteeVersion      *big.Int
		PkCHHash              [32]byte
		CurrentPkCHVersion    *big.Int
		ConfiguredEpochLength *big.Int
		Epoch                 *big.Int
		ActiveThreshold       *big.Int
		ActiveLimit           *big.Int
	})
	out := ret
	err := _Contracts.contract.Call(opts, out, "getSystemParams")
	return *ret, err
}

// GetSystemParams is a free data retrieval call binding the contract method 0x40380348.
//
// Solidity: function getSystemParams() constant returns(uint256 committeeVersion, bytes32 pkCHHash, uint256 currentPkCHVersion, uint256 configuredEpochLength, uint256 epoch, uint256 activeThreshold, uint256 activeLimit)
func (_Contracts *ContractsSession) GetSystemParams() (struct {
	CommitteeVersion      *big.Int
	PkCHHash              [32]byte
	CurrentPkCHVersion    *big.Int
	ConfiguredEpochLength *big.Int
	Epoch                 *big.Int
	ActiveThreshold       *big.Int
	ActiveLimit           *big.Int
}, error) {
	return _Contracts.Contract.GetSystemParams(&_Contracts.CallOpts)
}

// GetSystemParams is a free data retrieval call binding the contract method 0x40380348.
//
// Solidity: function getSystemParams() constant returns(uint256 committeeVersion, bytes32 pkCHHash, uint256 currentPkCHVersion, uint256 configuredEpochLength, uint256 epoch, uint256 activeThreshold, uint256 activeLimit)
func (_Contracts *ContractsCallerSession) GetSystemParams() (struct {
	CommitteeVersion      *big.Int
	PkCHHash              [32]byte
	CurrentPkCHVersion    *big.Int
	ConfiguredEpochLength *big.Int
	Epoch                 *big.Int
	ActiveThreshold       *big.Int
	ActiveLimit           *big.Int
}, error) {
	return _Contracts.Contract.GetSystemParams(&_Contracts.CallOpts)
}

// GetTEEEnclavePK is a free data retrieval call binding the contract method 0xf9dd7ebb.
//
// Solidity: function getTEEEnclavePK(bytes32 sigPubKey) constant returns(bytes32[2])
func (_Contracts *ContractsCaller) GetTEEEnclavePK(opts *bind.CallOpts, sigPubKey [32]byte) ([2][32]byte, error) {
	var (
		ret0 = new([2][32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "getTEEEnclavePK", sigPubKey)
	return *ret0, err
}

// GetTEEEnclavePK is a free data retrieval call binding the contract method 0xf9dd7ebb.
//
// Solidity: function getTEEEnclavePK(bytes32 sigPubKey) constant returns(bytes32[2])
func (_Contracts *ContractsSession) GetTEEEnclavePK(sigPubKey [32]byte) ([2][32]byte, error) {
	return _Contracts.Contract.GetTEEEnclavePK(&_Contracts.CallOpts, sigPubKey)
}

// GetTEEEnclavePK is a free data retrieval call binding the contract method 0xf9dd7ebb.
//
// Solidity: function getTEEEnclavePK(bytes32 sigPubKey) constant returns(bytes32[2])
func (_Contracts *ContractsCallerSession) GetTEEEnclavePK(sigPubKey [32]byte) ([2][32]byte, error) {
	return _Contracts.Contract.GetTEEEnclavePK(&_Contracts.CallOpts, sigPubKey)
}

// GetTEEExitInfo is a free data retrieval call binding the contract method 0xec56dbb0.
//
// Solidity: function getTEEExitInfo(bytes32 sigPubKey) constant returns(bool pending, address teeOwner, uint256 exitEpoch, uint256 eraseProofDeadlineBlock, bytes32 eraseProofHash, uint256 finalizedAt)
func (_Contracts *ContractsCaller) GetTEEExitInfo(opts *bind.CallOpts, sigPubKey [32]byte) (struct {
	Pending                 bool
	TeeOwner                common.Address
	ExitEpoch               *big.Int
	EraseProofDeadlineBlock *big.Int
	EraseProofHash          [32]byte
	FinalizedAt             *big.Int
}, error) {
	ret := new(struct {
		Pending                 bool
		TeeOwner                common.Address
		ExitEpoch               *big.Int
		EraseProofDeadlineBlock *big.Int
		EraseProofHash          [32]byte
		FinalizedAt             *big.Int
	})
	out := ret
	err := _Contracts.contract.Call(opts, out, "getTEEExitInfo", sigPubKey)
	return *ret, err
}

// GetTEEExitInfo is a free data retrieval call binding the contract method 0xec56dbb0.
//
// Solidity: function getTEEExitInfo(bytes32 sigPubKey) constant returns(bool pending, address teeOwner, uint256 exitEpoch, uint256 eraseProofDeadlineBlock, bytes32 eraseProofHash, uint256 finalizedAt)
func (_Contracts *ContractsSession) GetTEEExitInfo(sigPubKey [32]byte) (struct {
	Pending                 bool
	TeeOwner                common.Address
	ExitEpoch               *big.Int
	EraseProofDeadlineBlock *big.Int
	EraseProofHash          [32]byte
	FinalizedAt             *big.Int
}, error) {
	return _Contracts.Contract.GetTEEExitInfo(&_Contracts.CallOpts, sigPubKey)
}

// GetTEEExitInfo is a free data retrieval call binding the contract method 0xec56dbb0.
//
// Solidity: function getTEEExitInfo(bytes32 sigPubKey) constant returns(bool pending, address teeOwner, uint256 exitEpoch, uint256 eraseProofDeadlineBlock, bytes32 eraseProofHash, uint256 finalizedAt)
func (_Contracts *ContractsCallerSession) GetTEEExitInfo(sigPubKey [32]byte) (struct {
	Pending                 bool
	TeeOwner                common.Address
	ExitEpoch               *big.Int
	EraseProofDeadlineBlock *big.Int
	EraseProofHash          [32]byte
	FinalizedAt             *big.Int
}, error) {
	return _Contracts.Contract.GetTEEExitInfo(&_Contracts.CallOpts, sigPubKey)
}

// GetTEEInfo is a free data retrieval call binding the contract method 0xf565ea5f.
//
// Solidity: function getTEEInfo(bytes32 sigPubKey) constant returns(bytes32, bytes32, address, uint256, bool)
func (_Contracts *ContractsCaller) GetTEEInfo(opts *bind.CallOpts, sigPubKey [32]byte) ([32]byte, [32]byte, common.Address, *big.Int, bool, error) {
	var (
		ret0 = new([32]byte)
		ret1 = new([32]byte)
		ret2 = new(common.Address)
		ret3 = new(*big.Int)
		ret4 = new(bool)
	)
	out := &[]interface{}{
		ret0,
		ret1,
		ret2,
		ret3,
		ret4,
	}
	err := _Contracts.contract.Call(opts, out, "getTEEInfo", sigPubKey)
	return *ret0, *ret1, *ret2, *ret3, *ret4, err
}

// GetTEEInfo is a free data retrieval call binding the contract method 0xf565ea5f.
//
// Solidity: function getTEEInfo(bytes32 sigPubKey) constant returns(bytes32, bytes32, address, uint256, bool)
func (_Contracts *ContractsSession) GetTEEInfo(sigPubKey [32]byte) ([32]byte, [32]byte, common.Address, *big.Int, bool, error) {
	return _Contracts.Contract.GetTEEInfo(&_Contracts.CallOpts, sigPubKey)
}

// GetTEEInfo is a free data retrieval call binding the contract method 0xf565ea5f.
//
// Solidity: function getTEEInfo(bytes32 sigPubKey) constant returns(bytes32, bytes32, address, uint256, bool)
func (_Contracts *ContractsCallerSession) GetTEEInfo(sigPubKey [32]byte) ([32]byte, [32]byte, common.Address, *big.Int, bool, error) {
	return _Contracts.Contract.GetTEEInfo(&_Contracts.CallOpts, sigPubKey)
}

// GetTEESelectionVRF is a free data retrieval call binding the contract method 0xc2dbb46f.
//
// Solidity: function getTEESelectionVRF(uint256 epoch, bytes32 sigPubKey) constant returns(bool exists, bytes32 vrfResult, bytes32 proofHash, uint256 submittedAtBlock, uint256 submittedAt)
func (_Contracts *ContractsCaller) GetTEESelectionVRF(opts *bind.CallOpts, epoch *big.Int, sigPubKey [32]byte) (struct {
	Exists           bool
	VrfResult        [32]byte
	ProofHash        [32]byte
	SubmittedAtBlock *big.Int
	SubmittedAt      *big.Int
}, error) {
	ret := new(struct {
		Exists           bool
		VrfResult        [32]byte
		ProofHash        [32]byte
		SubmittedAtBlock *big.Int
		SubmittedAt      *big.Int
	})
	out := ret
	err := _Contracts.contract.Call(opts, out, "getTEESelectionVRF", epoch, sigPubKey)
	return *ret, err
}

// GetTEESelectionVRF is a free data retrieval call binding the contract method 0xc2dbb46f.
//
// Solidity: function getTEESelectionVRF(uint256 epoch, bytes32 sigPubKey) constant returns(bool exists, bytes32 vrfResult, bytes32 proofHash, uint256 submittedAtBlock, uint256 submittedAt)
func (_Contracts *ContractsSession) GetTEESelectionVRF(epoch *big.Int, sigPubKey [32]byte) (struct {
	Exists           bool
	VrfResult        [32]byte
	ProofHash        [32]byte
	SubmittedAtBlock *big.Int
	SubmittedAt      *big.Int
}, error) {
	return _Contracts.Contract.GetTEESelectionVRF(&_Contracts.CallOpts, epoch, sigPubKey)
}

// GetTEESelectionVRF is a free data retrieval call binding the contract method 0xc2dbb46f.
//
// Solidity: function getTEESelectionVRF(uint256 epoch, bytes32 sigPubKey) constant returns(bool exists, bytes32 vrfResult, bytes32 proofHash, uint256 submittedAtBlock, uint256 submittedAt)
func (_Contracts *ContractsCallerSession) GetTEESelectionVRF(epoch *big.Int, sigPubKey [32]byte) (struct {
	Exists           bool
	VrfResult        [32]byte
	ProofHash        [32]byte
	SubmittedAtBlock *big.Int
	SubmittedAt      *big.Int
}, error) {
	return _Contracts.Contract.GetTEESelectionVRF(&_Contracts.CallOpts, epoch, sigPubKey)
}

// GetTEEStatusByOwner is a free data retrieval call binding the contract method 0x1d3e33f2.
//
// Solidity: function getTEEStatusByOwner(address _owner) constant returns(uint8, bytes32)
func (_Contracts *ContractsCaller) GetTEEStatusByOwner(opts *bind.CallOpts, _owner common.Address) (uint8, [32]byte, error) {
	var (
		ret0 = new(uint8)
		ret1 = new([32]byte)
	)
	out := &[]interface{}{
		ret0,
		ret1,
	}
	err := _Contracts.contract.Call(opts, out, "getTEEStatusByOwner", _owner)
	return *ret0, *ret1, err
}

// GetTEEStatusByOwner is a free data retrieval call binding the contract method 0x1d3e33f2.
//
// Solidity: function getTEEStatusByOwner(address _owner) constant returns(uint8, bytes32)
func (_Contracts *ContractsSession) GetTEEStatusByOwner(_owner common.Address) (uint8, [32]byte, error) {
	return _Contracts.Contract.GetTEEStatusByOwner(&_Contracts.CallOpts, _owner)
}

// GetTEEStatusByOwner is a free data retrieval call binding the contract method 0x1d3e33f2.
//
// Solidity: function getTEEStatusByOwner(address _owner) constant returns(uint8, bytes32)
func (_Contracts *ContractsCallerSession) GetTEEStatusByOwner(_owner common.Address) (uint8, [32]byte, error) {
	return _Contracts.Contract.GetTEEStatusByOwner(&_Contracts.CallOpts, _owner)
}

// IndexOfSigPubKey is a free data retrieval call binding the contract method 0xb27174b3.
//
// Solidity: function indexOfSigPubKey(bytes32 ) constant returns(uint256)
func (_Contracts *ContractsCaller) IndexOfSigPubKey(opts *bind.CallOpts, arg0 [32]byte) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "indexOfSigPubKey", arg0)
	return *ret0, err
}

// IndexOfSigPubKey is a free data retrieval call binding the contract method 0xb27174b3.
//
// Solidity: function indexOfSigPubKey(bytes32 ) constant returns(uint256)
func (_Contracts *ContractsSession) IndexOfSigPubKey(arg0 [32]byte) (*big.Int, error) {
	return _Contracts.Contract.IndexOfSigPubKey(&_Contracts.CallOpts, arg0)
}

// IndexOfSigPubKey is a free data retrieval call binding the contract method 0xb27174b3.
//
// Solidity: function indexOfSigPubKey(bytes32 ) constant returns(uint256)
func (_Contracts *ContractsCallerSession) IndexOfSigPubKey(arg0 [32]byte) (*big.Int, error) {
	return _Contracts.Contract.IndexOfSigPubKey(&_Contracts.CallOpts, arg0)
}

// IsEraseProofOverdue is a free data retrieval call binding the contract method 0x674d9ec4.
//
// Solidity: function isEraseProofOverdue(bytes32 sigPubKey) constant returns(bool)
func (_Contracts *ContractsCaller) IsEraseProofOverdue(opts *bind.CallOpts, sigPubKey [32]byte) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "isEraseProofOverdue", sigPubKey)
	return *ret0, err
}

// IsEraseProofOverdue is a free data retrieval call binding the contract method 0x674d9ec4.
//
// Solidity: function isEraseProofOverdue(bytes32 sigPubKey) constant returns(bool)
func (_Contracts *ContractsSession) IsEraseProofOverdue(sigPubKey [32]byte) (bool, error) {
	return _Contracts.Contract.IsEraseProofOverdue(&_Contracts.CallOpts, sigPubKey)
}

// IsEraseProofOverdue is a free data retrieval call binding the contract method 0x674d9ec4.
//
// Solidity: function isEraseProofOverdue(bytes32 sigPubKey) constant returns(bool)
func (_Contracts *ContractsCallerSession) IsEraseProofOverdue(sigPubKey [32]byte) (bool, error) {
	return _Contracts.Contract.IsEraseProofOverdue(&_Contracts.CallOpts, sigPubKey)
}

// IsHardwareIdRegistered is a free data retrieval call binding the contract method 0x05e9719a.
//
// Solidity: function isHardwareIdRegistered(bytes32 hardwareId) constant returns(bool)
func (_Contracts *ContractsCaller) IsHardwareIdRegistered(opts *bind.CallOpts, hardwareId [32]byte) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "isHardwareIdRegistered", hardwareId)
	return *ret0, err
}

// IsHardwareIdRegistered is a free data retrieval call binding the contract method 0x05e9719a.
//
// Solidity: function isHardwareIdRegistered(bytes32 hardwareId) constant returns(bool)
func (_Contracts *ContractsSession) IsHardwareIdRegistered(hardwareId [32]byte) (bool, error) {
	return _Contracts.Contract.IsHardwareIdRegistered(&_Contracts.CallOpts, hardwareId)
}

// IsHardwareIdRegistered is a free data retrieval call binding the contract method 0x05e9719a.
//
// Solidity: function isHardwareIdRegistered(bytes32 hardwareId) constant returns(bool)
func (_Contracts *ContractsCallerSession) IsHardwareIdRegistered(hardwareId [32]byte) (bool, error) {
	return _Contracts.Contract.IsHardwareIdRegistered(&_Contracts.CallOpts, hardwareId)
}

// IsRedactionExecuted is a free data retrieval call binding the contract method 0xd4b15462.
//
// Solidity: function isRedactionExecuted(bytes32 reqID) constant returns(bool)
func (_Contracts *ContractsCaller) IsRedactionExecuted(opts *bind.CallOpts, reqID [32]byte) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "isRedactionExecuted", reqID)
	return *ret0, err
}

// IsRedactionExecuted is a free data retrieval call binding the contract method 0xd4b15462.
//
// Solidity: function isRedactionExecuted(bytes32 reqID) constant returns(bool)
func (_Contracts *ContractsSession) IsRedactionExecuted(reqID [32]byte) (bool, error) {
	return _Contracts.Contract.IsRedactionExecuted(&_Contracts.CallOpts, reqID)
}

// IsRedactionExecuted is a free data retrieval call binding the contract method 0xd4b15462.
//
// Solidity: function isRedactionExecuted(bytes32 reqID) constant returns(bool)
func (_Contracts *ContractsCallerSession) IsRedactionExecuted(reqID [32]byte) (bool, error) {
	return _Contracts.Contract.IsRedactionExecuted(&_Contracts.CallOpts, reqID)
}

// IsRotationPreparationPhase is a free data retrieval call binding the contract method 0x53dc03dd.
//
// Solidity: function isRotationPreparationPhase() constant returns(bool)
func (_Contracts *ContractsCaller) IsRotationPreparationPhase(opts *bind.CallOpts) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "isRotationPreparationPhase")
	return *ret0, err
}

// IsRotationPreparationPhase is a free data retrieval call binding the contract method 0x53dc03dd.
//
// Solidity: function isRotationPreparationPhase() constant returns(bool)
func (_Contracts *ContractsSession) IsRotationPreparationPhase() (bool, error) {
	return _Contracts.Contract.IsRotationPreparationPhase(&_Contracts.CallOpts)
}

// IsRotationPreparationPhase is a free data retrieval call binding the contract method 0x53dc03dd.
//
// Solidity: function isRotationPreparationPhase() constant returns(bool)
func (_Contracts *ContractsCallerSession) IsRotationPreparationPhase() (bool, error) {
	return _Contracts.Contract.IsRotationPreparationPhase(&_Contracts.CallOpts)
}

// IsSigPubKeyRegistered is a free data retrieval call binding the contract method 0xfef06743.
//
// Solidity: function isSigPubKeyRegistered(bytes32 ) constant returns(bool)
func (_Contracts *ContractsCaller) IsSigPubKeyRegistered(opts *bind.CallOpts, arg0 [32]byte) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "isSigPubKeyRegistered", arg0)
	return *ret0, err
}

// IsSigPubKeyRegistered is a free data retrieval call binding the contract method 0xfef06743.
//
// Solidity: function isSigPubKeyRegistered(bytes32 ) constant returns(bool)
func (_Contracts *ContractsSession) IsSigPubKeyRegistered(arg0 [32]byte) (bool, error) {
	return _Contracts.Contract.IsSigPubKeyRegistered(&_Contracts.CallOpts, arg0)
}

// IsSigPubKeyRegistered is a free data retrieval call binding the contract method 0xfef06743.
//
// Solidity: function isSigPubKeyRegistered(bytes32 ) constant returns(bool)
func (_Contracts *ContractsCallerSession) IsSigPubKeyRegistered(arg0 [32]byte) (bool, error) {
	return _Contracts.Contract.IsSigPubKeyRegistered(&_Contracts.CallOpts, arg0)
}

// IsSystemReady is a free data retrieval call binding the contract method 0x285ebd4c.
//
// Solidity: function isSystemReady() constant returns(bool)
func (_Contracts *ContractsCaller) IsSystemReady(opts *bind.CallOpts) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "isSystemReady")
	return *ret0, err
}

// IsSystemReady is a free data retrieval call binding the contract method 0x285ebd4c.
//
// Solidity: function isSystemReady() constant returns(bool)
func (_Contracts *ContractsSession) IsSystemReady() (bool, error) {
	return _Contracts.Contract.IsSystemReady(&_Contracts.CallOpts)
}

// IsSystemReady is a free data retrieval call binding the contract method 0x285ebd4c.
//
// Solidity: function isSystemReady() constant returns(bool)
func (_Contracts *ContractsCallerSession) IsSystemReady() (bool, error) {
	return _Contracts.Contract.IsSystemReady(&_Contracts.CallOpts)
}

// LastRotationBlock is a free data retrieval call binding the contract method 0x2e9c3bb8.
//
// Solidity: function lastRotationBlock() constant returns(uint256)
func (_Contracts *ContractsCaller) LastRotationBlock(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "lastRotationBlock")
	return *ret0, err
}

// LastRotationBlock is a free data retrieval call binding the contract method 0x2e9c3bb8.
//
// Solidity: function lastRotationBlock() constant returns(uint256)
func (_Contracts *ContractsSession) LastRotationBlock() (*big.Int, error) {
	return _Contracts.Contract.LastRotationBlock(&_Contracts.CallOpts)
}

// LastRotationBlock is a free data retrieval call binding the contract method 0x2e9c3bb8.
//
// Solidity: function lastRotationBlock() constant returns(uint256)
func (_Contracts *ContractsCallerSession) LastRotationBlock() (*big.Int, error) {
	return _Contracts.Contract.LastRotationBlock(&_Contracts.CallOpts)
}

// LastRotationTimestamp is a free data retrieval call binding the contract method 0xeb215f28.
//
// Solidity: function lastRotationTimestamp() constant returns(uint256)
func (_Contracts *ContractsCaller) LastRotationTimestamp(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "lastRotationTimestamp")
	return *ret0, err
}

// LastRotationTimestamp is a free data retrieval call binding the contract method 0xeb215f28.
//
// Solidity: function lastRotationTimestamp() constant returns(uint256)
func (_Contracts *ContractsSession) LastRotationTimestamp() (*big.Int, error) {
	return _Contracts.Contract.LastRotationTimestamp(&_Contracts.CallOpts)
}

// LastRotationTimestamp is a free data retrieval call binding the contract method 0xeb215f28.
//
// Solidity: function lastRotationTimestamp() constant returns(uint256)
func (_Contracts *ContractsCallerSession) LastRotationTimestamp() (*big.Int, error) {
	return _Contracts.Contract.LastRotationTimestamp(&_Contracts.CallOpts)
}

// NT is a free data retrieval call binding the contract method 0x41d7aa28.
//
// Solidity: function nT() constant returns(uint256)
func (_Contracts *ContractsCaller) NT(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "nT")
	return *ret0, err
}

// NT is a free data retrieval call binding the contract method 0x41d7aa28.
//
// Solidity: function nT() constant returns(uint256)
func (_Contracts *ContractsSession) NT() (*big.Int, error) {
	return _Contracts.Contract.NT(&_Contracts.CallOpts)
}

// NT is a free data retrieval call binding the contract method 0x41d7aa28.
//
// Solidity: function nT() constant returns(uint256)
func (_Contracts *ContractsCallerSession) NT() (*big.Int, error) {
	return _Contracts.Contract.NT(&_Contracts.CallOpts)
}

// NextPendingNonceOfOwner is a free data retrieval call binding the contract method 0xd1b6fddb.
//
// Solidity: function nextPendingNonceOfOwner(address ) constant returns(uint256)
func (_Contracts *ContractsCaller) NextPendingNonceOfOwner(opts *bind.CallOpts, arg0 common.Address) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "nextPendingNonceOfOwner", arg0)
	return *ret0, err
}

// NextPendingNonceOfOwner is a free data retrieval call binding the contract method 0xd1b6fddb.
//
// Solidity: function nextPendingNonceOfOwner(address ) constant returns(uint256)
func (_Contracts *ContractsSession) NextPendingNonceOfOwner(arg0 common.Address) (*big.Int, error) {
	return _Contracts.Contract.NextPendingNonceOfOwner(&_Contracts.CallOpts, arg0)
}

// NextPendingNonceOfOwner is a free data retrieval call binding the contract method 0xd1b6fddb.
//
// Solidity: function nextPendingNonceOfOwner(address ) constant returns(uint256)
func (_Contracts *ContractsCallerSession) NextPendingNonceOfOwner(arg0 common.Address) (*big.Int, error) {
	return _Contracts.Contract.NextPendingNonceOfOwner(&_Contracts.CallOpts, arg0)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() constant returns(address)
func (_Contracts *ContractsCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "owner")
	return *ret0, err
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() constant returns(address)
func (_Contracts *ContractsSession) Owner() (common.Address, error) {
	return _Contracts.Contract.Owner(&_Contracts.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() constant returns(address)
func (_Contracts *ContractsCallerSession) Owner() (common.Address, error) {
	return _Contracts.Contract.Owner(&_Contracts.CallOpts)
}

// PendingTEEOfOwner is a free data retrieval call binding the contract method 0x2118700d.
//
// Solidity: function pendingTEEOfOwner(address ) constant returns(uint256 deposit, uint256 nonce, bytes32 challengeHash)
func (_Contracts *ContractsCaller) PendingTEEOfOwner(opts *bind.CallOpts, arg0 common.Address) (struct {
	Deposit       *big.Int
	Nonce         *big.Int
	ChallengeHash [32]byte
}, error) {
	ret := new(struct {
		Deposit       *big.Int
		Nonce         *big.Int
		ChallengeHash [32]byte
	})
	out := ret
	err := _Contracts.contract.Call(opts, out, "pendingTEEOfOwner", arg0)
	return *ret, err
}

// PendingTEEOfOwner is a free data retrieval call binding the contract method 0x2118700d.
//
// Solidity: function pendingTEEOfOwner(address ) constant returns(uint256 deposit, uint256 nonce, bytes32 challengeHash)
func (_Contracts *ContractsSession) PendingTEEOfOwner(arg0 common.Address) (struct {
	Deposit       *big.Int
	Nonce         *big.Int
	ChallengeHash [32]byte
}, error) {
	return _Contracts.Contract.PendingTEEOfOwner(&_Contracts.CallOpts, arg0)
}

// PendingTEEOfOwner is a free data retrieval call binding the contract method 0x2118700d.
//
// Solidity: function pendingTEEOfOwner(address ) constant returns(uint256 deposit, uint256 nonce, bytes32 challengeHash)
func (_Contracts *ContractsCallerSession) PendingTEEOfOwner(arg0 common.Address) (struct {
	Deposit       *big.Int
	Nonce         *big.Int
	ChallengeHash [32]byte
}, error) {
	return _Contracts.Contract.PendingTEEOfOwner(&_Contracts.CallOpts, arg0)
}

// PkCH is a free data retrieval call binding the contract method 0x8c4aad51.
//
// Solidity: function pkCH() constant returns(bytes)
func (_Contracts *ContractsCaller) PkCH(opts *bind.CallOpts) ([]byte, error) {
	var (
		ret0 = new([]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "pkCH")
	return *ret0, err
}

// PkCH is a free data retrieval call binding the contract method 0x8c4aad51.
//
// Solidity: function pkCH() constant returns(bytes)
func (_Contracts *ContractsSession) PkCH() ([]byte, error) {
	return _Contracts.Contract.PkCH(&_Contracts.CallOpts)
}

// PkCH is a free data retrieval call binding the contract method 0x8c4aad51.
//
// Solidity: function pkCH() constant returns(bytes)
func (_Contracts *ContractsCallerSession) PkCH() ([]byte, error) {
	return _Contracts.Contract.PkCH(&_Contracts.CallOpts)
}

// PkCHVersion is a free data retrieval call binding the contract method 0x7c48a1d9.
//
// Solidity: function pkCHVersion() constant returns(uint256)
func (_Contracts *ContractsCaller) PkCHVersion(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "pkCHVersion")
	return *ret0, err
}

// PkCHVersion is a free data retrieval call binding the contract method 0x7c48a1d9.
//
// Solidity: function pkCHVersion() constant returns(uint256)
func (_Contracts *ContractsSession) PkCHVersion() (*big.Int, error) {
	return _Contracts.Contract.PkCHVersion(&_Contracts.CallOpts)
}

// PkCHVersion is a free data retrieval call binding the contract method 0x7c48a1d9.
//
// Solidity: function pkCHVersion() constant returns(uint256)
func (_Contracts *ContractsCallerSession) PkCHVersion() (*big.Int, error) {
	return _Contracts.Contract.PkCHVersion(&_Contracts.CallOpts)
}

// RotationDeadlineBlock is a free data retrieval call binding the contract method 0x083e1110.
//
// Solidity: function rotationDeadlineBlock() constant returns(uint256)
func (_Contracts *ContractsCaller) RotationDeadlineBlock(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "rotationDeadlineBlock")
	return *ret0, err
}

// RotationDeadlineBlock is a free data retrieval call binding the contract method 0x083e1110.
//
// Solidity: function rotationDeadlineBlock() constant returns(uint256)
func (_Contracts *ContractsSession) RotationDeadlineBlock() (*big.Int, error) {
	return _Contracts.Contract.RotationDeadlineBlock(&_Contracts.CallOpts)
}

// RotationDeadlineBlock is a free data retrieval call binding the contract method 0x083e1110.
//
// Solidity: function rotationDeadlineBlock() constant returns(uint256)
func (_Contracts *ContractsCallerSession) RotationDeadlineBlock() (*big.Int, error) {
	return _Contracts.Contract.RotationDeadlineBlock(&_Contracts.CallOpts)
}

// RotationPreparationRequested is a free data retrieval call binding the contract method 0x3d700f28.
//
// Solidity: function rotationPreparationRequested(uint256 ) constant returns(bool)
func (_Contracts *ContractsCaller) RotationPreparationRequested(opts *bind.CallOpts, arg0 *big.Int) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "rotationPreparationRequested", arg0)
	return *ret0, err
}

// RotationPreparationRequested is a free data retrieval call binding the contract method 0x3d700f28.
//
// Solidity: function rotationPreparationRequested(uint256 ) constant returns(bool)
func (_Contracts *ContractsSession) RotationPreparationRequested(arg0 *big.Int) (bool, error) {
	return _Contracts.Contract.RotationPreparationRequested(&_Contracts.CallOpts, arg0)
}

// RotationPreparationRequested is a free data retrieval call binding the contract method 0x3d700f28.
//
// Solidity: function rotationPreparationRequested(uint256 ) constant returns(bool)
func (_Contracts *ContractsCallerSession) RotationPreparationRequested(arg0 *big.Int) (bool, error) {
	return _Contracts.Contract.RotationPreparationRequested(&_Contracts.CallOpts, arg0)
}

// SeedByEpoch is a free data retrieval call binding the contract method 0x47336891.
//
// Solidity: function seedByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsCaller) SeedByEpoch(opts *bind.CallOpts, arg0 *big.Int) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "seedByEpoch", arg0)
	return *ret0, err
}

// SeedByEpoch is a free data retrieval call binding the contract method 0x47336891.
//
// Solidity: function seedByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsSession) SeedByEpoch(arg0 *big.Int) ([32]byte, error) {
	return _Contracts.Contract.SeedByEpoch(&_Contracts.CallOpts, arg0)
}

// SeedByEpoch is a free data retrieval call binding the contract method 0x47336891.
//
// Solidity: function seedByEpoch(uint256 ) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) SeedByEpoch(arg0 *big.Int) ([32]byte, error) {
	return _Contracts.Contract.SeedByEpoch(&_Contracts.CallOpts, arg0)
}

// SigPubKeyOfOwner is a free data retrieval call binding the contract method 0x7cc27d57.
//
// Solidity: function sigPubKeyOfOwner(address ) constant returns(bytes32)
func (_Contracts *ContractsCaller) SigPubKeyOfOwner(opts *bind.CallOpts, arg0 common.Address) ([32]byte, error) {
	var (
		ret0 = new([32]byte)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "sigPubKeyOfOwner", arg0)
	return *ret0, err
}

// SigPubKeyOfOwner is a free data retrieval call binding the contract method 0x7cc27d57.
//
// Solidity: function sigPubKeyOfOwner(address ) constant returns(bytes32)
func (_Contracts *ContractsSession) SigPubKeyOfOwner(arg0 common.Address) ([32]byte, error) {
	return _Contracts.Contract.SigPubKeyOfOwner(&_Contracts.CallOpts, arg0)
}

// SigPubKeyOfOwner is a free data retrieval call binding the contract method 0x7cc27d57.
//
// Solidity: function sigPubKeyOfOwner(address ) constant returns(bytes32)
func (_Contracts *ContractsCallerSession) SigPubKeyOfOwner(arg0 common.Address) ([32]byte, error) {
	return _Contracts.Contract.SigPubKeyOfOwner(&_Contracts.CallOpts, arg0)
}

// SteadyEpochLength is a free data retrieval call binding the contract method 0x0a3ab49e.
//
// Solidity: function steadyEpochLength() constant returns(uint256)
func (_Contracts *ContractsCaller) SteadyEpochLength(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "steadyEpochLength")
	return *ret0, err
}

// SteadyEpochLength is a free data retrieval call binding the contract method 0x0a3ab49e.
//
// Solidity: function steadyEpochLength() constant returns(uint256)
func (_Contracts *ContractsSession) SteadyEpochLength() (*big.Int, error) {
	return _Contracts.Contract.SteadyEpochLength(&_Contracts.CallOpts)
}

// SteadyEpochLength is a free data retrieval call binding the contract method 0x0a3ab49e.
//
// Solidity: function steadyEpochLength() constant returns(uint256)
func (_Contracts *ContractsCallerSession) SteadyEpochLength() (*big.Int, error) {
	return _Contracts.Contract.SteadyEpochLength(&_Contracts.CallOpts)
}

// TT is a free data retrieval call binding the contract method 0x3e3328b4.
//
// Solidity: function tT() constant returns(uint256)
func (_Contracts *ContractsCaller) TT(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "tT")
	return *ret0, err
}

// TT is a free data retrieval call binding the contract method 0x3e3328b4.
//
// Solidity: function tT() constant returns(uint256)
func (_Contracts *ContractsSession) TT() (*big.Int, error) {
	return _Contracts.Contract.TT(&_Contracts.CallOpts)
}

// TT is a free data retrieval call binding the contract method 0x3e3328b4.
//
// Solidity: function tT() constant returns(uint256)
func (_Contracts *ContractsCallerSession) TT() (*big.Int, error) {
	return _Contracts.Contract.TT(&_Contracts.CallOpts)
}

// TrustedMREnclave is a free data retrieval call binding the contract method 0xe728ef42.
//
// Solidity: function trustedMREnclave(bytes32 ) constant returns(bool)
func (_Contracts *ContractsCaller) TrustedMREnclave(opts *bind.CallOpts, arg0 [32]byte) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "trustedMREnclave", arg0)
	return *ret0, err
}

// TrustedMREnclave is a free data retrieval call binding the contract method 0xe728ef42.
//
// Solidity: function trustedMREnclave(bytes32 ) constant returns(bool)
func (_Contracts *ContractsSession) TrustedMREnclave(arg0 [32]byte) (bool, error) {
	return _Contracts.Contract.TrustedMREnclave(&_Contracts.CallOpts, arg0)
}

// TrustedMREnclave is a free data retrieval call binding the contract method 0xe728ef42.
//
// Solidity: function trustedMREnclave(bytes32 ) constant returns(bool)
func (_Contracts *ContractsCallerSession) TrustedMREnclave(arg0 [32]byte) (bool, error) {
	return _Contracts.Contract.TrustedMREnclave(&_Contracts.CallOpts, arg0)
}

// TrustedMREnclaveCount is a free data retrieval call binding the contract method 0x8c6df622.
//
// Solidity: function trustedMREnclaveCount() constant returns(uint256)
func (_Contracts *ContractsCaller) TrustedMREnclaveCount(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Contracts.contract.Call(opts, out, "trustedMREnclaveCount")
	return *ret0, err
}

// TrustedMREnclaveCount is a free data retrieval call binding the contract method 0x8c6df622.
//
// Solidity: function trustedMREnclaveCount() constant returns(uint256)
func (_Contracts *ContractsSession) TrustedMREnclaveCount() (*big.Int, error) {
	return _Contracts.Contract.TrustedMREnclaveCount(&_Contracts.CallOpts)
}

// TrustedMREnclaveCount is a free data retrieval call binding the contract method 0x8c6df622.
//
// Solidity: function trustedMREnclaveCount() constant returns(uint256)
func (_Contracts *ContractsCallerSession) TrustedMREnclaveCount() (*big.Int, error) {
	return _Contracts.Contract.TrustedMREnclaveCount(&_Contracts.CallOpts)
}

// CancelPendingTEE is a paid mutator transaction binding the contract method 0x4ea60932.
//
// Solidity: function cancelPendingTEE() returns(bool)
func (_Contracts *ContractsTransactor) CancelPendingTEE(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "cancelPendingTEE")
}

// CancelPendingTEE is a paid mutator transaction binding the contract method 0x4ea60932.
//
// Solidity: function cancelPendingTEE() returns(bool)
func (_Contracts *ContractsSession) CancelPendingTEE() (*types.Transaction, error) {
	return _Contracts.Contract.CancelPendingTEE(&_Contracts.TransactOpts)
}

// CancelPendingTEE is a paid mutator transaction binding the contract method 0x4ea60932.
//
// Solidity: function cancelPendingTEE() returns(bool)
func (_Contracts *ContractsTransactorSession) CancelPendingTEE() (*types.Transaction, error) {
	return _Contracts.Contract.CancelPendingTEE(&_Contracts.TransactOpts)
}

// ExecuteRedaction is a paid mutator transaction binding the contract method 0xc82a63bc.
//
// Solidity: function executeRedaction(bytes32 reqID, bytes32 rPrimeHash, bytes32 proofHash, bytes32 resultHash, bytes piReqA) returns(bool)
func (_Contracts *ContractsTransactor) ExecuteRedaction(opts *bind.TransactOpts, reqID [32]byte, rPrimeHash [32]byte, proofHash [32]byte, resultHash [32]byte, piReqA []byte) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "executeRedaction", reqID, rPrimeHash, proofHash, resultHash, piReqA)
}

// ExecuteRedaction is a paid mutator transaction binding the contract method 0xc82a63bc.
//
// Solidity: function executeRedaction(bytes32 reqID, bytes32 rPrimeHash, bytes32 proofHash, bytes32 resultHash, bytes piReqA) returns(bool)
func (_Contracts *ContractsSession) ExecuteRedaction(reqID [32]byte, rPrimeHash [32]byte, proofHash [32]byte, resultHash [32]byte, piReqA []byte) (*types.Transaction, error) {
	return _Contracts.Contract.ExecuteRedaction(&_Contracts.TransactOpts, reqID, rPrimeHash, proofHash, resultHash, piReqA)
}

// ExecuteRedaction is a paid mutator transaction binding the contract method 0xc82a63bc.
//
// Solidity: function executeRedaction(bytes32 reqID, bytes32 rPrimeHash, bytes32 proofHash, bytes32 resultHash, bytes piReqA) returns(bool)
func (_Contracts *ContractsTransactorSession) ExecuteRedaction(reqID [32]byte, rPrimeHash [32]byte, proofHash [32]byte, resultHash [32]byte, piReqA []byte) (*types.Transaction, error) {
	return _Contracts.Contract.ExecuteRedaction(&_Contracts.TransactOpts, reqID, rPrimeHash, proofHash, resultHash, piReqA)
}

// PrepareRotation is a paid mutator transaction binding the contract method 0x093ebf74.
//
// Solidity: function prepareRotation() returns(bool)
func (_Contracts *ContractsTransactor) PrepareRotation(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "prepareRotation")
}

// PrepareRotation is a paid mutator transaction binding the contract method 0x093ebf74.
//
// Solidity: function prepareRotation() returns(bool)
func (_Contracts *ContractsSession) PrepareRotation() (*types.Transaction, error) {
	return _Contracts.Contract.PrepareRotation(&_Contracts.TransactOpts)
}

// PrepareRotation is a paid mutator transaction binding the contract method 0x093ebf74.
//
// Solidity: function prepareRotation() returns(bool)
func (_Contracts *ContractsTransactorSession) PrepareRotation() (*types.Transaction, error) {
	return _Contracts.Contract.PrepareRotation(&_Contracts.TransactOpts)
}

// RegisterCommitteePubKey is a paid mutator transaction binding the contract method 0xdf3e9516.
//
// Solidity: function registerCommitteePubKey(uint256[4] _committeePubKey) returns(bool)
func (_Contracts *ContractsTransactor) RegisterCommitteePubKey(opts *bind.TransactOpts, _committeePubKey [4]*big.Int) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "registerCommitteePubKey", _committeePubKey)
}

// RegisterCommitteePubKey is a paid mutator transaction binding the contract method 0xdf3e9516.
//
// Solidity: function registerCommitteePubKey(uint256[4] _committeePubKey) returns(bool)
func (_Contracts *ContractsSession) RegisterCommitteePubKey(_committeePubKey [4]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.RegisterCommitteePubKey(&_Contracts.TransactOpts, _committeePubKey)
}

// RegisterCommitteePubKey is a paid mutator transaction binding the contract method 0xdf3e9516.
//
// Solidity: function registerCommitteePubKey(uint256[4] _committeePubKey) returns(bool)
func (_Contracts *ContractsTransactorSession) RegisterCommitteePubKey(_committeePubKey [4]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.RegisterCommitteePubKey(&_Contracts.TransactOpts, _committeePubKey)
}

// RegisterTEE is a paid mutator transaction binding the contract method 0xe618c49e.
//
// Solidity: function registerTEE(address _owner, uint256 _pendingNonce, bytes32 _MR_ENCLAVE, bytes32 _HardwareId, PermissionManagerV3TEERegistrationEvidence evidence, uint256[2] _signature) returns(bool)
func (_Contracts *ContractsTransactor) RegisterTEE(opts *bind.TransactOpts, _owner common.Address, _pendingNonce *big.Int, _MR_ENCLAVE [32]byte, _HardwareId [32]byte, evidence PermissionManagerV3TEERegistrationEvidence, _signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "registerTEE", _owner, _pendingNonce, _MR_ENCLAVE, _HardwareId, evidence, _signature)
}

// RegisterTEE is a paid mutator transaction binding the contract method 0xe618c49e.
//
// Solidity: function registerTEE(address _owner, uint256 _pendingNonce, bytes32 _MR_ENCLAVE, bytes32 _HardwareId, PermissionManagerV3TEERegistrationEvidence evidence, uint256[2] _signature) returns(bool)
func (_Contracts *ContractsSession) RegisterTEE(_owner common.Address, _pendingNonce *big.Int, _MR_ENCLAVE [32]byte, _HardwareId [32]byte, evidence PermissionManagerV3TEERegistrationEvidence, _signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.RegisterTEE(&_Contracts.TransactOpts, _owner, _pendingNonce, _MR_ENCLAVE, _HardwareId, evidence, _signature)
}

// RegisterTEE is a paid mutator transaction binding the contract method 0xe618c49e.
//
// Solidity: function registerTEE(address _owner, uint256 _pendingNonce, bytes32 _MR_ENCLAVE, bytes32 _HardwareId, PermissionManagerV3TEERegistrationEvidence evidence, uint256[2] _signature) returns(bool)
func (_Contracts *ContractsTransactorSession) RegisterTEE(_owner common.Address, _pendingNonce *big.Int, _MR_ENCLAVE [32]byte, _HardwareId [32]byte, evidence PermissionManagerV3TEERegistrationEvidence, _signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.RegisterTEE(&_Contracts.TransactOpts, _owner, _pendingNonce, _MR_ENCLAVE, _HardwareId, evidence, _signature)
}

// RejectRedactRequest is a paid mutator transaction binding the contract method 0x48655344.
//
// Solidity: function rejectRedactRequest(PermissionManagerV3ReqFields req, bytes32 dPolicy, bytes32 reasonHash, uint256[2] signature) returns(bool)
func (_Contracts *ContractsTransactor) RejectRedactRequest(opts *bind.TransactOpts, req PermissionManagerV3ReqFields, dPolicy [32]byte, reasonHash [32]byte, signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "rejectRedactRequest", req, dPolicy, reasonHash, signature)
}

// RejectRedactRequest is a paid mutator transaction binding the contract method 0x48655344.
//
// Solidity: function rejectRedactRequest(PermissionManagerV3ReqFields req, bytes32 dPolicy, bytes32 reasonHash, uint256[2] signature) returns(bool)
func (_Contracts *ContractsSession) RejectRedactRequest(req PermissionManagerV3ReqFields, dPolicy [32]byte, reasonHash [32]byte, signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.RejectRedactRequest(&_Contracts.TransactOpts, req, dPolicy, reasonHash, signature)
}

// RejectRedactRequest is a paid mutator transaction binding the contract method 0x48655344.
//
// Solidity: function rejectRedactRequest(PermissionManagerV3ReqFields req, bytes32 dPolicy, bytes32 reasonHash, uint256[2] signature) returns(bool)
func (_Contracts *ContractsTransactorSession) RejectRedactRequest(req PermissionManagerV3ReqFields, dPolicy [32]byte, reasonHash [32]byte, signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.RejectRedactRequest(&_Contracts.TransactOpts, req, dPolicy, reasonHash, signature)
}

// RevokeTEE is a paid mutator transaction binding the contract method 0x3c498647.
//
// Solidity: function revokeTEE(bytes32 sigPubKey, bytes32 reasonHash, uint256 slashAmount, uint256[2] _signature) returns(bool)
func (_Contracts *ContractsTransactor) RevokeTEE(opts *bind.TransactOpts, sigPubKey [32]byte, reasonHash [32]byte, slashAmount *big.Int, _signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "revokeTEE", sigPubKey, reasonHash, slashAmount, _signature)
}

// RevokeTEE is a paid mutator transaction binding the contract method 0x3c498647.
//
// Solidity: function revokeTEE(bytes32 sigPubKey, bytes32 reasonHash, uint256 slashAmount, uint256[2] _signature) returns(bool)
func (_Contracts *ContractsSession) RevokeTEE(sigPubKey [32]byte, reasonHash [32]byte, slashAmount *big.Int, _signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.RevokeTEE(&_Contracts.TransactOpts, sigPubKey, reasonHash, slashAmount, _signature)
}

// RevokeTEE is a paid mutator transaction binding the contract method 0x3c498647.
//
// Solidity: function revokeTEE(bytes32 sigPubKey, bytes32 reasonHash, uint256 slashAmount, uint256[2] _signature) returns(bool)
func (_Contracts *ContractsTransactorSession) RevokeTEE(sigPubKey [32]byte, reasonHash [32]byte, slashAmount *big.Int, _signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.RevokeTEE(&_Contracts.TransactOpts, sigPubKey, reasonHash, slashAmount, _signature)
}

// SetPkCH is a paid mutator transaction binding the contract method 0xca5eb7bc.
//
// Solidity: function setPkCH(bytes _pkCH, bytes signature) returns()
func (_Contracts *ContractsTransactor) SetPkCH(opts *bind.TransactOpts, _pkCH []byte, signature []byte) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "setPkCH", _pkCH, signature)
}

// SetPkCH is a paid mutator transaction binding the contract method 0xca5eb7bc.
//
// Solidity: function setPkCH(bytes _pkCH, bytes signature) returns()
func (_Contracts *ContractsSession) SetPkCH(_pkCH []byte, signature []byte) (*types.Transaction, error) {
	return _Contracts.Contract.SetPkCH(&_Contracts.TransactOpts, _pkCH, signature)
}

// SetPkCH is a paid mutator transaction binding the contract method 0xca5eb7bc.
//
// Solidity: function setPkCH(bytes _pkCH, bytes signature) returns()
func (_Contracts *ContractsTransactorSession) SetPkCH(_pkCH []byte, signature []byte) (*types.Transaction, error) {
	return _Contracts.Contract.SetPkCH(&_Contracts.TransactOpts, _pkCH, signature)
}

// SetTrustedMREnclave is a paid mutator transaction binding the contract method 0x8364c1a1.
//
// Solidity: function setTrustedMREnclave(bytes32 mrEnclave, bool trusted) returns(bool)
func (_Contracts *ContractsTransactor) SetTrustedMREnclave(opts *bind.TransactOpts, mrEnclave [32]byte, trusted bool) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "setTrustedMREnclave", mrEnclave, trusted)
}

// SetTrustedMREnclave is a paid mutator transaction binding the contract method 0x8364c1a1.
//
// Solidity: function setTrustedMREnclave(bytes32 mrEnclave, bool trusted) returns(bool)
func (_Contracts *ContractsSession) SetTrustedMREnclave(mrEnclave [32]byte, trusted bool) (*types.Transaction, error) {
	return _Contracts.Contract.SetTrustedMREnclave(&_Contracts.TransactOpts, mrEnclave, trusted)
}

// SetTrustedMREnclave is a paid mutator transaction binding the contract method 0x8364c1a1.
//
// Solidity: function setTrustedMREnclave(bytes32 mrEnclave, bool trusted) returns(bool)
func (_Contracts *ContractsTransactorSession) SetTrustedMREnclave(mrEnclave [32]byte, trusted bool) (*types.Transaction, error) {
	return _Contracts.Contract.SetTrustedMREnclave(&_Contracts.TransactOpts, mrEnclave, trusted)
}

// StakeTEE is a paid mutator transaction binding the contract method 0x1af8267f.
//
// Solidity: function stakeTEE() returns(uint256 pendingNonce)
func (_Contracts *ContractsTransactor) StakeTEE(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "stakeTEE")
}

// StakeTEE is a paid mutator transaction binding the contract method 0x1af8267f.
//
// Solidity: function stakeTEE() returns(uint256 pendingNonce)
func (_Contracts *ContractsSession) StakeTEE() (*types.Transaction, error) {
	return _Contracts.Contract.StakeTEE(&_Contracts.TransactOpts)
}

// StakeTEE is a paid mutator transaction binding the contract method 0x1af8267f.
//
// Solidity: function stakeTEE() returns(uint256 pendingNonce)
func (_Contracts *ContractsTransactorSession) StakeTEE() (*types.Transaction, error) {
	return _Contracts.Contract.StakeTEE(&_Contracts.TransactOpts)
}

// SubmitEraseProof is a paid mutator transaction binding the contract method 0x72a4330e.
//
// Solidity: function submitEraseProof(bytes32 sigPubKey, uint256 epoch, bytes32 eraseProofHash, bytes signature) returns(bool)
func (_Contracts *ContractsTransactor) SubmitEraseProof(opts *bind.TransactOpts, sigPubKey [32]byte, epoch *big.Int, eraseProofHash [32]byte, signature []byte) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "submitEraseProof", sigPubKey, epoch, eraseProofHash, signature)
}

// SubmitEraseProof is a paid mutator transaction binding the contract method 0x72a4330e.
//
// Solidity: function submitEraseProof(bytes32 sigPubKey, uint256 epoch, bytes32 eraseProofHash, bytes signature) returns(bool)
func (_Contracts *ContractsSession) SubmitEraseProof(sigPubKey [32]byte, epoch *big.Int, eraseProofHash [32]byte, signature []byte) (*types.Transaction, error) {
	return _Contracts.Contract.SubmitEraseProof(&_Contracts.TransactOpts, sigPubKey, epoch, eraseProofHash, signature)
}

// SubmitEraseProof is a paid mutator transaction binding the contract method 0x72a4330e.
//
// Solidity: function submitEraseProof(bytes32 sigPubKey, uint256 epoch, bytes32 eraseProofHash, bytes signature) returns(bool)
func (_Contracts *ContractsTransactorSession) SubmitEraseProof(sigPubKey [32]byte, epoch *big.Int, eraseProofHash [32]byte, signature []byte) (*types.Transaction, error) {
	return _Contracts.Contract.SubmitEraseProof(&_Contracts.TransactOpts, sigPubKey, epoch, eraseProofHash, signature)
}

// SubmitRedactAuthorization is a paid mutator transaction binding the contract method 0x51a4111b.
//
// Solidity: function submitRedactAuthorization(PermissionManagerV3ReqFields req, bytes32 dPolicy, uint256 expiresAt, uint256[2] signature) returns(bool)
func (_Contracts *ContractsTransactor) SubmitRedactAuthorization(opts *bind.TransactOpts, req PermissionManagerV3ReqFields, dPolicy [32]byte, expiresAt *big.Int, signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "submitRedactAuthorization", req, dPolicy, expiresAt, signature)
}

// SubmitRedactAuthorization is a paid mutator transaction binding the contract method 0x51a4111b.
//
// Solidity: function submitRedactAuthorization(PermissionManagerV3ReqFields req, bytes32 dPolicy, uint256 expiresAt, uint256[2] signature) returns(bool)
func (_Contracts *ContractsSession) SubmitRedactAuthorization(req PermissionManagerV3ReqFields, dPolicy [32]byte, expiresAt *big.Int, signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.SubmitRedactAuthorization(&_Contracts.TransactOpts, req, dPolicy, expiresAt, signature)
}

// SubmitRedactAuthorization is a paid mutator transaction binding the contract method 0x51a4111b.
//
// Solidity: function submitRedactAuthorization(PermissionManagerV3ReqFields req, bytes32 dPolicy, uint256 expiresAt, uint256[2] signature) returns(bool)
func (_Contracts *ContractsTransactorSession) SubmitRedactAuthorization(req PermissionManagerV3ReqFields, dPolicy [32]byte, expiresAt *big.Int, signature [2]*big.Int) (*types.Transaction, error) {
	return _Contracts.Contract.SubmitRedactAuthorization(&_Contracts.TransactOpts, req, dPolicy, expiresAt, signature)
}

// SubmitTEESelectionVRF is a paid mutator transaction binding the contract method 0x1e5cccd8.
//
// Solidity: function submitTEESelectionVRF(uint256 epoch, bytes32 sigPubKey, bytes32 vrfResult, bytes vrfProof) returns(bool)
func (_Contracts *ContractsTransactor) SubmitTEESelectionVRF(opts *bind.TransactOpts, epoch *big.Int, sigPubKey [32]byte, vrfResult [32]byte, vrfProof []byte) (*types.Transaction, error) {
	return _Contracts.contract.Transact(opts, "submitTEESelectionVRF", epoch, sigPubKey, vrfResult, vrfProof)
}

// SubmitTEESelectionVRF is a paid mutator transaction binding the contract method 0x1e5cccd8.
//
// Solidity: function submitTEESelectionVRF(uint256 epoch, bytes32 sigPubKey, bytes32 vrfResult, bytes vrfProof) returns(bool)
func (_Contracts *ContractsSession) SubmitTEESelectionVRF(epoch *big.Int, sigPubKey [32]byte, vrfResult [32]byte, vrfProof []byte) (*types.Transaction, error) {
	return _Contracts.Contract.SubmitTEESelectionVRF(&_Contracts.TransactOpts, epoch, sigPubKey, vrfResult, vrfProof)
}

// SubmitTEESelectionVRF is a paid mutator transaction binding the contract method 0x1e5cccd8.
//
// Solidity: function submitTEESelectionVRF(uint256 epoch, bytes32 sigPubKey, bytes32 vrfResult, bytes vrfProof) returns(bool)
func (_Contracts *ContractsTransactorSession) SubmitTEESelectionVRF(epoch *big.Int, sigPubKey [32]byte, vrfResult [32]byte, vrfProof []byte) (*types.Transaction, error) {
	return _Contracts.Contract.SubmitTEESelectionVRF(&_Contracts.TransactOpts, epoch, sigPubKey, vrfResult, vrfProof)
}

// ContractsActiveSetCheckpointedIterator is returned from FilterActiveSetCheckpointed and is used to iterate over the raw logs and unpacked data for ActiveSetCheckpointed events raised by the Contracts contract.
type ContractsActiveSetCheckpointedIterator struct {
	Event *ContractsActiveSetCheckpointed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsActiveSetCheckpointedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsActiveSetCheckpointed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsActiveSetCheckpointed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsActiveSetCheckpointedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsActiveSetCheckpointedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsActiveSetCheckpointed represents a ActiveSetCheckpointed event raised by the Contracts contract.
type ContractsActiveSetCheckpointed struct {
	Epoch         *big.Int
	ActiveSetHash [32]byte
	HardwareIds   [][32]byte
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterActiveSetCheckpointed is a free log retrieval operation binding the contract event 0x9999a867bedf762df746f19073b4ffcfa489fc80ab3cbff37e142f2ead698ea6.
//
// Solidity: event ActiveSetCheckpointed(uint256 indexed epoch, bytes32 indexed activeSetHash, bytes32[] hardwareIds)
func (_Contracts *ContractsFilterer) FilterActiveSetCheckpointed(opts *bind.FilterOpts, epoch []*big.Int, activeSetHash [][32]byte) (*ContractsActiveSetCheckpointedIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var activeSetHashRule []interface{}
	for _, activeSetHashItem := range activeSetHash {
		activeSetHashRule = append(activeSetHashRule, activeSetHashItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "ActiveSetCheckpointed", epochRule, activeSetHashRule)
	if err != nil {
		return nil, err
	}
	return &ContractsActiveSetCheckpointedIterator{contract: _Contracts.contract, event: "ActiveSetCheckpointed", logs: logs, sub: sub}, nil
}

// WatchActiveSetCheckpointed is a free log subscription operation binding the contract event 0x9999a867bedf762df746f19073b4ffcfa489fc80ab3cbff37e142f2ead698ea6.
//
// Solidity: event ActiveSetCheckpointed(uint256 indexed epoch, bytes32 indexed activeSetHash, bytes32[] hardwareIds)
func (_Contracts *ContractsFilterer) WatchActiveSetCheckpointed(opts *bind.WatchOpts, sink chan<- *ContractsActiveSetCheckpointed, epoch []*big.Int, activeSetHash [][32]byte) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var activeSetHashRule []interface{}
	for _, activeSetHashItem := range activeSetHash {
		activeSetHashRule = append(activeSetHashRule, activeSetHashItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "ActiveSetCheckpointed", epochRule, activeSetHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsActiveSetCheckpointed)
				if err := _Contracts.contract.UnpackLog(event, "ActiveSetCheckpointed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseActiveSetCheckpointed is a log parse operation binding the contract event 0x9999a867bedf762df746f19073b4ffcfa489fc80ab3cbff37e142f2ead698ea6.
//
// Solidity: event ActiveSetCheckpointed(uint256 indexed epoch, bytes32 indexed activeSetHash, bytes32[] hardwareIds)
func (_Contracts *ContractsFilterer) ParseActiveSetCheckpointed(log types.Log) (*ContractsActiveSetCheckpointed, error) {
	event := new(ContractsActiveSetCheckpointed)
	if err := _Contracts.contract.UnpackLog(event, "ActiveSetCheckpointed", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsActiveSetRotatedIterator is returned from FilterActiveSetRotated and is used to iterate over the raw logs and unpacked data for ActiveSetRotated events raised by the Contracts contract.
type ContractsActiveSetRotatedIterator struct {
	Event *ContractsActiveSetRotated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsActiveSetRotatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsActiveSetRotated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsActiveSetRotated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsActiveSetRotatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsActiveSetRotatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsActiveSetRotated represents a ActiveSetRotated event raised by the Contracts contract.
type ContractsActiveSetRotated struct {
	Epoch               *big.Int
	ActiveSetHash       [32]byte
	RotatedAt           *big.Int
	FinalityBlockNumber *big.Int
	FinalityBlockHash   [32]byte
	Seed                [32]byte
	HardwareIds         [][32]byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterActiveSetRotated is a free log retrieval operation binding the contract event 0xf84454c8aef448f966078ee6ef92f10a2079fcc9bf32f042aef6bb38458344ae.
//
// Solidity: event ActiveSetRotated(uint256 indexed epoch, bytes32 indexed activeSetHash, uint256 rotatedAt, uint256 finalityBlockNumber, bytes32 finalityBlockHash, bytes32 seed, bytes32[] hardwareIds)
func (_Contracts *ContractsFilterer) FilterActiveSetRotated(opts *bind.FilterOpts, epoch []*big.Int, activeSetHash [][32]byte) (*ContractsActiveSetRotatedIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var activeSetHashRule []interface{}
	for _, activeSetHashItem := range activeSetHash {
		activeSetHashRule = append(activeSetHashRule, activeSetHashItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "ActiveSetRotated", epochRule, activeSetHashRule)
	if err != nil {
		return nil, err
	}
	return &ContractsActiveSetRotatedIterator{contract: _Contracts.contract, event: "ActiveSetRotated", logs: logs, sub: sub}, nil
}

// WatchActiveSetRotated is a free log subscription operation binding the contract event 0xf84454c8aef448f966078ee6ef92f10a2079fcc9bf32f042aef6bb38458344ae.
//
// Solidity: event ActiveSetRotated(uint256 indexed epoch, bytes32 indexed activeSetHash, uint256 rotatedAt, uint256 finalityBlockNumber, bytes32 finalityBlockHash, bytes32 seed, bytes32[] hardwareIds)
func (_Contracts *ContractsFilterer) WatchActiveSetRotated(opts *bind.WatchOpts, sink chan<- *ContractsActiveSetRotated, epoch []*big.Int, activeSetHash [][32]byte) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var activeSetHashRule []interface{}
	for _, activeSetHashItem := range activeSetHash {
		activeSetHashRule = append(activeSetHashRule, activeSetHashItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "ActiveSetRotated", epochRule, activeSetHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsActiveSetRotated)
				if err := _Contracts.contract.UnpackLog(event, "ActiveSetRotated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseActiveSetRotated is a log parse operation binding the contract event 0xf84454c8aef448f966078ee6ef92f10a2079fcc9bf32f042aef6bb38458344ae.
//
// Solidity: event ActiveSetRotated(uint256 indexed epoch, bytes32 indexed activeSetHash, uint256 rotatedAt, uint256 finalityBlockNumber, bytes32 finalityBlockHash, bytes32 seed, bytes32[] hardwareIds)
func (_Contracts *ContractsFilterer) ParseActiveSetRotated(log types.Log) (*ContractsActiveSetRotated, error) {
	event := new(ContractsActiveSetRotated)
	if err := _Contracts.contract.UnpackLog(event, "ActiveSetRotated", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsActiveSetRotationDeferredIterator is returned from FilterActiveSetRotationDeferred and is used to iterate over the raw logs and unpacked data for ActiveSetRotationDeferred events raised by the Contracts contract.
type ContractsActiveSetRotationDeferredIterator struct {
	Event *ContractsActiveSetRotationDeferred // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsActiveSetRotationDeferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsActiveSetRotationDeferred)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsActiveSetRotationDeferred)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsActiveSetRotationDeferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsActiveSetRotationDeferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsActiveSetRotationDeferred represents a ActiveSetRotationDeferred event raised by the Contracts contract.
type ContractsActiveSetRotationDeferred struct {
	Epoch          *big.Int
	SubmittedCount *big.Int
	RequiredCount  *big.Int
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterActiveSetRotationDeferred is a free log retrieval operation binding the contract event 0x545257ed6a2739c2e37a334fe69415ce2d5367209b0bdba09752b18ab2dc5e67.
//
// Solidity: event ActiveSetRotationDeferred(uint256 indexed epoch, uint256 submittedCount, uint256 requiredCount)
func (_Contracts *ContractsFilterer) FilterActiveSetRotationDeferred(opts *bind.FilterOpts, epoch []*big.Int) (*ContractsActiveSetRotationDeferredIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "ActiveSetRotationDeferred", epochRule)
	if err != nil {
		return nil, err
	}
	return &ContractsActiveSetRotationDeferredIterator{contract: _Contracts.contract, event: "ActiveSetRotationDeferred", logs: logs, sub: sub}, nil
}

// WatchActiveSetRotationDeferred is a free log subscription operation binding the contract event 0x545257ed6a2739c2e37a334fe69415ce2d5367209b0bdba09752b18ab2dc5e67.
//
// Solidity: event ActiveSetRotationDeferred(uint256 indexed epoch, uint256 submittedCount, uint256 requiredCount)
func (_Contracts *ContractsFilterer) WatchActiveSetRotationDeferred(opts *bind.WatchOpts, sink chan<- *ContractsActiveSetRotationDeferred, epoch []*big.Int) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "ActiveSetRotationDeferred", epochRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsActiveSetRotationDeferred)
				if err := _Contracts.contract.UnpackLog(event, "ActiveSetRotationDeferred", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseActiveSetRotationDeferred is a log parse operation binding the contract event 0x545257ed6a2739c2e37a334fe69415ce2d5367209b0bdba09752b18ab2dc5e67.
//
// Solidity: event ActiveSetRotationDeferred(uint256 indexed epoch, uint256 submittedCount, uint256 requiredCount)
func (_Contracts *ContractsFilterer) ParseActiveSetRotationDeferred(log types.Log) (*ContractsActiveSetRotationDeferred, error) {
	event := new(ContractsActiveSetRotationDeferred)
	if err := _Contracts.contract.UnpackLog(event, "ActiveSetRotationDeferred", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsCommitteePubKeyRegisteredIterator is returned from FilterCommitteePubKeyRegistered and is used to iterate over the raw logs and unpacked data for CommitteePubKeyRegistered events raised by the Contracts contract.
type ContractsCommitteePubKeyRegisteredIterator struct {
	Event *ContractsCommitteePubKeyRegistered // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsCommitteePubKeyRegisteredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsCommitteePubKeyRegistered)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsCommitteePubKeyRegistered)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsCommitteePubKeyRegisteredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsCommitteePubKeyRegisteredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsCommitteePubKeyRegistered represents a CommitteePubKeyRegistered event raised by the Contracts contract.
type ContractsCommitteePubKeyRegistered struct {
	Caller common.Address
	PubKey [4]*big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterCommitteePubKeyRegistered is a free log retrieval operation binding the contract event 0x310ee78c937867a3d1525109c5ef1d5d0ad367492ab561fdc0df6f6e2983d786.
//
// Solidity: event CommitteePubKeyRegistered(address indexed caller, uint256[4] pubKey)
func (_Contracts *ContractsFilterer) FilterCommitteePubKeyRegistered(opts *bind.FilterOpts, caller []common.Address) (*ContractsCommitteePubKeyRegisteredIterator, error) {

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "CommitteePubKeyRegistered", callerRule)
	if err != nil {
		return nil, err
	}
	return &ContractsCommitteePubKeyRegisteredIterator{contract: _Contracts.contract, event: "CommitteePubKeyRegistered", logs: logs, sub: sub}, nil
}

// WatchCommitteePubKeyRegistered is a free log subscription operation binding the contract event 0x310ee78c937867a3d1525109c5ef1d5d0ad367492ab561fdc0df6f6e2983d786.
//
// Solidity: event CommitteePubKeyRegistered(address indexed caller, uint256[4] pubKey)
func (_Contracts *ContractsFilterer) WatchCommitteePubKeyRegistered(opts *bind.WatchOpts, sink chan<- *ContractsCommitteePubKeyRegistered, caller []common.Address) (event.Subscription, error) {

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "CommitteePubKeyRegistered", callerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsCommitteePubKeyRegistered)
				if err := _Contracts.contract.UnpackLog(event, "CommitteePubKeyRegistered", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseCommitteePubKeyRegistered is a log parse operation binding the contract event 0x310ee78c937867a3d1525109c5ef1d5d0ad367492ab561fdc0df6f6e2983d786.
//
// Solidity: event CommitteePubKeyRegistered(address indexed caller, uint256[4] pubKey)
func (_Contracts *ContractsFilterer) ParseCommitteePubKeyRegistered(log types.Log) (*ContractsCommitteePubKeyRegistered, error) {
	event := new(ContractsCommitteePubKeyRegistered)
	if err := _Contracts.contract.UnpackLog(event, "CommitteePubKeyRegistered", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsPendingTEECancelledIterator is returned from FilterPendingTEECancelled and is used to iterate over the raw logs and unpacked data for PendingTEECancelled events raised by the Contracts contract.
type ContractsPendingTEECancelledIterator struct {
	Event *ContractsPendingTEECancelled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsPendingTEECancelledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsPendingTEECancelled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsPendingTEECancelled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsPendingTEECancelledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsPendingTEECancelledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsPendingTEECancelled represents a PendingTEECancelled event raised by the Contracts contract.
type ContractsPendingTEECancelled struct {
	Owner        common.Address
	PendingNonce *big.Int
	Deposit      *big.Int
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterPendingTEECancelled is a free log retrieval operation binding the contract event 0x5463fb04a95f5fe5300ab0be9d3632d0f4b30b6d9d5d9eae3bf3e664612331bd.
//
// Solidity: event PendingTEECancelled(address indexed owner, uint256 indexed pendingNonce, uint256 deposit)
func (_Contracts *ContractsFilterer) FilterPendingTEECancelled(opts *bind.FilterOpts, owner []common.Address, pendingNonce []*big.Int) (*ContractsPendingTEECancelledIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var pendingNonceRule []interface{}
	for _, pendingNonceItem := range pendingNonce {
		pendingNonceRule = append(pendingNonceRule, pendingNonceItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "PendingTEECancelled", ownerRule, pendingNonceRule)
	if err != nil {
		return nil, err
	}
	return &ContractsPendingTEECancelledIterator{contract: _Contracts.contract, event: "PendingTEECancelled", logs: logs, sub: sub}, nil
}

// WatchPendingTEECancelled is a free log subscription operation binding the contract event 0x5463fb04a95f5fe5300ab0be9d3632d0f4b30b6d9d5d9eae3bf3e664612331bd.
//
// Solidity: event PendingTEECancelled(address indexed owner, uint256 indexed pendingNonce, uint256 deposit)
func (_Contracts *ContractsFilterer) WatchPendingTEECancelled(opts *bind.WatchOpts, sink chan<- *ContractsPendingTEECancelled, owner []common.Address, pendingNonce []*big.Int) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var pendingNonceRule []interface{}
	for _, pendingNonceItem := range pendingNonce {
		pendingNonceRule = append(pendingNonceRule, pendingNonceItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "PendingTEECancelled", ownerRule, pendingNonceRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsPendingTEECancelled)
				if err := _Contracts.contract.UnpackLog(event, "PendingTEECancelled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParsePendingTEECancelled is a log parse operation binding the contract event 0x5463fb04a95f5fe5300ab0be9d3632d0f4b30b6d9d5d9eae3bf3e664612331bd.
//
// Solidity: event PendingTEECancelled(address indexed owner, uint256 indexed pendingNonce, uint256 deposit)
func (_Contracts *ContractsFilterer) ParsePendingTEECancelled(log types.Log) (*ContractsPendingTEECancelled, error) {
	event := new(ContractsPendingTEECancelled)
	if err := _Contracts.contract.UnpackLog(event, "PendingTEECancelled", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsPkCHSetIterator is returned from FilterPkCHSet and is used to iterate over the raw logs and unpacked data for PkCHSet events raised by the Contracts contract.
type ContractsPkCHSetIterator struct {
	Event *ContractsPkCHSet // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsPkCHSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsPkCHSet)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsPkCHSet)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsPkCHSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsPkCHSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsPkCHSet represents a PkCHSet event raised by the Contracts contract.
type ContractsPkCHSet struct {
	Version  *big.Int
	PkCHHash [32]byte
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterPkCHSet is a free log retrieval operation binding the contract event 0xfdb5ca5200719124ffc6cfbdfb877d825b7c50e80612d700f92cddd1358416e4.
//
// Solidity: event PkCHSet(uint256 indexed version, bytes32 indexed pkCHHash)
func (_Contracts *ContractsFilterer) FilterPkCHSet(opts *bind.FilterOpts, version []*big.Int, pkCHHash [][32]byte) (*ContractsPkCHSetIterator, error) {

	var versionRule []interface{}
	for _, versionItem := range version {
		versionRule = append(versionRule, versionItem)
	}
	var pkCHHashRule []interface{}
	for _, pkCHHashItem := range pkCHHash {
		pkCHHashRule = append(pkCHHashRule, pkCHHashItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "PkCHSet", versionRule, pkCHHashRule)
	if err != nil {
		return nil, err
	}
	return &ContractsPkCHSetIterator{contract: _Contracts.contract, event: "PkCHSet", logs: logs, sub: sub}, nil
}

// WatchPkCHSet is a free log subscription operation binding the contract event 0xfdb5ca5200719124ffc6cfbdfb877d825b7c50e80612d700f92cddd1358416e4.
//
// Solidity: event PkCHSet(uint256 indexed version, bytes32 indexed pkCHHash)
func (_Contracts *ContractsFilterer) WatchPkCHSet(opts *bind.WatchOpts, sink chan<- *ContractsPkCHSet, version []*big.Int, pkCHHash [][32]byte) (event.Subscription, error) {

	var versionRule []interface{}
	for _, versionItem := range version {
		versionRule = append(versionRule, versionItem)
	}
	var pkCHHashRule []interface{}
	for _, pkCHHashItem := range pkCHHash {
		pkCHHashRule = append(pkCHHashRule, pkCHHashItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "PkCHSet", versionRule, pkCHHashRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsPkCHSet)
				if err := _Contracts.contract.UnpackLog(event, "PkCHSet", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParsePkCHSet is a log parse operation binding the contract event 0xfdb5ca5200719124ffc6cfbdfb877d825b7c50e80612d700f92cddd1358416e4.
//
// Solidity: event PkCHSet(uint256 indexed version, bytes32 indexed pkCHHash)
func (_Contracts *ContractsFilterer) ParsePkCHSet(log types.Log) (*ContractsPkCHSet, error) {
	event := new(ContractsPkCHSet)
	if err := _Contracts.contract.UnpackLog(event, "PkCHSet", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsRedactAuthorizedIterator is returned from FilterRedactAuthorized and is used to iterate over the raw logs and unpacked data for RedactAuthorized events raised by the Contracts contract.
type ContractsRedactAuthorizedIterator struct {
	Event *ContractsRedactAuthorized // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsRedactAuthorizedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsRedactAuthorized)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsRedactAuthorized)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsRedactAuthorizedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsRedactAuthorizedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsRedactAuthorized represents a RedactAuthorized event raised by the Contracts contract.
type ContractsRedactAuthorized struct {
	ReqID                  [32]byte
	Epoch                  *big.Int
	DReq                   [32]byte
	DPolicy                [32]byte
	ActiveSetHash          [32]byte
	PkCHHash               [32]byte
	CommitteePubKeyVersion *big.Int
	ExpiresAt              *big.Int
	Signature              [2]*big.Int
	Raw                    types.Log // Blockchain specific contextual infos
}

// FilterRedactAuthorized is a free log retrieval operation binding the contract event 0xada59ac183a19b56e18e22bb2912fe051d8fcd2b8762ccfddbea3eb6d2b36652.
//
// Solidity: event RedactAuthorized(bytes32 indexed reqID, uint256 indexed epoch, bytes32 dReq, bytes32 dPolicy, bytes32 activeSetHash, bytes32 pkCHHash, uint256 committeePubKeyVersion, uint256 expiresAt, uint256[2] signature)
func (_Contracts *ContractsFilterer) FilterRedactAuthorized(opts *bind.FilterOpts, reqID [][32]byte, epoch []*big.Int) (*ContractsRedactAuthorizedIterator, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}
	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "RedactAuthorized", reqIDRule, epochRule)
	if err != nil {
		return nil, err
	}
	return &ContractsRedactAuthorizedIterator{contract: _Contracts.contract, event: "RedactAuthorized", logs: logs, sub: sub}, nil
}

// WatchRedactAuthorized is a free log subscription operation binding the contract event 0xada59ac183a19b56e18e22bb2912fe051d8fcd2b8762ccfddbea3eb6d2b36652.
//
// Solidity: event RedactAuthorized(bytes32 indexed reqID, uint256 indexed epoch, bytes32 dReq, bytes32 dPolicy, bytes32 activeSetHash, bytes32 pkCHHash, uint256 committeePubKeyVersion, uint256 expiresAt, uint256[2] signature)
func (_Contracts *ContractsFilterer) WatchRedactAuthorized(opts *bind.WatchOpts, sink chan<- *ContractsRedactAuthorized, reqID [][32]byte, epoch []*big.Int) (event.Subscription, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}
	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "RedactAuthorized", reqIDRule, epochRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsRedactAuthorized)
				if err := _Contracts.contract.UnpackLog(event, "RedactAuthorized", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRedactAuthorized is a log parse operation binding the contract event 0xada59ac183a19b56e18e22bb2912fe051d8fcd2b8762ccfddbea3eb6d2b36652.
//
// Solidity: event RedactAuthorized(bytes32 indexed reqID, uint256 indexed epoch, bytes32 dReq, bytes32 dPolicy, bytes32 activeSetHash, bytes32 pkCHHash, uint256 committeePubKeyVersion, uint256 expiresAt, uint256[2] signature)
func (_Contracts *ContractsFilterer) ParseRedactAuthorized(log types.Log) (*ContractsRedactAuthorized, error) {
	event := new(ContractsRedactAuthorized)
	if err := _Contracts.contract.UnpackLog(event, "RedactAuthorized", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsRedactExecutedIterator is returned from FilterRedactExecuted and is used to iterate over the raw logs and unpacked data for RedactExecuted events raised by the Contracts contract.
type ContractsRedactExecutedIterator struct {
	Event *ContractsRedactExecuted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsRedactExecutedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsRedactExecuted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsRedactExecuted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsRedactExecutedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsRedactExecutedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsRedactExecuted represents a RedactExecuted event raised by the Contracts contract.
type ContractsRedactExecuted struct {
	ReqID      [32]byte
	RPrimeHash [32]byte
	ProofHash  [32]byte
	ResultHash [32]byte
	PiReqAHash [32]byte
	PiReqA     []byte
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRedactExecuted is a free log retrieval operation binding the contract event 0x59d6f0fc082eb7202ec82de15ba92b56013698937bb5266c48d824e56377fd8c.
//
// Solidity: event RedactExecuted(bytes32 indexed reqID, bytes32 rPrimeHash, bytes32 proofHash, bytes32 resultHash, bytes32 piReqAHash, bytes piReqA)
func (_Contracts *ContractsFilterer) FilterRedactExecuted(opts *bind.FilterOpts, reqID [][32]byte) (*ContractsRedactExecutedIterator, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "RedactExecuted", reqIDRule)
	if err != nil {
		return nil, err
	}
	return &ContractsRedactExecutedIterator{contract: _Contracts.contract, event: "RedactExecuted", logs: logs, sub: sub}, nil
}

// WatchRedactExecuted is a free log subscription operation binding the contract event 0x59d6f0fc082eb7202ec82de15ba92b56013698937bb5266c48d824e56377fd8c.
//
// Solidity: event RedactExecuted(bytes32 indexed reqID, bytes32 rPrimeHash, bytes32 proofHash, bytes32 resultHash, bytes32 piReqAHash, bytes piReqA)
func (_Contracts *ContractsFilterer) WatchRedactExecuted(opts *bind.WatchOpts, sink chan<- *ContractsRedactExecuted, reqID [][32]byte) (event.Subscription, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "RedactExecuted", reqIDRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsRedactExecuted)
				if err := _Contracts.contract.UnpackLog(event, "RedactExecuted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRedactExecuted is a log parse operation binding the contract event 0x59d6f0fc082eb7202ec82de15ba92b56013698937bb5266c48d824e56377fd8c.
//
// Solidity: event RedactExecuted(bytes32 indexed reqID, bytes32 rPrimeHash, bytes32 proofHash, bytes32 resultHash, bytes32 piReqAHash, bytes piReqA)
func (_Contracts *ContractsFilterer) ParseRedactExecuted(log types.Log) (*ContractsRedactExecuted, error) {
	event := new(ContractsRedactExecuted)
	if err := _Contracts.contract.UnpackLog(event, "RedactExecuted", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsRedactExpiredIterator is returned from FilterRedactExpired and is used to iterate over the raw logs and unpacked data for RedactExpired events raised by the Contracts contract.
type ContractsRedactExpiredIterator struct {
	Event *ContractsRedactExpired // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsRedactExpiredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsRedactExpired)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsRedactExpired)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsRedactExpiredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsRedactExpiredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsRedactExpired represents a RedactExpired event raised by the Contracts contract.
type ContractsRedactExpired struct {
	ReqID     [32]byte
	ExpiredAt *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterRedactExpired is a free log retrieval operation binding the contract event 0x8b87668059bc287b70db8f03625d588e3311c769a5d3cba6f6089d494126c44b.
//
// Solidity: event RedactExpired(bytes32 indexed reqID, uint256 expiredAt)
func (_Contracts *ContractsFilterer) FilterRedactExpired(opts *bind.FilterOpts, reqID [][32]byte) (*ContractsRedactExpiredIterator, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "RedactExpired", reqIDRule)
	if err != nil {
		return nil, err
	}
	return &ContractsRedactExpiredIterator{contract: _Contracts.contract, event: "RedactExpired", logs: logs, sub: sub}, nil
}

// WatchRedactExpired is a free log subscription operation binding the contract event 0x8b87668059bc287b70db8f03625d588e3311c769a5d3cba6f6089d494126c44b.
//
// Solidity: event RedactExpired(bytes32 indexed reqID, uint256 expiredAt)
func (_Contracts *ContractsFilterer) WatchRedactExpired(opts *bind.WatchOpts, sink chan<- *ContractsRedactExpired, reqID [][32]byte) (event.Subscription, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "RedactExpired", reqIDRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsRedactExpired)
				if err := _Contracts.contract.UnpackLog(event, "RedactExpired", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRedactExpired is a log parse operation binding the contract event 0x8b87668059bc287b70db8f03625d588e3311c769a5d3cba6f6089d494126c44b.
//
// Solidity: event RedactExpired(bytes32 indexed reqID, uint256 expiredAt)
func (_Contracts *ContractsFilterer) ParseRedactExpired(log types.Log) (*ContractsRedactExpired, error) {
	event := new(ContractsRedactExpired)
	if err := _Contracts.contract.UnpackLog(event, "RedactExpired", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsRedactRejectedIterator is returned from FilterRedactRejected and is used to iterate over the raw logs and unpacked data for RedactRejected events raised by the Contracts contract.
type ContractsRedactRejectedIterator struct {
	Event *ContractsRedactRejected // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsRedactRejectedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsRedactRejected)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsRedactRejected)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsRedactRejectedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsRedactRejectedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsRedactRejected represents a RedactRejected event raised by the Contracts contract.
type ContractsRedactRejected struct {
	ReqID      [32]byte
	DReq       [32]byte
	DPolicy    [32]byte
	ReasonHash [32]byte
	Signature  [2]*big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRedactRejected is a free log retrieval operation binding the contract event 0x3e8bb8f721df92ea6a48860bcaf3465b3d7bfb740cdb9387f08c2a72add5cf02.
//
// Solidity: event RedactRejected(bytes32 indexed reqID, bytes32 dReq, bytes32 dPolicy, bytes32 reasonHash, uint256[2] signature)
func (_Contracts *ContractsFilterer) FilterRedactRejected(opts *bind.FilterOpts, reqID [][32]byte) (*ContractsRedactRejectedIterator, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "RedactRejected", reqIDRule)
	if err != nil {
		return nil, err
	}
	return &ContractsRedactRejectedIterator{contract: _Contracts.contract, event: "RedactRejected", logs: logs, sub: sub}, nil
}

// WatchRedactRejected is a free log subscription operation binding the contract event 0x3e8bb8f721df92ea6a48860bcaf3465b3d7bfb740cdb9387f08c2a72add5cf02.
//
// Solidity: event RedactRejected(bytes32 indexed reqID, bytes32 dReq, bytes32 dPolicy, bytes32 reasonHash, uint256[2] signature)
func (_Contracts *ContractsFilterer) WatchRedactRejected(opts *bind.WatchOpts, sink chan<- *ContractsRedactRejected, reqID [][32]byte) (event.Subscription, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "RedactRejected", reqIDRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsRedactRejected)
				if err := _Contracts.contract.UnpackLog(event, "RedactRejected", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRedactRejected is a log parse operation binding the contract event 0x3e8bb8f721df92ea6a48860bcaf3465b3d7bfb740cdb9387f08c2a72add5cf02.
//
// Solidity: event RedactRejected(bytes32 indexed reqID, bytes32 dReq, bytes32 dPolicy, bytes32 reasonHash, uint256[2] signature)
func (_Contracts *ContractsFilterer) ParseRedactRejected(log types.Log) (*ContractsRedactRejected, error) {
	event := new(ContractsRedactRejected)
	if err := _Contracts.contract.UnpackLog(event, "RedactRejected", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsRedactRequestPayloadRecordedIterator is returned from FilterRedactRequestPayloadRecorded and is used to iterate over the raw logs and unpacked data for RedactRequestPayloadRecorded events raised by the Contracts contract.
type ContractsRedactRequestPayloadRecordedIterator struct {
	Event *ContractsRedactRequestPayloadRecorded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsRedactRequestPayloadRecordedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsRedactRequestPayloadRecorded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsRedactRequestPayloadRecorded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsRedactRequestPayloadRecordedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsRedactRequestPayloadRecordedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsRedactRequestPayloadRecorded represents a RedactRequestPayloadRecorded event raised by the Contracts contract.
type ContractsRedactRequestPayloadRecorded struct {
	ReqID         [32]byte
	TxID          [32]byte
	MHash         [32]byte
	MPrimeHash    [32]byte
	PosHash       [32]byte
	MetaHash      [32]byte
	ActiveSetHash [32]byte
	PkCHHash      [32]byte
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterRedactRequestPayloadRecorded is a free log retrieval operation binding the contract event 0xcc41da18ef531d7ab13d9c2b6b27e7edec2ee91ba944920fce962ca335e6ca79.
//
// Solidity: event RedactRequestPayloadRecorded(bytes32 indexed reqID, bytes32 txID, bytes32 mHash, bytes32 mPrimeHash, bytes32 posHash, bytes32 metaHash, bytes32 activeSetHash, bytes32 pkCHHash)
func (_Contracts *ContractsFilterer) FilterRedactRequestPayloadRecorded(opts *bind.FilterOpts, reqID [][32]byte) (*ContractsRedactRequestPayloadRecordedIterator, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "RedactRequestPayloadRecorded", reqIDRule)
	if err != nil {
		return nil, err
	}
	return &ContractsRedactRequestPayloadRecordedIterator{contract: _Contracts.contract, event: "RedactRequestPayloadRecorded", logs: logs, sub: sub}, nil
}

// WatchRedactRequestPayloadRecorded is a free log subscription operation binding the contract event 0xcc41da18ef531d7ab13d9c2b6b27e7edec2ee91ba944920fce962ca335e6ca79.
//
// Solidity: event RedactRequestPayloadRecorded(bytes32 indexed reqID, bytes32 txID, bytes32 mHash, bytes32 mPrimeHash, bytes32 posHash, bytes32 metaHash, bytes32 activeSetHash, bytes32 pkCHHash)
func (_Contracts *ContractsFilterer) WatchRedactRequestPayloadRecorded(opts *bind.WatchOpts, sink chan<- *ContractsRedactRequestPayloadRecorded, reqID [][32]byte) (event.Subscription, error) {

	var reqIDRule []interface{}
	for _, reqIDItem := range reqID {
		reqIDRule = append(reqIDRule, reqIDItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "RedactRequestPayloadRecorded", reqIDRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsRedactRequestPayloadRecorded)
				if err := _Contracts.contract.UnpackLog(event, "RedactRequestPayloadRecorded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRedactRequestPayloadRecorded is a log parse operation binding the contract event 0xcc41da18ef531d7ab13d9c2b6b27e7edec2ee91ba944920fce962ca335e6ca79.
//
// Solidity: event RedactRequestPayloadRecorded(bytes32 indexed reqID, bytes32 txID, bytes32 mHash, bytes32 mPrimeHash, bytes32 posHash, bytes32 metaHash, bytes32 activeSetHash, bytes32 pkCHHash)
func (_Contracts *ContractsFilterer) ParseRedactRequestPayloadRecorded(log types.Log) (*ContractsRedactRequestPayloadRecorded, error) {
	event := new(ContractsRedactRequestPayloadRecorded)
	if err := _Contracts.contract.UnpackLog(event, "RedactRequestPayloadRecorded", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEEEraseProofDeadlineSetIterator is returned from FilterTEEEraseProofDeadlineSet and is used to iterate over the raw logs and unpacked data for TEEEraseProofDeadlineSet events raised by the Contracts contract.
type ContractsTEEEraseProofDeadlineSetIterator struct {
	Event *ContractsTEEEraseProofDeadlineSet // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEEEraseProofDeadlineSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEEEraseProofDeadlineSet)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEEEraseProofDeadlineSet)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEEEraseProofDeadlineSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEEEraseProofDeadlineSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEEEraseProofDeadlineSet represents a TEEEraseProofDeadlineSet event raised by the Contracts contract.
type ContractsTEEEraseProofDeadlineSet struct {
	HardwareId    [32]byte
	Owner         common.Address
	ExitEpoch     *big.Int
	DeadlineBlock *big.Int
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterTEEEraseProofDeadlineSet is a free log retrieval operation binding the contract event 0xd89759ab8b2be4f199931242fe85754f7fa5512fc67c88a0cc5766b2b6fa279d.
//
// Solidity: event TEEEraseProofDeadlineSet(bytes32 indexed HardwareId, address indexed owner, uint256 indexed exitEpoch, uint256 deadlineBlock)
func (_Contracts *ContractsFilterer) FilterTEEEraseProofDeadlineSet(opts *bind.FilterOpts, HardwareId [][32]byte, owner []common.Address, exitEpoch []*big.Int) (*ContractsTEEEraseProofDeadlineSetIterator, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var exitEpochRule []interface{}
	for _, exitEpochItem := range exitEpoch {
		exitEpochRule = append(exitEpochRule, exitEpochItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEEEraseProofDeadlineSet", HardwareIdRule, ownerRule, exitEpochRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEEEraseProofDeadlineSetIterator{contract: _Contracts.contract, event: "TEEEraseProofDeadlineSet", logs: logs, sub: sub}, nil
}

// WatchTEEEraseProofDeadlineSet is a free log subscription operation binding the contract event 0xd89759ab8b2be4f199931242fe85754f7fa5512fc67c88a0cc5766b2b6fa279d.
//
// Solidity: event TEEEraseProofDeadlineSet(bytes32 indexed HardwareId, address indexed owner, uint256 indexed exitEpoch, uint256 deadlineBlock)
func (_Contracts *ContractsFilterer) WatchTEEEraseProofDeadlineSet(opts *bind.WatchOpts, sink chan<- *ContractsTEEEraseProofDeadlineSet, HardwareId [][32]byte, owner []common.Address, exitEpoch []*big.Int) (event.Subscription, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var exitEpochRule []interface{}
	for _, exitEpochItem := range exitEpoch {
		exitEpochRule = append(exitEpochRule, exitEpochItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEEEraseProofDeadlineSet", HardwareIdRule, ownerRule, exitEpochRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEEEraseProofDeadlineSet)
				if err := _Contracts.contract.UnpackLog(event, "TEEEraseProofDeadlineSet", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEEEraseProofDeadlineSet is a log parse operation binding the contract event 0xd89759ab8b2be4f199931242fe85754f7fa5512fc67c88a0cc5766b2b6fa279d.
//
// Solidity: event TEEEraseProofDeadlineSet(bytes32 indexed HardwareId, address indexed owner, uint256 indexed exitEpoch, uint256 deadlineBlock)
func (_Contracts *ContractsFilterer) ParseTEEEraseProofDeadlineSet(log types.Log) (*ContractsTEEEraseProofDeadlineSet, error) {
	event := new(ContractsTEEEraseProofDeadlineSet)
	if err := _Contracts.contract.UnpackLog(event, "TEEEraseProofDeadlineSet", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEEExitPendingIterator is returned from FilterTEEExitPending and is used to iterate over the raw logs and unpacked data for TEEExitPending events raised by the Contracts contract.
type ContractsTEEExitPendingIterator struct {
	Event *ContractsTEEExitPending // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEEExitPendingIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEEExitPending)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEEExitPending)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEEExitPendingIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEEExitPendingIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEEExitPending represents a TEEExitPending event raised by the Contracts contract.
type ContractsTEEExitPending struct {
	HardwareId [32]byte
	Owner      common.Address
	FromEpoch  *big.Int
	NextEpoch  *big.Int
	ExitedAt   *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterTEEExitPending is a free log retrieval operation binding the contract event 0x889a967a8b6706921f0ff6eae756673b3a45720e8ebe168e5d156a2f37214c5b.
//
// Solidity: event TEEExitPending(bytes32 indexed HardwareId, address indexed owner, uint256 indexed fromEpoch, uint256 nextEpoch, uint256 exitedAt)
func (_Contracts *ContractsFilterer) FilterTEEExitPending(opts *bind.FilterOpts, HardwareId [][32]byte, owner []common.Address, fromEpoch []*big.Int) (*ContractsTEEExitPendingIterator, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var fromEpochRule []interface{}
	for _, fromEpochItem := range fromEpoch {
		fromEpochRule = append(fromEpochRule, fromEpochItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEEExitPending", HardwareIdRule, ownerRule, fromEpochRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEEExitPendingIterator{contract: _Contracts.contract, event: "TEEExitPending", logs: logs, sub: sub}, nil
}

// WatchTEEExitPending is a free log subscription operation binding the contract event 0x889a967a8b6706921f0ff6eae756673b3a45720e8ebe168e5d156a2f37214c5b.
//
// Solidity: event TEEExitPending(bytes32 indexed HardwareId, address indexed owner, uint256 indexed fromEpoch, uint256 nextEpoch, uint256 exitedAt)
func (_Contracts *ContractsFilterer) WatchTEEExitPending(opts *bind.WatchOpts, sink chan<- *ContractsTEEExitPending, HardwareId [][32]byte, owner []common.Address, fromEpoch []*big.Int) (event.Subscription, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var fromEpochRule []interface{}
	for _, fromEpochItem := range fromEpoch {
		fromEpochRule = append(fromEpochRule, fromEpochItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEEExitPending", HardwareIdRule, ownerRule, fromEpochRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEEExitPending)
				if err := _Contracts.contract.UnpackLog(event, "TEEExitPending", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEEExitPending is a log parse operation binding the contract event 0x889a967a8b6706921f0ff6eae756673b3a45720e8ebe168e5d156a2f37214c5b.
//
// Solidity: event TEEExitPending(bytes32 indexed HardwareId, address indexed owner, uint256 indexed fromEpoch, uint256 nextEpoch, uint256 exitedAt)
func (_Contracts *ContractsFilterer) ParseTEEExitPending(log types.Log) (*ContractsTEEExitPending, error) {
	event := new(ContractsTEEExitPending)
	if err := _Contracts.contract.UnpackLog(event, "TEEExitPending", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEEExitedIterator is returned from FilterTEEExited and is used to iterate over the raw logs and unpacked data for TEEExited events raised by the Contracts contract.
type ContractsTEEExitedIterator struct {
	Event *ContractsTEEExited // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEEExitedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEEExited)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEEExited)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEEExitedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEEExitedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEEExited represents a TEEExited event raised by the Contracts contract.
type ContractsTEEExited struct {
	HardwareId      [32]byte
	Owner           common.Address
	Epoch           *big.Int
	EraseProofHash  [32]byte
	ExitedAt        *big.Int
	RefundedDeposit *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterTEEExited is a free log retrieval operation binding the contract event 0x25ecc55592b6da6238c7ec72c0e7d31ed5e95088032d55d8fb237805c55c8b41.
//
// Solidity: event TEEExited(bytes32 indexed HardwareId, address indexed owner, uint256 indexed epoch, bytes32 eraseProofHash, uint256 exitedAt, uint256 refundedDeposit)
func (_Contracts *ContractsFilterer) FilterTEEExited(opts *bind.FilterOpts, HardwareId [][32]byte, owner []common.Address, epoch []*big.Int) (*ContractsTEEExitedIterator, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEEExited", HardwareIdRule, ownerRule, epochRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEEExitedIterator{contract: _Contracts.contract, event: "TEEExited", logs: logs, sub: sub}, nil
}

// WatchTEEExited is a free log subscription operation binding the contract event 0x25ecc55592b6da6238c7ec72c0e7d31ed5e95088032d55d8fb237805c55c8b41.
//
// Solidity: event TEEExited(bytes32 indexed HardwareId, address indexed owner, uint256 indexed epoch, bytes32 eraseProofHash, uint256 exitedAt, uint256 refundedDeposit)
func (_Contracts *ContractsFilterer) WatchTEEExited(opts *bind.WatchOpts, sink chan<- *ContractsTEEExited, HardwareId [][32]byte, owner []common.Address, epoch []*big.Int) (event.Subscription, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEEExited", HardwareIdRule, ownerRule, epochRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEEExited)
				if err := _Contracts.contract.UnpackLog(event, "TEEExited", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEEExited is a log parse operation binding the contract event 0x25ecc55592b6da6238c7ec72c0e7d31ed5e95088032d55d8fb237805c55c8b41.
//
// Solidity: event TEEExited(bytes32 indexed HardwareId, address indexed owner, uint256 indexed epoch, bytes32 eraseProofHash, uint256 exitedAt, uint256 refundedDeposit)
func (_Contracts *ContractsFilterer) ParseTEEExited(log types.Log) (*ContractsTEEExited, error) {
	event := new(ContractsTEEExited)
	if err := _Contracts.contract.UnpackLog(event, "TEEExited", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEERegisteredIterator is returned from FilterTEERegistered and is used to iterate over the raw logs and unpacked data for TEERegistered events raised by the Contracts contract.
type ContractsTEERegisteredIterator struct {
	Event *ContractsTEERegistered // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEERegisteredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEERegistered)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEERegistered)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEERegisteredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEERegisteredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEERegistered represents a TEERegistered event raised by the Contracts contract.
type ContractsTEERegistered struct {
	MRENCLAVE     [32]byte
	HardwareId    [32]byte
	SigPubKey     [32]byte
	EnclavePKHash [32]byte
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterTEERegistered is a free log retrieval operation binding the contract event 0xd3cc4b05f4560945fdc6ba29306ed6c17d19e415bd6292bc6551864866b46efa.
//
// Solidity: event TEERegistered(bytes32 MR_ENCLAVE, bytes32 indexed HardwareId, bytes32 indexed sigPubKey, bytes32 enclavePKHash)
func (_Contracts *ContractsFilterer) FilterTEERegistered(opts *bind.FilterOpts, HardwareId [][32]byte, sigPubKey [][32]byte) (*ContractsTEERegisteredIterator, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var sigPubKeyRule []interface{}
	for _, sigPubKeyItem := range sigPubKey {
		sigPubKeyRule = append(sigPubKeyRule, sigPubKeyItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEERegistered", HardwareIdRule, sigPubKeyRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEERegisteredIterator{contract: _Contracts.contract, event: "TEERegistered", logs: logs, sub: sub}, nil
}

// WatchTEERegistered is a free log subscription operation binding the contract event 0xd3cc4b05f4560945fdc6ba29306ed6c17d19e415bd6292bc6551864866b46efa.
//
// Solidity: event TEERegistered(bytes32 MR_ENCLAVE, bytes32 indexed HardwareId, bytes32 indexed sigPubKey, bytes32 enclavePKHash)
func (_Contracts *ContractsFilterer) WatchTEERegistered(opts *bind.WatchOpts, sink chan<- *ContractsTEERegistered, HardwareId [][32]byte, sigPubKey [][32]byte) (event.Subscription, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var sigPubKeyRule []interface{}
	for _, sigPubKeyItem := range sigPubKey {
		sigPubKeyRule = append(sigPubKeyRule, sigPubKeyItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEERegistered", HardwareIdRule, sigPubKeyRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEERegistered)
				if err := _Contracts.contract.UnpackLog(event, "TEERegistered", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEERegistered is a log parse operation binding the contract event 0xd3cc4b05f4560945fdc6ba29306ed6c17d19e415bd6292bc6551864866b46efa.
//
// Solidity: event TEERegistered(bytes32 MR_ENCLAVE, bytes32 indexed HardwareId, bytes32 indexed sigPubKey, bytes32 enclavePKHash)
func (_Contracts *ContractsFilterer) ParseTEERegistered(log types.Log) (*ContractsTEERegistered, error) {
	event := new(ContractsTEERegistered)
	if err := _Contracts.contract.UnpackLog(event, "TEERegistered", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEERegistrationEvidenceRecordedIterator is returned from FilterTEERegistrationEvidenceRecorded and is used to iterate over the raw logs and unpacked data for TEERegistrationEvidenceRecorded events raised by the Contracts contract.
type ContractsTEERegistrationEvidenceRecordedIterator struct {
	Event *ContractsTEERegistrationEvidenceRecorded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEERegistrationEvidenceRecordedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEERegistrationEvidenceRecorded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEERegistrationEvidenceRecorded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEERegistrationEvidenceRecordedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEERegistrationEvidenceRecordedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEERegistrationEvidenceRecorded represents a TEERegistrationEvidenceRecorded event raised by the Contracts contract.
type ContractsTEERegistrationEvidenceRecorded struct {
	HardwareId       [32]byte
	SigPubKey        [32]byte
	VrfPubKey        [32]byte
	EnclavePKHash    [32]byte
	MrSigner         [32]byte
	QuoteHash        [32]byte
	RegisterCertHash [32]byte
	ChallengeHash    [32]byte
	RegisteredAt     *big.Int
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterTEERegistrationEvidenceRecorded is a free log retrieval operation binding the contract event 0xf111be0ac7f1d16057b0eee19d9ebc647d2ad634c119b3dfb8e56be2de1da56d.
//
// Solidity: event TEERegistrationEvidenceRecorded(bytes32 indexed HardwareId, bytes32 sigPubKey, bytes32 vrfPubKey, bytes32 enclavePKHash, bytes32 mrSigner, bytes32 quoteHash, bytes32 registerCertHash, bytes32 challengeHash, uint256 registeredAt)
func (_Contracts *ContractsFilterer) FilterTEERegistrationEvidenceRecorded(opts *bind.FilterOpts, HardwareId [][32]byte) (*ContractsTEERegistrationEvidenceRecordedIterator, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEERegistrationEvidenceRecorded", HardwareIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEERegistrationEvidenceRecordedIterator{contract: _Contracts.contract, event: "TEERegistrationEvidenceRecorded", logs: logs, sub: sub}, nil
}

// WatchTEERegistrationEvidenceRecorded is a free log subscription operation binding the contract event 0xf111be0ac7f1d16057b0eee19d9ebc647d2ad634c119b3dfb8e56be2de1da56d.
//
// Solidity: event TEERegistrationEvidenceRecorded(bytes32 indexed HardwareId, bytes32 sigPubKey, bytes32 vrfPubKey, bytes32 enclavePKHash, bytes32 mrSigner, bytes32 quoteHash, bytes32 registerCertHash, bytes32 challengeHash, uint256 registeredAt)
func (_Contracts *ContractsFilterer) WatchTEERegistrationEvidenceRecorded(opts *bind.WatchOpts, sink chan<- *ContractsTEERegistrationEvidenceRecorded, HardwareId [][32]byte) (event.Subscription, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEERegistrationEvidenceRecorded", HardwareIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEERegistrationEvidenceRecorded)
				if err := _Contracts.contract.UnpackLog(event, "TEERegistrationEvidenceRecorded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEERegistrationEvidenceRecorded is a log parse operation binding the contract event 0xf111be0ac7f1d16057b0eee19d9ebc647d2ad634c119b3dfb8e56be2de1da56d.
//
// Solidity: event TEERegistrationEvidenceRecorded(bytes32 indexed HardwareId, bytes32 sigPubKey, bytes32 vrfPubKey, bytes32 enclavePKHash, bytes32 mrSigner, bytes32 quoteHash, bytes32 registerCertHash, bytes32 challengeHash, uint256 registeredAt)
func (_Contracts *ContractsFilterer) ParseTEERegistrationEvidenceRecorded(log types.Log) (*ContractsTEERegistrationEvidenceRecorded, error) {
	event := new(ContractsTEERegistrationEvidenceRecorded)
	if err := _Contracts.contract.UnpackLog(event, "TEERegistrationEvidenceRecorded", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEERevokedIterator is returned from FilterTEERevoked and is used to iterate over the raw logs and unpacked data for TEERevoked events raised by the Contracts contract.
type ContractsTEERevokedIterator struct {
	Event *ContractsTEERevoked // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEERevokedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEERevoked)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEERevoked)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEERevokedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEERevokedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEERevoked represents a TEERevoked event raised by the Contracts contract.
type ContractsTEERevoked struct {
	HardwareId    [32]byte
	Owner         common.Address
	ReasonHash    [32]byte
	RevokedAt     *big.Int
	SlashedAmount *big.Int
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterTEERevoked is a free log retrieval operation binding the contract event 0xb73664d1974bb21f331e31a47199c4e3a57edd81d26b1b05d3ea4a8429a25d43.
//
// Solidity: event TEERevoked(bytes32 indexed HardwareId, address indexed owner, bytes32 reasonHash, uint256 revokedAt, uint256 slashedAmount)
func (_Contracts *ContractsFilterer) FilterTEERevoked(opts *bind.FilterOpts, HardwareId [][32]byte, owner []common.Address) (*ContractsTEERevokedIterator, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEERevoked", HardwareIdRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEERevokedIterator{contract: _Contracts.contract, event: "TEERevoked", logs: logs, sub: sub}, nil
}

// WatchTEERevoked is a free log subscription operation binding the contract event 0xb73664d1974bb21f331e31a47199c4e3a57edd81d26b1b05d3ea4a8429a25d43.
//
// Solidity: event TEERevoked(bytes32 indexed HardwareId, address indexed owner, bytes32 reasonHash, uint256 revokedAt, uint256 slashedAmount)
func (_Contracts *ContractsFilterer) WatchTEERevoked(opts *bind.WatchOpts, sink chan<- *ContractsTEERevoked, HardwareId [][32]byte, owner []common.Address) (event.Subscription, error) {

	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEERevoked", HardwareIdRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEERevoked)
				if err := _Contracts.contract.UnpackLog(event, "TEERevoked", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEERevoked is a log parse operation binding the contract event 0xb73664d1974bb21f331e31a47199c4e3a57edd81d26b1b05d3ea4a8429a25d43.
//
// Solidity: event TEERevoked(bytes32 indexed HardwareId, address indexed owner, bytes32 reasonHash, uint256 revokedAt, uint256 slashedAmount)
func (_Contracts *ContractsFilterer) ParseTEERevoked(log types.Log) (*ContractsTEERevoked, error) {
	event := new(ContractsTEERevoked)
	if err := _Contracts.contract.UnpackLog(event, "TEERevoked", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEERotationPreparationRequestedIterator is returned from FilterTEERotationPreparationRequested and is used to iterate over the raw logs and unpacked data for TEERotationPreparationRequested events raised by the Contracts contract.
type ContractsTEERotationPreparationRequestedIterator struct {
	Event *ContractsTEERotationPreparationRequested // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEERotationPreparationRequestedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEERotationPreparationRequested)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEERotationPreparationRequested)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEERotationPreparationRequestedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEERotationPreparationRequestedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEERotationPreparationRequested represents a TEERotationPreparationRequested event raised by the Contracts contract.
type ContractsTEERotationPreparationRequested struct {
	CurrentEpoch        *big.Int
	NextEpoch           *big.Int
	DeadlineBlock       *big.Int
	CandidateCount      *big.Int
	FinalityBlockNumber *big.Int
	FinalityBlockHash   [32]byte
	Seed                [32]byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterTEERotationPreparationRequested is a free log retrieval operation binding the contract event 0xe9c66affb346975c2b029e0eb46058348b639e25286df5dc8449372f3bde871b.
//
// Solidity: event TEERotationPreparationRequested(uint256 indexed currentEpoch, uint256 indexed nextEpoch, uint256 deadlineBlock, uint256 candidateCount, uint256 finalityBlockNumber, bytes32 finalityBlockHash, bytes32 seed)
func (_Contracts *ContractsFilterer) FilterTEERotationPreparationRequested(opts *bind.FilterOpts, currentEpoch []*big.Int, nextEpoch []*big.Int) (*ContractsTEERotationPreparationRequestedIterator, error) {

	var currentEpochRule []interface{}
	for _, currentEpochItem := range currentEpoch {
		currentEpochRule = append(currentEpochRule, currentEpochItem)
	}
	var nextEpochRule []interface{}
	for _, nextEpochItem := range nextEpoch {
		nextEpochRule = append(nextEpochRule, nextEpochItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEERotationPreparationRequested", currentEpochRule, nextEpochRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEERotationPreparationRequestedIterator{contract: _Contracts.contract, event: "TEERotationPreparationRequested", logs: logs, sub: sub}, nil
}

// WatchTEERotationPreparationRequested is a free log subscription operation binding the contract event 0xe9c66affb346975c2b029e0eb46058348b639e25286df5dc8449372f3bde871b.
//
// Solidity: event TEERotationPreparationRequested(uint256 indexed currentEpoch, uint256 indexed nextEpoch, uint256 deadlineBlock, uint256 candidateCount, uint256 finalityBlockNumber, bytes32 finalityBlockHash, bytes32 seed)
func (_Contracts *ContractsFilterer) WatchTEERotationPreparationRequested(opts *bind.WatchOpts, sink chan<- *ContractsTEERotationPreparationRequested, currentEpoch []*big.Int, nextEpoch []*big.Int) (event.Subscription, error) {

	var currentEpochRule []interface{}
	for _, currentEpochItem := range currentEpoch {
		currentEpochRule = append(currentEpochRule, currentEpochItem)
	}
	var nextEpochRule []interface{}
	for _, nextEpochItem := range nextEpoch {
		nextEpochRule = append(nextEpochRule, nextEpochItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEERotationPreparationRequested", currentEpochRule, nextEpochRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEERotationPreparationRequested)
				if err := _Contracts.contract.UnpackLog(event, "TEERotationPreparationRequested", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEERotationPreparationRequested is a log parse operation binding the contract event 0xe9c66affb346975c2b029e0eb46058348b639e25286df5dc8449372f3bde871b.
//
// Solidity: event TEERotationPreparationRequested(uint256 indexed currentEpoch, uint256 indexed nextEpoch, uint256 deadlineBlock, uint256 candidateCount, uint256 finalityBlockNumber, bytes32 finalityBlockHash, bytes32 seed)
func (_Contracts *ContractsFilterer) ParseTEERotationPreparationRequested(log types.Log) (*ContractsTEERotationPreparationRequested, error) {
	event := new(ContractsTEERotationPreparationRequested)
	if err := _Contracts.contract.UnpackLog(event, "TEERotationPreparationRequested", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEEStakePendingIterator is returned from FilterTEEStakePending and is used to iterate over the raw logs and unpacked data for TEEStakePending events raised by the Contracts contract.
type ContractsTEEStakePendingIterator struct {
	Event *ContractsTEEStakePending // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEEStakePendingIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEEStakePending)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEEStakePending)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEEStakePendingIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEEStakePendingIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEEStakePending represents a TEEStakePending event raised by the Contracts contract.
type ContractsTEEStakePending struct {
	Owner         common.Address
	PendingNonce  *big.Int
	Deposit       *big.Int
	ChallengeHash [32]byte
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterTEEStakePending is a free log retrieval operation binding the contract event 0xad63e94b43db4b75a69838f521c653d3d7fbd9dcd64828a05c37fd3b96c5edaa.
//
// Solidity: event TEEStakePending(address indexed owner, uint256 indexed pendingNonce, uint256 deposit, bytes32 challengeHash)
func (_Contracts *ContractsFilterer) FilterTEEStakePending(opts *bind.FilterOpts, owner []common.Address, pendingNonce []*big.Int) (*ContractsTEEStakePendingIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var pendingNonceRule []interface{}
	for _, pendingNonceItem := range pendingNonce {
		pendingNonceRule = append(pendingNonceRule, pendingNonceItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEEStakePending", ownerRule, pendingNonceRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEEStakePendingIterator{contract: _Contracts.contract, event: "TEEStakePending", logs: logs, sub: sub}, nil
}

// WatchTEEStakePending is a free log subscription operation binding the contract event 0xad63e94b43db4b75a69838f521c653d3d7fbd9dcd64828a05c37fd3b96c5edaa.
//
// Solidity: event TEEStakePending(address indexed owner, uint256 indexed pendingNonce, uint256 deposit, bytes32 challengeHash)
func (_Contracts *ContractsFilterer) WatchTEEStakePending(opts *bind.WatchOpts, sink chan<- *ContractsTEEStakePending, owner []common.Address, pendingNonce []*big.Int) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var pendingNonceRule []interface{}
	for _, pendingNonceItem := range pendingNonce {
		pendingNonceRule = append(pendingNonceRule, pendingNonceItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEEStakePending", ownerRule, pendingNonceRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEEStakePending)
				if err := _Contracts.contract.UnpackLog(event, "TEEStakePending", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEEStakePending is a log parse operation binding the contract event 0xad63e94b43db4b75a69838f521c653d3d7fbd9dcd64828a05c37fd3b96c5edaa.
//
// Solidity: event TEEStakePending(address indexed owner, uint256 indexed pendingNonce, uint256 deposit, bytes32 challengeHash)
func (_Contracts *ContractsFilterer) ParseTEEStakePending(log types.Log) (*ContractsTEEStakePending, error) {
	event := new(ContractsTEEStakePending)
	if err := _Contracts.contract.UnpackLog(event, "TEEStakePending", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTEEVRFSubmittedIterator is returned from FilterTEEVRFSubmitted and is used to iterate over the raw logs and unpacked data for TEEVRFSubmitted events raised by the Contracts contract.
type ContractsTEEVRFSubmittedIterator struct {
	Event *ContractsTEEVRFSubmitted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTEEVRFSubmittedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTEEVRFSubmitted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTEEVRFSubmitted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTEEVRFSubmittedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTEEVRFSubmittedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTEEVRFSubmitted represents a TEEVRFSubmitted event raised by the Contracts contract.
type ContractsTEEVRFSubmitted struct {
	Epoch            *big.Int
	HardwareId       [32]byte
	VrfPubKey        [32]byte
	VrfResult        [32]byte
	ProofHash        [32]byte
	SubmittedAtBlock *big.Int
	SubmittedAt      *big.Int
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterTEEVRFSubmitted is a free log retrieval operation binding the contract event 0x5a72afe2e027e5572d382952b5dd59e74de4ddf4c7e9a608feaa0c8c8c3601ed.
//
// Solidity: event TEEVRFSubmitted(uint256 indexed epoch, bytes32 indexed HardwareId, bytes32 vrfPubKey, bytes32 vrfResult, bytes32 proofHash, uint256 submittedAtBlock, uint256 submittedAt)
func (_Contracts *ContractsFilterer) FilterTEEVRFSubmitted(opts *bind.FilterOpts, epoch []*big.Int, HardwareId [][32]byte) (*ContractsTEEVRFSubmittedIterator, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TEEVRFSubmitted", epochRule, HardwareIdRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTEEVRFSubmittedIterator{contract: _Contracts.contract, event: "TEEVRFSubmitted", logs: logs, sub: sub}, nil
}

// WatchTEEVRFSubmitted is a free log subscription operation binding the contract event 0x5a72afe2e027e5572d382952b5dd59e74de4ddf4c7e9a608feaa0c8c8c3601ed.
//
// Solidity: event TEEVRFSubmitted(uint256 indexed epoch, bytes32 indexed HardwareId, bytes32 vrfPubKey, bytes32 vrfResult, bytes32 proofHash, uint256 submittedAtBlock, uint256 submittedAt)
func (_Contracts *ContractsFilterer) WatchTEEVRFSubmitted(opts *bind.WatchOpts, sink chan<- *ContractsTEEVRFSubmitted, epoch []*big.Int, HardwareId [][32]byte) (event.Subscription, error) {

	var epochRule []interface{}
	for _, epochItem := range epoch {
		epochRule = append(epochRule, epochItem)
	}
	var HardwareIdRule []interface{}
	for _, HardwareIdItem := range HardwareId {
		HardwareIdRule = append(HardwareIdRule, HardwareIdItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TEEVRFSubmitted", epochRule, HardwareIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTEEVRFSubmitted)
				if err := _Contracts.contract.UnpackLog(event, "TEEVRFSubmitted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTEEVRFSubmitted is a log parse operation binding the contract event 0x5a72afe2e027e5572d382952b5dd59e74de4ddf4c7e9a608feaa0c8c8c3601ed.
//
// Solidity: event TEEVRFSubmitted(uint256 indexed epoch, bytes32 indexed HardwareId, bytes32 vrfPubKey, bytes32 vrfResult, bytes32 proofHash, uint256 submittedAtBlock, uint256 submittedAt)
func (_Contracts *ContractsFilterer) ParseTEEVRFSubmitted(log types.Log) (*ContractsTEEVRFSubmitted, error) {
	event := new(ContractsTEEVRFSubmitted)
	if err := _Contracts.contract.UnpackLog(event, "TEEVRFSubmitted", log); err != nil {
		return nil, err
	}
	return event, nil
}

// ContractsTrustedMREnclaveSetIterator is returned from FilterTrustedMREnclaveSet and is used to iterate over the raw logs and unpacked data for TrustedMREnclaveSet events raised by the Contracts contract.
type ContractsTrustedMREnclaveSetIterator struct {
	Event *ContractsTrustedMREnclaveSet // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ContractsTrustedMREnclaveSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ContractsTrustedMREnclaveSet)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ContractsTrustedMREnclaveSet)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ContractsTrustedMREnclaveSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ContractsTrustedMREnclaveSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ContractsTrustedMREnclaveSet represents a TrustedMREnclaveSet event raised by the Contracts contract.
type ContractsTrustedMREnclaveSet struct {
	MrEnclave [32]byte
	Trusted   bool
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterTrustedMREnclaveSet is a free log retrieval operation binding the contract event 0x1b9179620a1bcffa69dfd36d35e5064af9eb1c5e8a1af027744c43b9f994d6b4.
//
// Solidity: event TrustedMREnclaveSet(bytes32 indexed mrEnclave, bool trusted)
func (_Contracts *ContractsFilterer) FilterTrustedMREnclaveSet(opts *bind.FilterOpts, mrEnclave [][32]byte) (*ContractsTrustedMREnclaveSetIterator, error) {

	var mrEnclaveRule []interface{}
	for _, mrEnclaveItem := range mrEnclave {
		mrEnclaveRule = append(mrEnclaveRule, mrEnclaveItem)
	}

	logs, sub, err := _Contracts.contract.FilterLogs(opts, "TrustedMREnclaveSet", mrEnclaveRule)
	if err != nil {
		return nil, err
	}
	return &ContractsTrustedMREnclaveSetIterator{contract: _Contracts.contract, event: "TrustedMREnclaveSet", logs: logs, sub: sub}, nil
}

// WatchTrustedMREnclaveSet is a free log subscription operation binding the contract event 0x1b9179620a1bcffa69dfd36d35e5064af9eb1c5e8a1af027744c43b9f994d6b4.
//
// Solidity: event TrustedMREnclaveSet(bytes32 indexed mrEnclave, bool trusted)
func (_Contracts *ContractsFilterer) WatchTrustedMREnclaveSet(opts *bind.WatchOpts, sink chan<- *ContractsTrustedMREnclaveSet, mrEnclave [][32]byte) (event.Subscription, error) {

	var mrEnclaveRule []interface{}
	for _, mrEnclaveItem := range mrEnclave {
		mrEnclaveRule = append(mrEnclaveRule, mrEnclaveItem)
	}

	logs, sub, err := _Contracts.contract.WatchLogs(opts, "TrustedMREnclaveSet", mrEnclaveRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ContractsTrustedMREnclaveSet)
				if err := _Contracts.contract.UnpackLog(event, "TrustedMREnclaveSet", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTrustedMREnclaveSet is a log parse operation binding the contract event 0x1b9179620a1bcffa69dfd36d35e5064af9eb1c5e8a1af027744c43b9f994d6b4.
//
// Solidity: event TrustedMREnclaveSet(bytes32 indexed mrEnclave, bool trusted)
func (_Contracts *ContractsFilterer) ParseTrustedMREnclaveSet(log types.Log) (*ContractsTrustedMREnclaveSet, error) {
	event := new(ContractsTrustedMREnclaveSet)
	if err := _Contracts.contract.UnpackLog(event, "TrustedMREnclaveSet", log); err != nil {
		return nil, err
	}
	return event, nil
}
