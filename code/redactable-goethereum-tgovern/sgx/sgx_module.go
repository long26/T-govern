package sgx

/*
#cgo !sgx_hw pkg-config: libsgx_urts_sim
#cgo sgx_hw pkg-config: libsgx_urts
#cgo CFLAGS:   -I${SRCDIR}/app -I${SRCDIR}/enclave
#cgo CXXFLAGS: -std=c++11
#cgo CXXFLAGS: -I${SRCDIR}/app -I${SRCDIR}/enclave
#cgo LDFLAGS:  -L${SRCDIR}/. -Wl,-rpath,$ORIGIN/../sgx -Wl,-rpath,$ORIGIN/../../sgx -Wl,-rpath,/opt/intel/sgxsdk/lib64 -lsgx_module -lstdc++ -lgmp -L${SRCDIR}/lib/miracl -lcore
// SIM builds do not link libsgx_dcap_ql, so sgx_qe_* (DCAP quote3, only used by
// generate_quote in HW mode) stay undefined in libsgx_module.so. Allow the
// executable link to tolerate that — they are never called at runtime in SIM
// (the test harness uses mock quotes).
#cgo !sgx_hw LDFLAGS: -Wl,--unresolved-symbols=ignore-in-shared-libs
#include "tee.h"
#include <stdlib.h>
#include <string.h>

#if SGX_DEAL_MAX_A_BUF != 4096 || SGX_DEAL_MAX_B0_BUF != 256 || SGX_DEAL_MAX_SHARES_BUF != 8192 || SGX_MAX_G1_BUF != 256 || SGX_MAX_G2_BUF != 256 || SGX_MAX_P_HEX_BUF != 128 || SGX_MAX_SCALAR_BUF != 64
#error "sgx_module.go buffer constants must match sgx/app/tee.h"
#endif
*/
import "C"
import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"os"
	"unsafe"

	bls12381 "github.com/kilic/bls12-381"
)

const (
	sgxDealMaxABuf      = 4096
	sgxDealMaxB0Buf     = 256
	sgxDealMaxSharesBuf = 8192
	sgxMaxG1Buf         = 256
	sgxMaxG2Buf         = 256
	sgxMaxPHexBuf       = 128
	sgxMaxScalarBuf     = 64
)

type SGXDealResult struct {
	CommitmentsA     []byte
	CommitmentB0     []byte
	SharesSerialized []byte
	G1Bytes          []byte
	G2Bytes          []byte
	PHex             string
}

type SGXExtractResult struct {
	GlobalPK        []byte
	GlobalPKG1      []byte
	VerificationKey []byte
	PkCHSignature   []byte
}

type SGXPartialAdaptResult struct {
	Contribution   []byte
	ProofChallenge []byte
	ProofResponse  []byte
	Y              []byte
	Sigma          []byte
}

type PkCHSigningInput struct {
	ChainID         []byte
	ContractAddress []byte
	TEEOwner        []byte
}

// TEEBenchmarkOperation identifies the algorithmic regions instrumented in
// enclave.cpp for Table 6.2. Calls are aggregate since the last reset.
type TEEBenchmarkOperation uint32

const (
	TEEBenchmarkDKGLocal TEEBenchmarkOperation = iota
	TEEBenchmarkAuthorizationCertVerify
	TEEBenchmarkPCollide
	TEEBenchmarkSignature
	TEEBenchmarkVRFKeyGen
	TEEBenchmarkVRFComp
	TEEBenchmarkShareErase
)

type TEEBenchmarkStats struct {
	Calls       uint64
	TotalCycles uint64
	MinCycles   uint64
	MaxCycles   uint64
}

func Init() error {
	if C.tee_init() != 0 {
		return fmt.Errorf("sgx init fail")
	}
	return nil
}

func Close() { C.tee_close() }

// BenchmarkNoop performs an empty ECALL. Time it from Go's testing.B or a
// caller-side monotonic clock to obtain the Table 6.2 ECALL boundary cost.
func BenchmarkNoop() error {
	if C.tee_benchmark_noop() != 0 {
		return errors.New("tee_benchmark_noop failed")
	}
	return nil
}

func ResetBenchmarkStats() error {
	if C.tee_benchmark_reset_stats() != 0 {
		return errors.New("tee_benchmark_reset_stats failed")
	}
	return nil
}

func GetBenchmarkStats(operation TEEBenchmarkOperation) (TEEBenchmarkStats, error) {
	var calls, total, min, max C.uint64_t
	if C.tee_benchmark_get_stats(C.uint32_t(operation), &calls, &total, &min, &max) != 0 {
		return TEEBenchmarkStats{}, fmt.Errorf("tee_benchmark_get_stats(%d) failed", operation)
	}
	return TEEBenchmarkStats{
		Calls:       uint64(calls),
		TotalCycles: uint64(total),
		MinCycles:   uint64(min),
		MaxCycles:   uint64(max),
	}, nil
}

var sgxStatusNames = map[uint32]string{
	0x0000: "SUCCESS",
	0x0001: "UNEXPECTED",
	0x0002: "INVALID_PARAMETER",
	0x0003: "OUT_OF_MEMORY",
	0x0004: "ENCLAVE_LOST",
	0x0005: "INVALID_STATE",
	0x0008: "FEATURE_NOT_SUPPORTED",
	0x0009: "PTHREAD_EXIT",
	0x000a: "MEMORY_MAP_FAILURE",
	0x1001: "INVALID_FUNCTION",
	0x1003: "OUT_OF_TCS",
	0x1006: "ENCLAVE_CRASHED",
	0x1007: "ECALL_NOT_ALLOWED",
	0x1008: "OCALL_NOT_ALLOWED",
	0x1009: "STACK_OVERRUN",
	0x2000: "UNDEFINED_SYMBOL",
	0x2001: "INVALID_ENCLAVE",
	0x2002: "INVALID_ENCLAVE_ID",
	0x2003: "INVALID_SIGNATURE",
	0x2004: "NDEBUG_ENCLAVE",
	0x2005: "OUT_OF_EPC",
	0x2006: "NO_DEVICE",
	0x2007: "MEMORY_MAP_CONFLICT",
	0x2009: "INVALID_METADATA",
	0x200c: "DEVICE_BUSY",
	0x200d: "INVALID_VERSION",
	0x200e: "MODE_INCOMPATIBLE",
	0x200f: "ENCLAVE_FILE_ACCESS",
	0x2010: "INVALID_MISC",
	0x3001: "MAC_MISMATCH",
	0x3002: "INVALID_ATTRIBUTE",
	0x3003: "INVALID_CPUSVN",
	0x3004: "INVALID_ISVSVN",
	0x3005: "INVALID_KEYNAME",
	0x3006: "UNSUPPORTED_FUNCTION",
	0x4001: "SERVICE_UNAVAILABLE",
	0x4002: "SERVICE_TIMEOUT",
	0x4004: "SERVICE_INVALID_PRIVILEGE",
	0x4006: "UPDATE_NEEDED",
	0x4007: "NETWORK_FAILURE",
	0x400a: "BUSY",
	0x5002: "NO_PRIVILEGE",
}

func formatSGXStatus(code C.uint32_t) string {
	value := uint32(code)
	if name, ok := sgxStatusNames[value]; ok {
		return fmt.Sprintf("0x%x(=%s)", value, name)
	}
	return fmt.Sprintf("0x%x(=UNKNOWN)", value)
}

func teeBridgeError(op string, ret C.int, st, retval C.uint32_t) error {
	return fmt.Errorf("%s failed: st=%s retval=%s bridge_ret=%d", op, formatSGXStatus(st), formatSGXStatus(retval), int(ret))
}

func GenerateQuote() (quoteData, hardwareID, committeePubkey, sigPubkey, vrfPubkey, enclavePK []byte, err error) {
	hardwareID = make([]byte, 32)
	committeePubkey = make([]byte, 128)
	sigPubkey = make([]byte, 32)
	vrfPubkey = make([]byte, 32)
	hardwareIDPtr := (*C.uint8_t)(unsafe.Pointer(&hardwareID[0]))
	committeePubkeyPtr := (*C.uint8_t)(unsafe.Pointer(&committeePubkey[0]))
	sigPubkeyPtr := (*C.uint8_t)(unsafe.Pointer(&sigPubkey[0]))
	vrfPubkeyPtr := (*C.uint8_t)(unsafe.Pointer(&vrfPubkey[0]))
	if C.tee_generate_quote(hardwareIDPtr, committeePubkeyPtr, sigPubkeyPtr, vrfPubkeyPtr) != 0 {
		return nil, nil, nil, nil, nil, nil, errors.New("tee_generate_quote failed")
	}
	enclavePK, err = GetEnclavePK()
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	quoteData, err = os.ReadFile("quote.dat")
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to read generated quote: %w", err)
	}
	return quoteData, hardwareID, committeePubkey, sigPubkey, vrfPubkey, enclavePK, nil
}

func GetIdentity() (hardwareID, committeePubkey, sigPubkey, vrfPubkey, enclavePK []byte, err error) {
	hardwareID = make([]byte, 32)
	committeePubkey = make([]byte, 128)
	sigPubkey = make([]byte, 32)
	vrfPubkey = make([]byte, 32)
	if C.tee_get_identity(
		(*C.uint8_t)(unsafe.Pointer(&hardwareID[0])),
		(*C.uint8_t)(unsafe.Pointer(&committeePubkey[0])),
		(*C.uint8_t)(unsafe.Pointer(&sigPubkey[0])),
		(*C.uint8_t)(unsafe.Pointer(&vrfPubkey[0])),
	) != 0 {
		return nil, nil, nil, nil, nil, errors.New("tee_get_identity failed")
	}
	enclavePK, err = GetEnclavePK()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return hardwareID, committeePubkey, sigPubkey, vrfPubkey, enclavePK, nil
}

func GetEnclaveState() (hasKey bool, index int, err error) {
	var hasKeyC C.int
	var indexC C.int
	if C.tee_get_enclave_state(&hasKeyC, &indexC) != 0 {
		return false, 0, errors.New("tee_get_enclave_state failed")
	}
	return hasKeyC != 0, int(indexC), nil
}

func GetEnclavePK() ([]byte, error) {
	pkBuf := make([]byte, 64)
	var outSize C.uint32_t
	ret := C.tee_get_enclave_pk(
		(*C.uint8_t)(unsafe.Pointer(&pkBuf[0])),
		C.uint32_t(len(pkBuf)),
		&outSize,
	)
	if ret != 0 {
		return nil, errors.New("tee_get_enclave_pk failed")
	}
	return pkBuf[:outSize], nil
}

// SetCommitteePubKey 设置委员会聚合公钥 g_vk_gc，用于证书签名验证
func SetCommitteePubKey(pubKey []byte) error {
	if len(pubKey) == 0 {
		return errors.New("pubKey must not be empty")
	}
	ret := C.tee_set_committee_pubkey(
		(*C.uint8_t)(unsafe.Pointer(&pubKey[0])),
		C.uint32_t(len(pubKey)),
	)
	if ret != 0 {
		return errors.New("tee_set_committee_pubkey failed")
	}
	return nil
}

func RegisterPeerEPK(peerIndex uint64, peerPK []byte) error {
	if len(peerPK) != 64 {
		return errors.New("peerPK must be 64 bytes")
	}
	ret := C.tee_register_peer_epk(
		C.int(peerIndex),
		(*C.uint8_t)(unsafe.Pointer(&peerPK[0])),
		C.uint32_t(len(peerPK)),
	)
	if ret != 0 {
		return errors.New("tee_register_peer_epk failed")
	}
	return nil
}

func DistKGCreateDeal(dealerIndex, threshold, numParties int, certEAMsg, certEASig, activeTEEList, activeSetCertInput []byte, activeTEECount, activeSetCertInputLen uint32) (*SGXDealResult, error) {
	commitmentsABuf := make([]byte, sgxDealMaxABuf)
	b0Buf := make([]byte, sgxDealMaxB0Buf)
	sharesBuf := make([]byte, sgxDealMaxSharesBuf)
	g1Buf := make([]byte, sgxMaxG1Buf)
	g2Buf := make([]byte, sgxMaxG2Buf)
	pHexBuf := make([]byte, sgxMaxPHexBuf)

	var aOutSize, b0OutSize, sharesOutSize C.uint32_t

	var certEAMsgPtr, certEASigPtr *C.uint8_t
	if len(certEAMsg) > 0 {
		certEAMsgPtr = (*C.uint8_t)(unsafe.Pointer(&certEAMsg[0]))
	}
	if len(certEASig) > 0 {
		certEASigPtr = (*C.uint8_t)(unsafe.Pointer(&certEASig[0]))
	}

	var activeTEEListPtr *C.uint8_t
	if len(activeTEEList) > 0 {
		activeTEEListPtr = (*C.uint8_t)(unsafe.Pointer(&activeTEEList[0]))
	}

	var activeSetCertInputPtr *C.uint8_t
	if len(activeSetCertInput) > 0 {
		activeSetCertInputPtr = (*C.uint8_t)(unsafe.Pointer(&activeSetCertInput[0]))
	}

	ret := C.tee_distkg_create_deal(
		C.int(dealerIndex),
		C.int(threshold),
		C.int(numParties),
		(*C.uint8_t)(unsafe.Pointer(&commitmentsABuf[0])),
		C.uint32_t(len(commitmentsABuf)),
		&aOutSize,
		(*C.uint8_t)(unsafe.Pointer(&b0Buf[0])),
		C.uint32_t(len(b0Buf)),
		&b0OutSize,
		(*C.uint8_t)(unsafe.Pointer(&sharesBuf[0])),
		C.uint32_t(len(sharesBuf)),
		&sharesOutSize,
		(*C.uint8_t)(unsafe.Pointer(&g1Buf[0])),
		C.uint32_t(len(g1Buf)),
		(*C.uint8_t)(unsafe.Pointer(&g2Buf[0])),
		C.uint32_t(len(g2Buf)),
		(*C.char)(unsafe.Pointer(&pHexBuf[0])),
		C.uint32_t(len(pHexBuf)),
		certEAMsgPtr, C.uint32_t(len(certEAMsg)),
		certEASigPtr, C.uint32_t(len(certEASig)),
		activeTEEListPtr, C.uint32_t(activeTEECount),
		activeSetCertInputPtr, C.uint32_t(activeSetCertInputLen),
	)
	if ret != 0 {
		return nil, fmt.Errorf("tee_distkg_create_deal failed: %d", ret)
	}

	pHexStr := C.GoString((*C.char)(unsafe.Pointer(&pHexBuf[0])))

	return &SGXDealResult{
		CommitmentsA:     commitmentsABuf[:aOutSize],
		CommitmentB0:     b0Buf[:b0OutSize],
		SharesSerialized: sharesBuf[:sharesOutSize],
		G1Bytes:          g1Buf[:96],
		G2Bytes:          g2Buf[:192],
		PHex:             pHexStr,
	}, nil
}

func ParseSGXShares(data []byte, numParties int) []*big.Int {
	shares := make([]*big.Int, 0, numParties)
	offset := 0
	for i := 0; i < numParties && offset+4 <= len(data); i++ {
		sz := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4
		if offset+int(sz) > len(data) {
			break
		}
		shareData := data[offset : offset+int(sz)]
		if len(shareData) > 0 && shareData[0] == 0x00 {
			shares = append(shares, new(big.Int).SetBytes(shareData[1:]))
		} else if len(shareData) > 0 && shareData[0] == 0x01 {
			shares = append(shares, nil)
		} else {
			shares = append(shares, new(big.Int).SetBytes(shareData))
		}
		offset += int(sz)
	}
	return shares
}

func DistKGVerifyShare(receiverIndex, dealerIndex, threshold, numParties int,
	commitmentsA, commitmentB0, encryptedShareBytes []byte, certEAMsg, certEASig, activeTEEList, activeSetCertInput []byte, activeTEECount, activeSetCertInputLen uint32) (bool, error) {

	shareData := encryptedShareBytes

	var certEAMsgPtr, certEASigPtr *C.uint8_t
	if len(certEAMsg) > 0 {
		certEAMsgPtr = (*C.uint8_t)(unsafe.Pointer(&certEAMsg[0]))
	}
	if len(certEASig) > 0 {
		certEASigPtr = (*C.uint8_t)(unsafe.Pointer(&certEASig[0]))
	}

	var activeTEEListPtr *C.uint8_t
	if len(activeTEEList) > 0 {
		activeTEEListPtr = (*C.uint8_t)(unsafe.Pointer(&activeTEEList[0]))
	}

	var activeSetCertInputPtr *C.uint8_t
	if len(activeSetCertInput) > 0 {
		activeSetCertInputPtr = (*C.uint8_t)(unsafe.Pointer(&activeSetCertInput[0]))
	}

	var valid C.int
	ret := C.tee_distkg_verify_share(
		C.int(receiverIndex),
		C.int(dealerIndex),
		C.int(threshold),
		C.int(numParties),
		(*C.uint8_t)(unsafe.Pointer(&commitmentsA[0])),
		C.uint32_t(len(commitmentsA)),
		(*C.uint8_t)(unsafe.Pointer(&commitmentB0[0])),
		C.uint32_t(len(commitmentB0)),
		(*C.uint8_t)(unsafe.Pointer(&shareData[0])),
		C.uint32_t(len(shareData)),
		&valid,
		certEAMsgPtr, C.uint32_t(len(certEAMsg)),
		certEASigPtr, C.uint32_t(len(certEASig)),
		activeTEEListPtr, C.uint32_t(activeTEECount),
		activeSetCertInputPtr, C.uint32_t(activeSetCertInputLen),
	)
	if ret != 0 {
		return false, fmt.Errorf("tee_distkg_verify_share failed: %d", ret)
	}
	return valid != 0, nil
}

func DistKGExtract(myIndex, threshold, numParties int,
	qualIndices []int32,
	b0Data []byte, b0DealerMap []int32,
	a0Data []byte, a0DealerMap []int32,
	encryptedSharesData []byte, sharesFromMap []int32,
	certEAMsg, certEASig, activeTEEList, activeSetCertInput []byte, activeTEECount, activeSetCertInputLen uint32,
	pkCHSigningInput ...PkCHSigningInput) (*SGXExtractResult, error) {

	sharesData := encryptedSharesData
	if len(sharesFromMap) == 0 && len(encryptedSharesData) > 0 {
		scalarSize := 48
		numQual := len(qualIndices)
		if len(encryptedSharesData) == numQual*scalarSize {
			buf := make([]byte, 0, numQual*(4+1+scalarSize))
			for i := 0; i < numQual; i++ {
				entrySize := uint32(1 + scalarSize)
				szBuf := make([]byte, 4)
				binary.LittleEndian.PutUint32(szBuf, entrySize)
				buf = append(buf, szBuf...)
				buf = append(buf, 0x00)
				buf = append(buf, encryptedSharesData[i*scalarSize:(i+1)*scalarSize]...)
			}
			sharesData = buf
			sharesFromMap = qualIndices
		}
	}

	pkBuf := make([]byte, sgxMaxG2Buf)
	pkG1Buf := make([]byte, sgxMaxG1Buf)
	vkBuf := make([]byte, sgxMaxG1Buf)
	pkSigBuf := make([]byte, 65)
	var pkOutSize, pkG1OutSize, vkOutSize, pkSigOutSize C.uint32_t

	var qualPtr *C.int32_t
	if len(qualIndices) > 0 {
		qualPtr = (*C.int32_t)(unsafe.Pointer(&qualIndices[0]))
	}
	var b0MapPtr, a0MapPtr, sharesMapPtr *C.int32_t
	if len(b0DealerMap) > 0 {
		b0MapPtr = (*C.int32_t)(unsafe.Pointer(&b0DealerMap[0]))
	}
	if len(a0DealerMap) > 0 {
		a0MapPtr = (*C.int32_t)(unsafe.Pointer(&a0DealerMap[0]))
	}
	if len(sharesFromMap) > 0 {
		sharesMapPtr = (*C.int32_t)(unsafe.Pointer(&sharesFromMap[0]))
	}

	var b0Ptr, a0Ptr, sharesPtr *C.uint8_t
	if len(b0Data) > 0 {
		b0Ptr = (*C.uint8_t)(unsafe.Pointer(&b0Data[0]))
	}
	if len(a0Data) > 0 {
		a0Ptr = (*C.uint8_t)(unsafe.Pointer(&a0Data[0]))
	}
	if len(sharesData) > 0 {
		sharesPtr = (*C.uint8_t)(unsafe.Pointer(&sharesData[0]))
	}

	var certEAMsgPtr, certEASigPtr *C.uint8_t
	if len(certEAMsg) > 0 {
		certEAMsgPtr = (*C.uint8_t)(unsafe.Pointer(&certEAMsg[0]))
	}
	if len(certEASig) > 0 {
		certEASigPtr = (*C.uint8_t)(unsafe.Pointer(&certEASig[0]))
	}

	var activeTEEListPtr *C.uint8_t
	if len(activeTEEList) > 0 {
		activeTEEListPtr = (*C.uint8_t)(unsafe.Pointer(&activeTEEList[0]))
	}

	var activeSetCertInputPtr *C.uint8_t
	if len(activeSetCertInput) > 0 {
		activeSetCertInputPtr = (*C.uint8_t)(unsafe.Pointer(&activeSetCertInput[0]))
	}

	var pkCHChainIDPtr, pkCHContractAddressPtr, pkCHTEEOwnerPtr, pkCHSignaturePtr *C.uint8_t
	var pkCHChainIDLen, pkCHContractAddressLen, pkCHTEEOwnerLen, pkCHSignatureBufSize C.uint32_t
	var pkCHSignatureOutSizePtr *C.uint32_t
	if len(pkCHSigningInput) > 0 {
		input := pkCHSigningInput[0]
		if len(input.ChainID) != 32 {
			return nil, fmt.Errorf("pkCH chainID must be 32 bytes")
		}
		if len(input.ContractAddress) != 32 {
			return nil, fmt.Errorf("pkCH contractAddress must be 32 bytes")
		}
		if len(input.TEEOwner) != 32 {
			return nil, fmt.Errorf("pkCH teeOwner must be 32 bytes")
		}
		pkCHChainIDPtr = (*C.uint8_t)(unsafe.Pointer(&input.ChainID[0]))
		pkCHChainIDLen = C.uint32_t(len(input.ChainID))
		pkCHContractAddressPtr = (*C.uint8_t)(unsafe.Pointer(&input.ContractAddress[0]))
		pkCHContractAddressLen = C.uint32_t(len(input.ContractAddress))
		pkCHTEEOwnerPtr = (*C.uint8_t)(unsafe.Pointer(&input.TEEOwner[0]))
		pkCHTEEOwnerLen = C.uint32_t(len(input.TEEOwner))
		pkCHSignaturePtr = (*C.uint8_t)(unsafe.Pointer(&pkSigBuf[0]))
		pkCHSignatureBufSize = C.uint32_t(len(pkSigBuf))
		pkCHSignatureOutSizePtr = &pkSigOutSize
	}

	ret := C.tee_distkg_extract(
		C.int(myIndex),
		C.int(threshold),
		C.int(numParties),
		qualPtr, C.uint32_t(len(qualIndices)),
		b0Ptr, C.uint32_t(len(b0Data)),
		b0MapPtr, C.uint32_t(len(b0DealerMap)),
		a0Ptr, C.uint32_t(len(a0Data)),
		a0MapPtr, C.uint32_t(len(a0DealerMap)),
		sharesPtr, C.uint32_t(len(sharesData)),
		sharesMapPtr, C.uint32_t(len(sharesFromMap)),
		(*C.uint8_t)(unsafe.Pointer(&pkBuf[0])),
		C.uint32_t(len(pkBuf)),
		&pkOutSize,
		(*C.uint8_t)(unsafe.Pointer(&pkG1Buf[0])),
		C.uint32_t(len(pkG1Buf)),
		&pkG1OutSize,
		(*C.uint8_t)(unsafe.Pointer(&vkBuf[0])),
		C.uint32_t(len(vkBuf)),
		&vkOutSize,
		pkCHChainIDPtr, pkCHChainIDLen,
		pkCHContractAddressPtr, pkCHContractAddressLen,
		pkCHTEEOwnerPtr, pkCHTEEOwnerLen,
		pkCHSignaturePtr, pkCHSignatureBufSize, pkCHSignatureOutSizePtr,
		certEAMsgPtr, C.uint32_t(len(certEAMsg)),
		certEASigPtr, C.uint32_t(len(certEASig)),
		activeTEEListPtr, C.uint32_t(activeTEECount),
		activeSetCertInputPtr, C.uint32_t(activeSetCertInputLen),
	)
	if ret != 0 {
		return nil, fmt.Errorf("tee_distkg_extract failed: %d", ret)
	}
	var pkCHSignature []byte
	if pkSigOutSize > 0 {
		if pkSigOutSize != 65 {
			return nil, fmt.Errorf("unexpected pkCH signature length: got %d, want 65", pkSigOutSize)
		}
		pkCHSignature = append([]byte(nil), pkSigBuf[:pkSigOutSize]...)
	}

	return &SGXExtractResult{
		GlobalPK:        pkBuf[:pkOutSize],
		GlobalPKG1:      pkG1Buf[:pkG1OutSize],
		VerificationKey: vkBuf[:vkOutSize],
		PkCHSignature:   pkCHSignature,
	}, nil
}

func DKRSCreateDeal(dealerIndex, newThreshold, newNumParties int, certEAMsg, certEASig, activeTEEList, activeSetCertInput []byte, activeTEECount, activeSetCertInputLen uint32) (*SGXDealResult, error) {
	commitmentsABuf := make([]byte, sgxDealMaxABuf)
	b0Buf := make([]byte, sgxDealMaxB0Buf)
	sharesBuf := make([]byte, sgxDealMaxSharesBuf)
	g1Buf := make([]byte, sgxMaxG1Buf)
	g2Buf := make([]byte, sgxMaxG2Buf)
	pHexBuf := make([]byte, sgxMaxPHexBuf)

	var aOutSize, b0OutSize, sharesOutSize C.uint32_t

	var certEAMsgPtr, certEASigPtr *C.uint8_t
	if len(certEAMsg) > 0 {
		certEAMsgPtr = (*C.uint8_t)(unsafe.Pointer(&certEAMsg[0]))
	}
	if len(certEASig) > 0 {
		certEASigPtr = (*C.uint8_t)(unsafe.Pointer(&certEASig[0]))
	}

	var activeTEEListPtr *C.uint8_t
	if len(activeTEEList) > 0 {
		activeTEEListPtr = (*C.uint8_t)(unsafe.Pointer(&activeTEEList[0]))
	}

	var activeSetCertInputPtr *C.uint8_t
	if len(activeSetCertInput) > 0 {
		activeSetCertInputPtr = (*C.uint8_t)(unsafe.Pointer(&activeSetCertInput[0]))
	}

	var bridgeSt, bridgeRetval C.uint32_t
	ret := C.tee_dkrs_create_deal(
		C.int(dealerIndex),
		C.int(newThreshold),
		C.int(newNumParties),
		(*C.uint8_t)(unsafe.Pointer(&commitmentsABuf[0])),
		C.uint32_t(len(commitmentsABuf)),
		&aOutSize,
		(*C.uint8_t)(unsafe.Pointer(&b0Buf[0])),
		C.uint32_t(len(b0Buf)),
		&b0OutSize,
		(*C.uint8_t)(unsafe.Pointer(&sharesBuf[0])),
		C.uint32_t(len(sharesBuf)),
		&sharesOutSize,
		(*C.uint8_t)(unsafe.Pointer(&g1Buf[0])),
		C.uint32_t(len(g1Buf)),
		(*C.uint8_t)(unsafe.Pointer(&g2Buf[0])),
		C.uint32_t(len(g2Buf)),
		(*C.char)(unsafe.Pointer(&pHexBuf[0])),
		C.uint32_t(len(pHexBuf)),
		certEAMsgPtr, C.uint32_t(len(certEAMsg)),
		certEASigPtr, C.uint32_t(len(certEASig)),
		activeTEEListPtr, C.uint32_t(activeTEECount),
		activeSetCertInputPtr, C.uint32_t(activeSetCertInputLen),
		&bridgeSt,
		&bridgeRetval,
	)
	if ret != 0 {
		return nil, teeBridgeError("tee_dkrs_create_deal", ret, bridgeSt, bridgeRetval)
	}

	pHexStr := C.GoString((*C.char)(unsafe.Pointer(&pHexBuf[0])))

	return &SGXDealResult{
		CommitmentsA:     commitmentsABuf[:aOutSize],
		CommitmentB0:     b0Buf[:b0OutSize],
		SharesSerialized: sharesBuf[:sharesOutSize],
		G1Bytes:          g1Buf[:96],
		G2Bytes:          g2Buf[:192],
		PHex:             pHexStr,
	}, nil
}

func DKRSVerifyShare(receiverIndex, dealerIndex, newThreshold, newNumParties int,
	commitmentsA, commitmentB0, encryptedShareBytes []byte, certEAMsg, certEASig, activeTEEList, activeSetCertInput []byte, activeTEECount, activeSetCertInputLen uint32) (bool, error) {

	shareData := encryptedShareBytes

	var certEAMsgPtr, certEASigPtr *C.uint8_t
	if len(certEAMsg) > 0 {
		certEAMsgPtr = (*C.uint8_t)(unsafe.Pointer(&certEAMsg[0]))
	}
	if len(certEASig) > 0 {
		certEASigPtr = (*C.uint8_t)(unsafe.Pointer(&certEASig[0]))
	}

	var activeTEEListPtr *C.uint8_t
	if len(activeTEEList) > 0 {
		activeTEEListPtr = (*C.uint8_t)(unsafe.Pointer(&activeTEEList[0]))
	}

	var activeSetCertInputPtr *C.uint8_t
	if len(activeSetCertInput) > 0 {
		activeSetCertInputPtr = (*C.uint8_t)(unsafe.Pointer(&activeSetCertInput[0]))
	}

	var valid C.int
	ret := C.tee_dkrs_verify_share(
		C.int(receiverIndex),
		C.int(dealerIndex),
		C.int(newThreshold),
		C.int(newNumParties),
		(*C.uint8_t)(unsafe.Pointer(&commitmentsA[0])),
		C.uint32_t(len(commitmentsA)),
		(*C.uint8_t)(unsafe.Pointer(&commitmentB0[0])),
		C.uint32_t(len(commitmentB0)),
		(*C.uint8_t)(unsafe.Pointer(&shareData[0])),
		C.uint32_t(len(shareData)),
		&valid,
		certEAMsgPtr, C.uint32_t(len(certEAMsg)),
		certEASigPtr, C.uint32_t(len(certEASig)),
		activeTEEListPtr, C.uint32_t(activeTEECount),
		activeSetCertInputPtr, C.uint32_t(activeSetCertInputLen),
	)
	if ret != 0 {
		return false, fmt.Errorf("tee_dkrs_verify_share failed: %d", ret)
	}
	return valid != 0, nil
}

func DKRSExtract(myIndex, newThreshold, newNumParties, oldNumParties int,
	qualOldIndices []int32,
	b0Data []byte, b0DealerMap []int32,
	a0Data []byte, a0DealerMap []int32,
	encryptedSharesData []byte, sharesFromMap []int32,
	certEAMsg, certEASig, activeTEEList, activeSetCertInput []byte, activeTEECount, activeSetCertInputLen uint32) (*SGXExtractResult, error) {

	sharesData := encryptedSharesData
	if len(sharesFromMap) == 0 && len(encryptedSharesData) > 0 {
		scalarSize := 48
		numQual := len(qualOldIndices)
		if len(encryptedSharesData) == numQual*scalarSize {
			buf := make([]byte, 0, numQual*(4+1+scalarSize))
			for i := 0; i < numQual; i++ {
				entrySize := uint32(1 + scalarSize)
				szBuf := make([]byte, 4)
				binary.LittleEndian.PutUint32(szBuf, entrySize)
				buf = append(buf, szBuf...)
				buf = append(buf, 0x00)
				buf = append(buf, encryptedSharesData[i*scalarSize:(i+1)*scalarSize]...)
			}
			sharesData = buf
			sharesFromMap = qualOldIndices
		}
	}

	pkBuf := make([]byte, sgxMaxG2Buf)
	pkG1Buf := make([]byte, sgxMaxG1Buf)
	vkBuf := make([]byte, sgxMaxG1Buf)
	var pkOutSize, pkG1OutSize, vkOutSize C.uint32_t

	var qualPtr *C.int32_t
	if len(qualOldIndices) > 0 {
		qualPtr = (*C.int32_t)(unsafe.Pointer(&qualOldIndices[0]))
	}
	var b0MapPtr, a0MapPtr, sharesMapPtr *C.int32_t
	if len(b0DealerMap) > 0 {
		b0MapPtr = (*C.int32_t)(unsafe.Pointer(&b0DealerMap[0]))
	}
	if len(a0DealerMap) > 0 {
		a0MapPtr = (*C.int32_t)(unsafe.Pointer(&a0DealerMap[0]))
	}
	if len(sharesFromMap) > 0 {
		sharesMapPtr = (*C.int32_t)(unsafe.Pointer(&sharesFromMap[0]))
	}

	var b0Ptr, a0Ptr, sharesPtr *C.uint8_t
	if len(b0Data) > 0 {
		b0Ptr = (*C.uint8_t)(unsafe.Pointer(&b0Data[0]))
	}
	if len(a0Data) > 0 {
		a0Ptr = (*C.uint8_t)(unsafe.Pointer(&a0Data[0]))
	}
	if len(sharesData) > 0 {
		sharesPtr = (*C.uint8_t)(unsafe.Pointer(&sharesData[0]))
	}

	var certEAMsgPtr, certEASigPtr *C.uint8_t
	if len(certEAMsg) > 0 {
		certEAMsgPtr = (*C.uint8_t)(unsafe.Pointer(&certEAMsg[0]))
	}
	if len(certEASig) > 0 {
		certEASigPtr = (*C.uint8_t)(unsafe.Pointer(&certEASig[0]))
	}

	var activeTEEListPtr *C.uint8_t
	if len(activeTEEList) > 0 {
		activeTEEListPtr = (*C.uint8_t)(unsafe.Pointer(&activeTEEList[0]))
	}

	var activeSetCertInputPtr *C.uint8_t
	if len(activeSetCertInput) > 0 {
		activeSetCertInputPtr = (*C.uint8_t)(unsafe.Pointer(&activeSetCertInput[0]))
	}

	ret := C.tee_dkrs_extract(
		C.int(myIndex),
		C.int(newThreshold),
		C.int(newNumParties),
		C.int(oldNumParties),
		qualPtr, C.uint32_t(len(qualOldIndices)),
		b0Ptr, C.uint32_t(len(b0Data)),
		b0MapPtr, C.uint32_t(len(b0DealerMap)),
		a0Ptr, C.uint32_t(len(a0Data)),
		a0MapPtr, C.uint32_t(len(a0DealerMap)),
		sharesPtr, C.uint32_t(len(sharesData)),
		sharesMapPtr, C.uint32_t(len(sharesFromMap)),
		(*C.uint8_t)(unsafe.Pointer(&pkBuf[0])),
		C.uint32_t(len(pkBuf)),
		&pkOutSize,
		(*C.uint8_t)(unsafe.Pointer(&pkG1Buf[0])),
		C.uint32_t(len(pkG1Buf)),
		&pkG1OutSize,
		(*C.uint8_t)(unsafe.Pointer(&vkBuf[0])),
		C.uint32_t(len(vkBuf)),
		&vkOutSize,
		certEAMsgPtr, C.uint32_t(len(certEAMsg)),
		certEASigPtr, C.uint32_t(len(certEASig)),
		activeTEEListPtr, C.uint32_t(activeTEECount),
		activeSetCertInputPtr, C.uint32_t(activeSetCertInputLen),
	)
	if ret != 0 {
		return nil, fmt.Errorf("tee_dkrs_extract failed: %d", ret)
	}

	return &SGXExtractResult{
		GlobalPK:        pkBuf[:pkOutSize],
		GlobalPKG1:      pkG1Buf[:pkG1OutSize],
		VerificationKey: vkBuf[:vkOutSize],
	}, nil
}

type SGXEraseProofResult struct {
	EraseProofHash []byte // 32 bytes: keccak256(epoch||hardwareId||"ERASED")
	Proof          []byte // 65 bytes: secp256k1 signature
}

type SGXVRFKeypairResult struct {
	VRFPubKey []byte // 32 bytes: leftPad32(ethereumAddress)
}

type SGXVRFEvalResult struct {
	VRFResult []byte // 32 bytes: keccak256(domain||seed||pubkey||r||s||v)
	VRFProof  []byte // 65 bytes: r(32)||s(32)||v(1)
}

func EraseShare(epoch uint64, nodeIndex int, chainID []byte, contractAddress []byte, teeOwner []byte) (*SGXEraseProofResult, error) {
	if len(chainID) != 32 {
		return nil, fmt.Errorf("chainID must be 32 bytes")
	}
	if len(contractAddress) != 32 {
		return nil, fmt.Errorf("contractAddress must be 32 bytes")
	}
	if len(teeOwner) != 32 {
		return nil, fmt.Errorf("teeOwner must be 32 bytes")
	}

	eraseProofHashBuf := make([]byte, 32)
	proofBuf := make([]byte, 65)
	var proofOutSize C.uint32_t

	ret := C.tee_erase_share(
		C.uint64_t(epoch),
		C.int(nodeIndex),
		(*C.uint8_t)(unsafe.Pointer(&chainID[0])),
		C.uint32_t(len(chainID)),
		(*C.uint8_t)(unsafe.Pointer(&contractAddress[0])),
		C.uint32_t(len(contractAddress)),
		(*C.uint8_t)(unsafe.Pointer(&teeOwner[0])),
		C.uint32_t(len(teeOwner)),
		(*C.uint8_t)(unsafe.Pointer(&eraseProofHashBuf[0])),
		C.uint32_t(len(eraseProofHashBuf)),
		(*C.uint8_t)(unsafe.Pointer(&proofBuf[0])),
		C.uint32_t(len(proofBuf)),
		&proofOutSize,
	)
	if ret != 0 {
		return nil, fmt.Errorf("tee_erase_share failed: %d", ret)
	}

	return &SGXEraseProofResult{
		EraseProofHash: eraseProofHashBuf,
		Proof:          proofBuf[:proofOutSize],
	}, nil
}

func VRFEval(chainID []byte, contractAddress []byte, epoch uint64, hardwareID []byte, vrfPubKey []byte, seed []byte) (*SGXVRFEvalResult, error) {
	if len(seed) == 0 {
		return nil, errors.New("seed is empty")
	}
	if len(chainID) == 0 {
		return nil, errors.New("chainID is empty")
	}
	if len(contractAddress) == 0 {
		return nil, errors.New("contractAddress is empty")
	}
	if len(hardwareID) == 0 {
		return nil, errors.New("hardwareID is empty")
	}
	if len(vrfPubKey) == 0 {
		return nil, errors.New("vrfPubKey is empty")
	}

	resultBuf := make([]byte, 32)
	proofBuf := make([]byte, 65)
	var proofOutSize C.uint32_t

	ret := C.tee_vrf_eval(
		(*C.uint8_t)(unsafe.Pointer(&chainID[0])),
		C.uint32_t(len(chainID)),
		(*C.uint8_t)(unsafe.Pointer(&contractAddress[0])),
		C.uint32_t(len(contractAddress)),
		C.uint64_t(epoch),
		(*C.uint8_t)(unsafe.Pointer(&hardwareID[0])),
		C.uint32_t(len(hardwareID)),
		(*C.uint8_t)(unsafe.Pointer(&vrfPubKey[0])),
		C.uint32_t(len(vrfPubKey)),
		(*C.uint8_t)(unsafe.Pointer(&seed[0])),
		C.uint32_t(len(seed)),
		(*C.uint8_t)(unsafe.Pointer(&resultBuf[0])),
		C.uint32_t(len(resultBuf)),
		(*C.uint8_t)(unsafe.Pointer(&proofBuf[0])),
		C.uint32_t(len(proofBuf)),
		&proofOutSize,
	)
	if ret != 0 {
		return nil, fmt.Errorf("tee_vrf_eval failed: %d", ret)
	}

	return &SGXVRFEvalResult{
		VRFResult: resultBuf[:32],
		VRFProof:  proofBuf[:proofOutSize],
	}, nil
}

func PartialAdapt(sourceMsg, targetMsg []byte, certReqAuthMsg, certReqAuthSig []byte, certEAMsg, certEASig []byte, dReq, dPolicy, committeePubKeyVersion, expiresAt, activeTEEList, activeSetCertInput, dReqInput []byte, activeTEECount, activeSetCertInputLen uint32) (*SGXPartialAdaptResult, error) {
	contribBuf := make([]byte, sgxMaxG1Buf)
	challengeBuf := make([]byte, sgxMaxScalarBuf)
	responseBuf := make([]byte, sgxMaxScalarBuf)
	yBuf := make([]byte, sgxMaxG1Buf)
	sigmaBuf := make([]byte, 65)
	var contribOutSize, yOutSize, sigmaOutSize C.uint32_t

	var srcPtr, tgtPtr *C.uint8_t
	if len(sourceMsg) > 0 {
		srcPtr = (*C.uint8_t)(unsafe.Pointer(&sourceMsg[0]))
	}
	if len(targetMsg) > 0 {
		tgtPtr = (*C.uint8_t)(unsafe.Pointer(&targetMsg[0]))
	}

	var certReqAuthMsgPtr, certReqAuthSigPtr *C.uint8_t
	if len(certReqAuthMsg) > 0 {
		certReqAuthMsgPtr = (*C.uint8_t)(unsafe.Pointer(&certReqAuthMsg[0]))
	}
	if len(certReqAuthSig) > 0 {
		certReqAuthSigPtr = (*C.uint8_t)(unsafe.Pointer(&certReqAuthSig[0]))
	}

	var certEAMsgPtr, certEASigPtr *C.uint8_t
	if len(certEAMsg) > 0 {
		certEAMsgPtr = (*C.uint8_t)(unsafe.Pointer(&certEAMsg[0]))
	}
	if len(certEASig) > 0 {
		certEASigPtr = (*C.uint8_t)(unsafe.Pointer(&certEASig[0]))
	}

	var dReqPtr *C.uint8_t
	if len(dReq) > 0 {
		dReqPtr = (*C.uint8_t)(unsafe.Pointer(&dReq[0]))
	}

	var dPolicyPtr *C.uint8_t
	if len(dPolicy) > 0 {
		dPolicyPtr = (*C.uint8_t)(unsafe.Pointer(&dPolicy[0]))
	}

	var cpkvPtr *C.uint8_t
	if len(committeePubKeyVersion) > 0 {
		cpkvPtr = (*C.uint8_t)(unsafe.Pointer(&committeePubKeyVersion[0]))
	}

	var expiresAtPtr *C.uint8_t
	if len(expiresAt) > 0 {
		expiresAtPtr = (*C.uint8_t)(unsafe.Pointer(&expiresAt[0]))
	}

	var activeTEEListPtr *C.uint8_t
	if len(activeTEEList) > 0 {
		activeTEEListPtr = (*C.uint8_t)(unsafe.Pointer(&activeTEEList[0]))
	}

	var activeSetCertInputPtr *C.uint8_t
	if len(activeSetCertInput) > 0 {
		activeSetCertInputPtr = (*C.uint8_t)(unsafe.Pointer(&activeSetCertInput[0]))
	}

	var dReqInputPtr *C.uint8_t
	if len(dReqInput) > 0 {
		dReqInputPtr = (*C.uint8_t)(unsafe.Pointer(&dReqInput[0]))
	}

	ret := C.tee_partial_adapt(
		srcPtr, C.uint32_t(len(sourceMsg)),
		tgtPtr, C.uint32_t(len(targetMsg)),
		certReqAuthMsgPtr, C.uint32_t(len(certReqAuthMsg)),
		certReqAuthSigPtr, C.uint32_t(len(certReqAuthSig)),
		certEAMsgPtr, C.uint32_t(len(certEAMsg)),
		certEASigPtr, C.uint32_t(len(certEASig)),
		dReqPtr, C.uint32_t(len(dReq)),
		dPolicyPtr, C.uint32_t(len(dPolicy)),
		cpkvPtr, C.uint32_t(len(committeePubKeyVersion)),
		expiresAtPtr, C.uint32_t(len(expiresAt)),
		activeTEEListPtr, C.uint32_t(activeTEECount),
		activeSetCertInputPtr, C.uint32_t(activeSetCertInputLen),
		dReqInputPtr, C.uint32_t(len(dReqInput)),
		(*C.uint8_t)(unsafe.Pointer(&contribBuf[0])),
		C.uint32_t(len(contribBuf)),
		&contribOutSize,
		(*C.uint8_t)(unsafe.Pointer(&challengeBuf[0])),
		C.uint32_t(len(challengeBuf)),
		(*C.uint8_t)(unsafe.Pointer(&responseBuf[0])),
		C.uint32_t(len(responseBuf)),
		(*C.uint8_t)(unsafe.Pointer(&yBuf[0])),
		C.uint32_t(len(yBuf)),
		&yOutSize,
		(*C.uint8_t)(unsafe.Pointer(&sigmaBuf[0])),
		C.uint32_t(len(sigmaBuf)),
		&sigmaOutSize,
	)
	if ret != 0 {
		return nil, fmt.Errorf("tee_partial_adapt failed: %d", ret)
	}

	return partialAdaptResultFromBuffers(contribBuf[:contribOutSize], challengeBuf, responseBuf, yBuf[:yOutSize], sigmaBuf[:sigmaOutSize])
}

func G1FromBytes(data []byte) (*bls12381.PointG1, error) {
	engine := bls12381.NewEngine()
	return engine.G1.FromBytes(data)
}

func G2FromBytes(data []byte) (*bls12381.PointG2, error) {
	engine := bls12381.NewEngine()
	return engine.G2.FromBytes(data)
}
