// 基于 BLS12-381 配对和 Shamir 秘密共享
package chameleon

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	gnarkfr "github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gnarkfft "github.com/consensys/gnark-crypto/ecc/bls12-381/fr/fft"
	"github.com/ethereum/go-ethereum/log"

	bls12381 "github.com/kilic/bls12-381"
)

const (
	// DKG commitment、验证密钥等内部 G1 点继续使用未压缩格式。
	g1PointByteSize = 96
	// 区块头中的变色龙哈希随机参数 r/r' 使用标准压缩格式。
	g1CompressedPointByteSize = 48
)

// Types
type (
	// PublicParameters contains the bilinear group parameters
	PublicParameters struct {
		g1 *bls12381.PointG1
		g2 *bls12381.PointG2
		gt *bls12381.GT
		p  *big.Int // prime order
	}

	// NITCHKey represents a NITCH key pair
	NITCHKey struct {
		PP              *PublicParameters
		GlobalPK        *bls12381.PointG2 // pk = g2^α
		GlobalPKG1      *bls12381.PointG1 // g1^α (useful for verification)
		Threshold       int
		NumParties      int
		SecretShare     *big.Int          // sk_i
		VerificationKey *bls12381.PointG1 // vk_i = g1^{sk_i}
		Index           int               // party index i (1-based)
	}

	// HashOutput represents a chameleon hash output
	HashOutput struct {
		Hash       *big.Int // hash value h (as big.Int)
		CheckValue *big.Int // r ∈ G1 (as big.Int, serialized from G1 point)
	}

	// AdaptationShare represents a partial adaptation share
	AdaptationShare struct {
		Index *big.Int          // 使用 *big.Int 以便 RLP 序列化（p2p 传输）
		Share *bls12381.PointG1 // c_i = y^{sk_i}
		Proof *NIZKProof
	}

	// NIZKProof is a non-interactive zero-knowledge proof
	NIZKProof struct {
		Omega *big.Int // challenge
		Z     *big.Int // response
	}

	// Message is a byte slice
	Message []byte
)

var (
	// Global bilinear pairing instance（kilic 库内部使用临时变量，并发会互相覆盖，需用 pairingMu 串行化）
	pairing   *bls12381.Engine
	pairingMu sync.Mutex
	rootCache = struct {
		mu  sync.RWMutex
		byN map[int]*big.Int
	}{byN: make(map[int]*big.Int)}

	// Error definitions
	ErrInvalidShare       = errors.New("invalid adaptation share")
	ErrInsufficientShares = errors.New("insufficient shares")
	ErrInvalidProof       = errors.New("invalid NIZK proof")
	ErrInvalidParameters  = errors.New("invalid parameters")
)

// Initialize the pairing engine
func init() {
	pairing = bls12381.NewEngine()
}

// ====================== Setup ======================

// Setup generates public parameters
func Setup() (*PublicParameters, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	// 确保配对引擎已初始化
	if pairing == nil {
		pairing = bls12381.NewEngine()
	}

	// Using BLS12-381 curve (fixed)
	g1 := pairing.G1.One()
	g2 := pairing.G2.One()

	// 验证生成的点是否有效
	if g1 == nil {
		return nil, fmt.Errorf("failed to generate G1 generator (One() returned nil)")
	}
	if g2 == nil {
		return nil, fmt.Errorf("failed to generate G2 generator (One() returned nil)")
	}

	// Get the group order (curve order for BLS12-381)
	// BLS12-381 order: 0x73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001
	p, _ := new(big.Int).SetString("73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)

	return &PublicParameters{
		g1: g1,
		g2: g2,
		gt: pairing.GT(),
		p:  p,
	}, nil
}

// ====================== Distributed Key Generation ======================

// SimulatedDistKG simulates the distributed key generation protocol
// In practice, this would be an interactive protocol
func SimulatedDistKG(pp *PublicParameters, t, n int) ([]*NITCHKey, error) {
	if t >= n {
		return nil, fmt.Errorf("threshold t must be less than n")
	}
	if !isPowerOfTwo(n) {
		return nil, fmt.Errorf("n must be a power of two")
	}

	// 以下所有 pairing.G1/G2 运算在 pairingMu 保护下进行
	pairingMu.Lock()
	defer pairingMu.Unlock()

	// Generate a random polynomial f(z) = α + a1*z + ... + at*z^t
	// where f(0) = α is the global secret
	α := randomScalar(pp.p)
	coefficients := make([]*big.Int, t+1)
	coefficients[0] = α

	// Generate random coefficients for degree 1 to t
	for i := 1; i <= t; i++ {
		coefficients[i] = randomScalar(pp.p)
	}

	// Compute global public key: pk = g2^α
	pk := pairing.G2.New()
	αFr := bigIntToFr(α)
	pairing.G2.MulScalar(pk, pp.g2, αFr)

	// Compute g1^α for verification
	pkG1 := pairing.G1.New()
	pairing.G1.MulScalar(pkG1, pp.g1, αFr)

	// Generate keys for each party
	keys := make([]*NITCHKey, n)
	for i := 0; i < n; i++ {
		// Compute share on root-of-unity domain: sk_i = f(omega^(i+1)).
		xi, err := evaluationPointForIndex(pp.p, n, i+1)
		if err != nil {
			return nil, err
		}
		share := evaluatePolynomial(coefficients, xi, pp.p)

		// Compute verification key: vk_i = g1^{sk_i}
		vk := pairing.G1.New()
		shareFr := bigIntToFr(share)
		pairing.G1.MulScalar(vk, pp.g1, shareFr)

		keys[i] = &NITCHKey{
			PP:              pp,
			GlobalPK:        pk,
			GlobalPKG1:      pkG1,
			Threshold:       t,
			NumParties:      n,
			SecretShare:     share,
			VerificationKey: vk,
			Index:           i + 1,
		}
	}

	return keys, nil
}

// ====================== NITCH 分布式密钥生成 (DistKG) Protocol 1 ======================
// 基于 Pedersen/Feldman VSS，实现 (t,n) 门限：n 个委员会成员共同生成全局公钥 pk，
// 每人获得私钥份额 sk_j 和验证密钥 vk_j，需 t+1 个份额才能重构或生成碰撞。

// DistKGDeal 步骤1 中成员 P_i 广播的承诺（公开部分）
// A[k] = g1^{a_{i,k}}, k=0..t；B0 = g2^{a_{i,0}}
// 携带公开参数 G1Bytes,G2Bytes,P 以便所有节点使用同一 (g1,g2)，保证份额可合并
type DistKGDeal struct {
	DealerIndex *big.Int // 成员索引 i (1-based)
	Threshold   *big.Int // 门限 t
	NumParties  *big.Int // 参与方数 n
	A           []byte   // 序列化后的 A_{i,0},...,A_{i,t} (G1 点依次拼接)
	B0          []byte   // 序列化后的 B_{i,0} (G2 点)
	// 公开参数：首个发出 Deal 的节点填写，其他节点从收到的 Deal 解析后使用同一 pp
	G1Bytes []byte // g1 点序列化（未压缩 96 字节）
	G2Bytes []byte // g2 点序列化
	P       string // 群阶 p 的十六进制字符串
}

// DistKGShare 步骤1 中 P_i 私密发送给 P_j 的份额 s_{i,j} = f_i(ω^j)
type DistKGShare struct {
	FromIndex *big.Int // 发送方 i (1-based)
	ToIndex   *big.Int // 接收方 j (1-based)
	Share     *big.Int // s_{i,j}
}

// DistKGComplaint 步骤2/3 中 P_j 对 P_i 的投诉（份额验证失败时）
type DistKGComplaint struct {
	ComplainantIndex *big.Int // 投诉方 j (1-based)
	DealerIndex      *big.Int // 被投诉方 i (1-based)
}

// DistKGResponse 步骤3 中 P_i 对被投诉份额的公开回应（广播正确份额 s_{i,j}）
type DistKGResponse struct {
	DealerIndex *big.Int // 回应方 i (1-based)
	ToIndex     *big.Int // 针对的接收方 j (1-based)
	Share       *big.Int // s_{i,j}
}

// DistKGVerifyKey 步骤4后广播的验证密钥
// 在DistKG Extraction完成后，各节点广播自己的验证密钥vk_j
type DistKGVerifyKey struct {
	Index           *big.Int // 节点索引 j (1-based)
	VerificationKey []byte   // vk_j = g1^{sk_j} (G1点序列化)
}

// DKRSDeal 是重分享阶段旧委员会成员广播的承诺。
// A[k] = g1^{a_k}, 其中 f_i(z)=sk_i+a_1 z+...+a_t z^t。
type DKRSDeal struct {
	DealerIndex   *big.Int
	OldThreshold  *big.Int
	OldNumParties *big.Int
	NewThreshold  *big.Int
	NewNumParties *big.Int
	A             []byte
	B0            []byte // G2 commitment to the constant coefficient
}

// DKRSShare 是旧委员会成员 P_i 发给新委员会成员 P_j 的份额 s_{i,j}=f_i(ω^j)。
type DKRSShare struct {
	FromIndex *big.Int
	ToIndex   *big.Int
	Share     *big.Int
}

// DKRSComplaint 是新委员会成员对旧委员会成员份额的投诉。
type DKRSComplaint struct {
	ComplainantIndex *big.Int
	DealerIndex      *big.Int
}

// DKRSResponse 是旧委员会成员对投诉的公开回应。
type DKRSResponse struct {
	DealerIndex *big.Int
	ToIndex     *big.Int
	Share       *big.Int
}

// DKRSVerifyKey 用于广播新委员会成员验证密钥。
type DKRSVerifyKey struct {
	Index           *big.Int
	VerificationKey []byte
}

// DKRSDealPrivate 是 Deal 对应的本地私有数据。
type DKRSDealPrivate struct {
	DealerIndex int
	Coeffs      []*big.Int
	Shares      map[int]*big.Int
}

// NewDKRSVerifyKey 从 NITCHKey 创建 DKRS 验证密钥消息。
func NewDKRSVerifyKey(key *NITCHKey) *DKRSVerifyKey {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	vkBytes := pairing.G1.ToBytes(key.VerificationKey)
	return &DKRSVerifyKey{
		Index:           big.NewInt(int64(key.Index)),
		VerificationKey: vkBytes,
	}
}

// GetVerificationKeyPoint 将序列化的验证密钥反序列化为 G1 点。
func (vk *DKRSVerifyKey) GetVerificationKeyPoint() (*bls12381.PointG1, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairing.G1.FromBytes(vk.VerificationKey)
}

// NewDistKGVerifyKey 从NITCHKey创建验证密钥消息
func NewDistKGVerifyKey(key *NITCHKey) *DistKGVerifyKey {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	vkBytes := pairing.G1.ToBytes(key.VerificationKey)
	return &DistKGVerifyKey{
		Index:           big.NewInt(int64(key.Index)),
		VerificationKey: vkBytes,
	}
}

// GetVerificationKeyPoint 反序列化为G1点
func (vk *DistKGVerifyKey) GetVerificationKeyPoint() (*bls12381.PointG1, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairing.G1.FromBytes(vk.VerificationKey)
}

// DistKGDealPrivate 步骤1 中 P_i 本地保存的私密数据（不广播）
// 含多项式系数与发给每个人的份额，用于生成 Share 消息
type DistKGDealPrivate struct {
	DealerIndex int
	Coeffs      []*big.Int       // a_{i,0},...,a_{i,t}
	Shares      map[int]*big.Int // j -> s_{i,j}, j=1..n
}

// CreateDistKGDeal 步骤1 Deal：成员 P_i 生成 t 阶多项式 f_i(z)，广播承诺 A、B0，并生成发给各成员的份额
// dealerIndex: 本节点索引 i (1-based), t: 门限, n: 参与方数
// 返回公开的 Deal（用于广播）和私密数据（用于向各 j 发送 Share）
//
// 输入校验在此完成，随后分发到内部 seam（当前为 pure-Go 实现，
// 见 hashseam_pure.go 的 pureCreateDistKGDeal；后续 build-tag 选择器可路由到 cgo 实现）。
func CreateDistKGDeal(pp *PublicParameters, dealerIndex, t, n int) (*DistKGDeal, *DistKGDealPrivate, error) {
	if t >= n {
		return nil, nil, fmt.Errorf("threshold t must be less than n")
	}
	if !isPowerOfTwo(n) {
		return nil, nil, fmt.Errorf("n must be a power of two")
	}
	if dealerIndex < 1 || dealerIndex > n {
		return nil, nil, fmt.Errorf("dealerIndex must be in [1,n]")
	}

	return seamCreateDistKGDeal(pp, dealerIndex, t, n)
}

// PublicParametersFromDeal 从 Deal 携带的公开参数解析出 PublicParameters，保证所有节点使用同一 (g1,g2,p)
func PublicParametersFromDeal(deal *DistKGDeal) (*PublicParameters, error) {
	if deal == nil || len(deal.G1Bytes) == 0 || len(deal.G2Bytes) == 0 || deal.P == "" {
		return nil, fmt.Errorf("deal has no public parameters (G1Bytes/G2Bytes/P)")
	}
	pairingMu.Lock()
	defer pairingMu.Unlock()
	g1, err := pairing.G1.FromBytes(deal.G1Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize G1 from deal: %v", err)
	}
	g2, err := pairing.G2.FromBytes(deal.G2Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize G2 from deal: %v", err)
	}
	p, ok := new(big.Int).SetString(deal.P, 16)
	if !ok {
		return nil, fmt.Errorf("failed to parse P from deal")
	}
	return &PublicParameters{
		g1: g1,
		g2: g2,
		gt: pairing.GT(),
		p:  p,
	}, nil
}

// VerifyDistKGShare 步骤2 验证：P_j 验证收到的 s_{i,j} 是否满足 g1^{s_{i,j}} = ∏_{k=0}^t (A_{i,k})^{j^k}
// 以及 e(g1, B_{i,0}) = e(A_{i,0}, g2)
// deal 为 P_i 广播的 DistKGDeal，shareSij 为 P_i 声称的 s_{i,j}，receiverIndex 为 j (1-based)
//
// 输入校验在此完成，随后分发到内部 seam（pureVerifyDistKGShare）。
func VerifyDistKGShare(pp *PublicParameters, deal *DistKGDeal, shareSij *big.Int, receiverIndex int) (bool, error) {
	if deal == nil || shareSij == nil {
		return false, ErrInvalidParameters
	}
	t, err := intFromBig(deal.Threshold, "deal.Threshold")
	if err != nil {
		return false, err
	}
	n, err := intFromBig(deal.NumParties, "deal.NumParties")
	if err != nil {
		return false, err
	}
	if !isPowerOfTwo(n) {
		return false, fmt.Errorf("n must be a power of two")
	}
	if receiverIndex < 1 || receiverIndex > n {
		return false, fmt.Errorf("receiverIndex must be in [1,n]")
	}

	return seamVerifyDistKGShare(pp, deal, shareSij, receiverIndex, t, n)
}

// DistKGExtract 步骤4 密钥提取：根据合格成员集合 QUAL、所有合格成员的 Deal、以及本节点 j 收到的份额，计算
// 全局公钥 pk、验证密钥 vk_j、私钥份额 sk_j，并返回本节点的 NITCHKey
// qual: 合格成员索引集合（1-based）, deals: 仅 QUAL 中成员的 Deal（key=DealerIndex）, sharesToJ: 发给本节点 j 的份额（key=DealerIndex，value=s_{i,j}）
//
// 输入校验在此完成，随后分发到内部 seam（pureDistKGExtract）。
func DistKGExtract(pp *PublicParameters, myIndex int, qual []int, deals map[int]*DistKGDeal, sharesToJ map[int]*big.Int) (*NITCHKey, error) {
	if len(qual) == 0 {
		return nil, ErrInvalidParameters
	}
	t, err := intFromBig(deals[qual[0]].Threshold, "deal.Threshold")
	if err != nil {
		return nil, err
	}
	n, err := intFromBig(deals[qual[0]].NumParties, "deal.NumParties")
	if err != nil {
		return nil, err
	}
	if !isPowerOfTwo(n) {
		return nil, fmt.Errorf("n must be a power of two")
	}
	for _, i := range qual {
		if _, ok := deals[i]; !ok {
			return nil, fmt.Errorf("missing deal for qualified member %d", i)
		}
		if _, ok := sharesToJ[i]; !ok {
			return nil, fmt.Errorf("missing share from %d for myself %d", i, myIndex)
		}
	}

	return seamDistKGExtract(pp, myIndex, qual, deals, sharesToJ, t, n)
}

// CreateDKRSDeal 由旧委员会成员创建重分享 Deal 和发往新委员会的份额。
// 评估点严格采用 x_j=ω^j（newN 域）。
//
// 输入校验在此完成，随后分发到内部 seam（pureCreateDKRSDeal）。
func CreateDKRSDeal(pp *PublicParameters, dealerIndex int, oldSecretShare *big.Int, oldThreshold int, newThreshold int, newN int) (*DKRSDeal, *DKRSDealPrivate, error) {
	if pp == nil || pp.p == nil || oldSecretShare == nil {
		return nil, nil, ErrInvalidParameters
	}
	if oldThreshold < 0 || newThreshold < 0 {
		return nil, nil, fmt.Errorf("threshold must be non-negative")
	}
	if newThreshold >= newN {
		return nil, nil, fmt.Errorf("new threshold must be less than newN")
	}
	if !isPowerOfTwo(newN) {
		return nil, nil, fmt.Errorf("newN must be a power of two")
	}
	if dealerIndex < 1 {
		return nil, nil, fmt.Errorf("dealerIndex must be >= 1")
	}

	return seamCreateDKRSDeal(pp, dealerIndex, oldSecretShare, oldThreshold, newThreshold, newN)
}

// VerifyDKRSShare 验证 DKRS 份额是否与承诺一致。
//
// 输入校验在此完成，随后分发到内部 seam（pureVerifyDKRSShare）。
func VerifyDKRSShare(pp *PublicParameters, deal *DKRSDeal, shareSij *big.Int, receiverIndex int) (bool, error) {
	if pp == nil || pp.p == nil || deal == nil || shareSij == nil {
		return false, ErrInvalidParameters
	}
	newT, err := intFromBig(deal.NewThreshold, "deal.NewThreshold")
	if err != nil {
		return false, err
	}
	newN, err := intFromBig(deal.NewNumParties, "deal.NewNumParties")
	if err != nil {
		return false, err
	}
	if !isPowerOfTwo(newN) {
		return false, fmt.Errorf("newN must be a power of two")
	}
	if receiverIndex < 1 || receiverIndex > newN {
		return false, fmt.Errorf("receiverIndex must be in [1,newN]")
	}
	if len(deal.A) != (newT+1)*g1PointByteSize {
		return false, fmt.Errorf("invalid deal A length")
	}

	return seamVerifyDKRSShare(pp, deal, shareSij, receiverIndex, newT, newN)
}

// BuildDKRSQUAL 根据投诉构建旧委员会合格集合。
func BuildDKRSQUAL(complaints []DKRSComplaint, oldThreshold int, oldN int) []int {
	complaintCount := make(map[int]int)
	for _, c := range complaints {
		idx, err := intFromBig(c.DealerIndex, "complaint.DealerIndex")
		if err != nil {
			continue
		}
		complaintCount[idx]++
	}
	qual := make([]int, 0, oldN)
	for i := 1; i <= oldN; i++ {
		if complaintCount[i] <= oldThreshold {
			qual = append(qual, i)
		}
	}
	return qual
}

// DKRSExtract 为新委员会成员提取新份额。
// 插值系数在旧委员会域点 x_i=ω_old^i 上计算，随后聚合得到 sk'_j。
//
// 输入校验在此完成，随后分发到内部 seam（pureDKRSExtract）。
func DKRSExtract(pp *PublicParameters, currentKey *NITCHKey, myNewIndex int, oldQual []int, deals map[int]*DKRSDeal, sharesToMe map[int]*big.Int) (*NITCHKey, error) {
	if pp == nil || pp.p == nil || currentKey == nil || currentKey.GlobalPK == nil || currentKey.GlobalPKG1 == nil {
		return nil, ErrInvalidParameters
	}
	if len(oldQual) == 0 {
		return nil, ErrInvalidParameters
	}
	if myNewIndex < 1 {
		return nil, fmt.Errorf("myNewIndex must be >= 1")
	}
	if deals == nil || sharesToMe == nil {
		return nil, ErrInvalidParameters
	}

	firstDeal, ok := deals[oldQual[0]]
	if !ok || firstDeal == nil {
		return nil, fmt.Errorf("missing deal for oldQual[0]")
	}
	newThreshold, err := intFromBig(firstDeal.NewThreshold, "deal.NewThreshold")
	if err != nil {
		return nil, err
	}
	newN, err := intFromBig(firstDeal.NewNumParties, "deal.NewNumParties")
	if err != nil {
		return nil, err
	}
	if !isPowerOfTwo(newN) {
		return nil, fmt.Errorf("newN must be a power of two")
	}
	if myNewIndex > newN {
		return nil, fmt.Errorf("myNewIndex must be in [1,newN]")
	}
	oldN := currentKey.NumParties
	if firstDeal.OldNumParties != nil && firstDeal.OldNumParties.Sign() > 0 {
		oldN, err = intFromBig(firstDeal.OldNumParties, "deal.OldNumParties")
		if err != nil {
			return nil, err
		}
	}
	if !isPowerOfTwo(oldN) {
		return nil, fmt.Errorf("oldN must be a power of two")
	}

	return seamDKRSExtract(pp, currentKey, myNewIndex, oldQual, deals, sharesToMe, newThreshold, newN, oldN)
}

// BuildQUAL 步骤3 投诉处理：根据投诉列表确定合格成员集合 QUAL
// 若某成员 i 被投诉次数 > t，则从 QUAL 中剔除
// complaints: 投诉列表（每项为 ComplainantIndex 对 DealerIndex 的投诉）；t: 门限；n: 参与方总数
// 返回 1-based 的合格成员索引列表（顺序稳定）
func BuildQUAL(complaints []DistKGComplaint, t, n int) []int {
	complaintCount := make(map[int]int)
	for _, c := range complaints {
		idx, err := intFromBig(c.DealerIndex, "complaint.DealerIndex")
		if err != nil {
			continue
		}
		complaintCount[idx]++
	}
	qual := make([]int, 0, n)
	for i := 1; i <= n; i++ {
		if complaintCount[i] <= t {
			qual = append(qual, i)
		}
	}
	return qual
}

// ====================== Hashing ======================

// Hash 实现步骤1：计算变色龙哈希值
//
// 算法：Hash（论文中的哈希算法）
// 输入：
//   - key: 包含公钥 pk = g_2^α 的密钥
//   - m: 消息
//   - r: 可选的 r 值（*big.Int 形式），如果为 nil 则随机生成
//
// 输出：
//   - HashOutput: 包含哈希值 h 和随机数 r
//
// 哈希计算：h = e(r, g_2) * e(H_1(m), pk) = e(r, g_2) * e(H_1(m), g_2^α)
//
// 步骤：
//  1. 计算 H_1(m)：将消息 m 映射到 G_1 群中的点
//  2. 使用传入的 r 值，如果为 nil 则生成随机数 r ∈ G_1
//  3. 计算配对：e(r, g_2) 和 e(H_1(m), pk)
//  4. 计算哈希：h = e(r, g_2) * e(H_1(m), pk)
//
// 调用时机：初始计算区块或交易的变色龙哈希时
func Hash(key *NITCHKey, m Message, r *big.Int) (*HashOutput, error) {
	// 检查必要的字段
	start := time.Now()
	if key == nil || key.PP == nil {
		return nil, fmt.Errorf("invalid key: key or PP is nil")
	}
	if key.GlobalPK == nil {
		return nil, fmt.Errorf("invalid key: GlobalPK is nil")
	}

	// 步骤1-4 全程在 pairingMu 保护下使用全局 pairing 引擎，与 Verify 保持一致。
	// 两个原因都必须串行化：
	//  1. kilic 引擎的 scratch（G1.t / G2.t 等）非线程安全（详见 bigIntToG1PointUnlocked）；
	//  2. AddPair/Affine 会“就地”改写传入的点（key.PP.g2、key.GlobalPK），这些系统公钥点
	//     在所有调用间共享——即便每个调用使用独立的 Engine（旧版 localPairing）也无法避免，
	//     因为竞争发生在“共享点”对象上，而非引擎上。因此必须用全局 pairingMu 串行化。
	pairingMu.Lock()
	defer pairingMu.Unlock()

	// 步骤1：H_1: {0,1}* -> G_1（哈希到曲线）
	h1m, err := hashToG1Unlocked(m)
	if err != nil {
		return nil, err
	}

	var rPoint *bls12381.PointG1
	if r != nil {
		// 将 r 从 big.Int 反序列化为 G1 点
		rPoint, err = bigIntToG1PointUnlocked(r)
		if err != nil {
			rBytesLen := len(r.Bytes())
			return nil, fmt.Errorf("failed to convert r to G1 point (r bytes length: %d): %v", rBytesLen, err)
		}
		// kilic/bls12-381 的 FromBytes 已验证点在曲线上；到达这里说明点合法。
	} else {
		return nil, fmt.Errorf("r is nil, please provide a valid r value")
	}

	// 验证输入参数
	if rPoint == nil {
		return nil, fmt.Errorf("invalid parameter: rPoint is nil")
	}
	if key.PP == nil || key.PP.g2 == nil {
		return nil, fmt.Errorf("invalid parameter: PP or PP.g2 is nil")
	}
	if h1m == nil {
		return nil, fmt.Errorf("invalid parameter: h1m is nil")
	}
	if key.GlobalPK == nil {
		return nil, fmt.Errorf("invalid parameter: GlobalPK is nil")
	}

	// 步骤3：计算配对 e(r, g_2) 和 e(H_1(m), pk)，其中 pk = g_2^α
	pairing.Reset()
	pairing.AddPair(rPoint, key.PP.g2)
	pairing.AddPair(h1m, key.GlobalPK)
	result := pairing.Result() // *fe12，配对结果

	// 验证配对结果
	if result == nil {
		return nil, fmt.Errorf("pairing result is nil")
	}

	// 步骤4：将 fe12 转换为 big.Int
	// fe12 是 GT 群的内部表示，直接序列化为字节
	// fe12ToBigInt 内部使用 NewGT()（每次新建）且 toBytes 为纯序列化，线程安全。
	hBigInt := fe12ToBigInt(result)
	rBigInt := g1PointToBigIntUnlocked(rPoint)
	cost := time.Since(start)
	log.Info("CHash computation completed", "hashTime=", cost)
	return &HashOutput{
		Hash:       hBigInt,
		CheckValue: rBigInt,
	}, nil
}

// ComputeTransactionR deterministically derives the initial NITCH randomizer
// for a signed transaction. The versioned domain separator and chain ID make
// the output stable across nodes while preventing cross-chain reuse.
func ComputeTransactionR(key *NITCHKey, chainID *big.Int, txWithoutRandomizerDigest []byte) (*big.Int, error) {
	if key == nil || key.PP == nil || key.GlobalPKG1 == nil {
		return nil, fmt.Errorf("invalid key: missing required fields")
	}
	if chainID == nil || chainID.Sign() < 0 {
		return nil, fmt.Errorf("invalid chain ID")
	}
	if len(txWithoutRandomizerDigest) == 0 {
		return nil, fmt.Errorf("transaction digest is empty")
	}

	chainIDBytes := chainID.Bytes()
	h := sha256.New()
	h.Write([]byte("NITCH_TX_R_V1"))
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(chainIDBytes)))
	h.Write(length[:])
	h.Write(chainIDBytes)
	binary.BigEndian.PutUint64(length[:], uint64(len(txWithoutRandomizerDigest)))
	h.Write(length[:])
	h.Write(txWithoutRandomizerDigest)
	seedBytes := h.Sum(nil)

	pairingMu.Lock()
	defer pairingMu.Unlock()
	seedPoint, err := hashToG1Unlocked(seedBytes)
	if err != nil {
		return nil, err
	}
	r := pairing.G1.New()
	pairing.G1.Add(r, seedPoint, key.GlobalPKG1)
	rBytes := pairing.G1.ToCompressed(r)
	if len(rBytes) != g1CompressedPointByteSize {
		return nil, fmt.Errorf("G1 ToCompressed(r) length=%d", len(rBytes))
	}
	if _, err := pairing.G1.FromCompressed(rBytes); err != nil {
		return nil, fmt.Errorf("generated transaction r is not a valid G1 point: %w", err)
	}
	return new(big.Int).SetBytes(rBytes), nil
}

// Verify 实现步骤4：验证变色龙哈希
//
// 算法：Verify（论文中的验证算法）
// 输入：
//   - key: 包含公钥 pk = g_2^α 的密钥
//   - h: 哈希值（*big.Int 形式）
//   - m: 消息
//   - r: 随机数 r（*big.Int 形式，需要反序列化为 G1 点）
//
// 输出：
//   - bool: 验证是否通过
//
// 验证公式：h == e(r, g_2) * e(H_1(m), pk)
//
// 调用时机：
//   - 步骤4：任何人可以使用公开的 pk 验证修改后的哈希
//   - 在区块链中，用于验证区块哈希的正确性
//
// 使用全局 pairing 并加锁，与交易级随机数派生一致，避免并发导致 kilic 库内部临时变量被覆盖。
func Verify(key *NITCHKey, h *big.Int, m Message, r *big.Int) bool {
	if h == nil || r == nil {
		return false
	}

	// 全程在全局 pairing 上计算，并加锁避免并发
	pairingMu.Lock()
	defer pairingMu.Unlock()
	start := time.Now()
	// 将 r 从 big.Int 反序列化为 G1 点（在已持锁下使用 Unlocked 版本，避免自死锁）
	rPoint, err := bigIntToG1PointUnlocked(r)
	if err != nil {
		return false
	}

	// 计算 H_1(m)，在已持锁下调用内部实现
	h1m, err := hashToG1Unlocked(m)
	if err != nil {
		return false
	}

	// 计算期望的哈希值：h = e(r, g_2) * e(H_1(m), pk)，使用全局 pairing
	pairing.Reset()
	pairing.AddPair(rPoint, key.PP.g2)
	pairing.AddPair(h1m, key.GlobalPK)
	expectedResult := pairing.Result()
	// 转换为 big.Int（GT/fe12 完整序列化，576 字节）
	expectedBigInt := fe12ToBigInt(expectedResult)

	// 兼容两种调用口径：
	//  1. 普通 chameleon 包调用传完整 GT 序列化 big.Int（如 HashOutput.Hash）；
	//  2. 区块路径传 common.BigToHash(hashOutput.Hash) 的末 32 字节截断值。
	expectedTrunc := truncateToHashBytes(expectedBigInt)
	cost := time.Since(start)
	log.Info("Hash verification completed", "time", cost)
	return h.Cmp(expectedBigInt) == 0 || h.Cmp(expectedTrunc) == 0
}

// truncateToHashBytes 精确复刻 common.Hash.SetBytes 的"末 32 字节"语义：
// 当 big.Int 的字节长度超过 32 时，保留末 32 字节（从左裁剪），返回新的 big.Int。
// 用于兼容 core/types/block.go: common.BigToHash(hashOutput.Hash) 的区块哈希口径。
func truncateToHashBytes(n *big.Int) *big.Int {
	b := n.Bytes()
	const hashLen = 32
	if len(b) > hashLen {
		b = b[len(b)-hashLen:]
	}
	return new(big.Int).SetBytes(b)
}

// ====================== Adaptation ======================

// PartialAdapt 实现步骤2：成员独立计算部分份额 c_i
//
// 算法：PartialAdapt（论文中的 PartialAdapt 算法）
// 输入：
//   - key: 委员会成员 P_i 的密钥（包含 sk_i = F(i) 和 vk_i = g_1^{sk_i}）
//   - m: 原始消息
//   - mPrime: 新消息 m'
//
// 输出：
//   - AdaptationShare: 投票 s_i = (i, c_i, π_i)，其中：
//   - Index: 成员索引 i
//   - Share: c_i = y^{sk_i}，y^α 的份额
//   - Proof: π_i，零知识证明
//
// 步骤：
//  1. 计算 y = H_1(m) * H_1(m')^{-1} ∈ G_1
//  2. 计算 c_i = y^{sk_i} ∈ G_1（这是 y^α 在指数上的份额）
//  3. 生成 NIZK 证明 π_i，证明 c_i 计算正确（即证明知道 sk_i 使得 c_i = y^{sk_i} 且 vk_i = g_1^{sk_i}）
//
// 调用时机：每个委员会成员 P_i 独立调用此方法，无需与其他成员通信
//
// 输入校验在此完成，随后分发到内部 seam（purePartialAdapt）。
func PartialAdapt(key *NITCHKey, m, mPrime Message) (*AdaptationShare, error) {
	return seamPartialAdapt(key, m, mPrime)
}

// PartialVerify 验证步骤2的输出：验证成员 P_i 的投票 s_i = (i, c_i, π_i)
//
// 算法：PartialVerify
// 输入：
//   - key: 成员 P_i 的公钥信息（包含 vk_i）
//   - share: 投票 s_i = (i, c_i, π_i)
//   - m, mPrime: 原始消息和新消息
//
// 输出：
//   - bool: 验证是否通过
//
// 验证内容：
//  1. 验证 NIZK 证明 π_i 的正确性
//  2. 验证 c_i 与消息 m, m' 的一致性
//
// 调用时机：收集者在组合份额前，验证每个投票的有效性
//
// 输入校验在此完成，随后分发到内部 seam（purePartialVerify）。
func PartialVerify(key *NITCHKey, share *AdaptationShare, m, mPrime Message) bool {
	return seamPartialVerify(key, share, m, mPrime)
}

// ====================== Combination ======================

// Combine 实现步骤3：收集者组合出 r'
//
// 算法：Combine（论文中的 Combine 算法）
// 输入：
//   - pp: 公开参数
//   - shares: 至少 t+1 个有效的投票 s_i = (i, c_i, π_i)
//   - t: 阈值（多项式次数）
//   - hashOutput: 原始哈希输出（包含 h 和 r）
//   - m, mPrime: 原始消息和新消息
//
// 输出：
//   - *big.Int: 新的随机数 r'（作为 big.Int），使得 h = e(r', g_2) * e(H_1(m'), pk)
//
// 步骤：
//  1. 验证：对每个投票 s_i，使用 PartialVerify 验证其正确性
//  2. 寻找足够集合：找到一个大小至少为 t+1 的索引集合 I，其中所有投票验证通过
//  3. 指数上的拉格朗日插值：
//     - 计算拉格朗日基系数 λ_i^I = Π_{j∈I, j≠i} (0 - j) / (i - j) mod p
//     - 计算 y^α = G(0) = Π_{i∈I} (c_i)^{λ_i^I}
//  4. 计算最终碰撞值：r' = r * y^α
//
// 调用时机：收集者（例如矿工）收集到至少 t+1 个有效投票后调用
func Combine(pp *PublicParameters, shares []*AdaptationShare, t int, numParties int,
	hashOutput *HashOutput, m, mPrime Message) (*big.Int, error) {
	if !isPowerOfTwo(numParties) {
		return nil, fmt.Errorf("numParties must be a power of two")
	}
	if pp == nil || pp.p == nil {
		return nil, ErrInvalidParameters
	}
	// 步骤1：检查是否有足够的份额
	if len(shares) < t+1 {
		return nil, ErrInsufficientShares
	}

	// 步骤2：验证所有份额
	// 注意：在实际实现中，需要根据 share.Index 找到对应的 key 进行验证
	// 这里假设份额已经通过 PartialVerify 验证过
	// 如果需要在此处验证，可以使用 CombineWithVerification 函数

	// 步骤3：计算 y = H_1(m) * H_1(m')^{-1}
	// 以下所有 pairing.G1 运算在 pairingMu 保护下进行，避免 kilic 引擎 scratch 被并发覆盖
	// 导致 FromBytes/ToBytes 偶发误判（如虚假的 "point is not on curve"）。
	pairingMu.Lock()
	defer pairingMu.Unlock()

	h1m, err1 := hashToG1Unlocked(m)
	h1mPrime, err2 := hashToG1Unlocked(mPrime)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("failed to hash messages")
	}

	y := pairing.G1.New()
	invH1mPrime := pairing.G1.New()
	pairing.G1.Neg(invH1mPrime, h1mPrime)
	pairing.G1.Add(y, h1m, invH1mPrime)

	// 步骤4：选择 t+1 个有效份额（在实际应用中应该选择已验证通过的份额）
	validShares := shares[:t+1]

	// 步骤5：收集索引并映射到单位根域点 x_i = ω^i
	indices := make([]*big.Int, len(validShares))
	seenIndices := make(map[int]struct{}, len(validShares))
	for i, share := range validShares {
		if share == nil || share.Index == nil {
			return nil, fmt.Errorf("share index is nil")
		}
		idx, err := intFromBig(share.Index, "share.Index")
		if err != nil {
			return nil, err
		}
		if _, exists := seenIndices[idx]; exists {
			return nil, fmt.Errorf("duplicate share index: %d", idx)
		}
		seenIndices[idx] = struct{}{}
		xi, err := evaluationPointForIndex(pp.p, numParties, idx)
		if err != nil {
			return nil, err
		}
		indices[i] = xi
	}

	// 步骤6：计算拉格朗日基系数 λ_i^I（在点 0 处）
	// λ_i^I = Π_{j∈I, j≠i} (0 - ω^j) / (ω^i - ω^j) mod p
	coeffs, err := lagrangeCoefficientsAtZeroGnarkFFT(indices, pp.p)
	if err != nil {
		return nil, fmt.Errorf("failed to compute Lagrange coefficients: %w", err)
	}

	// 步骤7：使用拉格朗日插值计算 y^α = G(0) = Π_{i∈I} (c_i)^{λ_i^I}
	// 在 G_1 群中，c_i^{λ_i} 等价于 c_i * λ_i（标量乘法）
	yAlpha := pairing.G1.Zero() // 初始化为单位元（在加法群中是零元）

	for i, share := range validShares {
		if share.Share == nil {
			return nil, fmt.Errorf("share point is nil for share %d", i)
		}
		// 计算 c_i^{λ_i} = share * λ_i（标量乘法）
		temp := pairing.G1.New()
		coeffFr := bigIntToFr(coeffs[i])
		pairing.G1.MulScalar(temp, share.Share, coeffFr)

		// 累加：y^α = y^α + c_i^{λ_i}
		pairing.G1.Add(yAlpha, yAlpha, temp)
	}

	// 步骤8：计算最终碰撞值 r' = r * y^α
	// 首先需要将 hashOutput.CheckValue (big.Int) 转换为 G1 点
	rPoint, err := bigIntToG1PointUnlocked(hashOutput.CheckValue)
	if err != nil {
		return nil, fmt.Errorf("failed to convert r to G1 point: %v", err)
	}

	// 在 G_1 群中，乘法运算等价于加法运算
	rPrimePoint := pairing.G1.New()
	pairing.G1.Add(rPrimePoint, rPoint, yAlpha) // r' = r + y^α = r * y^α

	// 将结果转换为 big.Int
	rPrime := g1PointToBigIntUnlocked(rPrimePoint)

	return rPrime, nil
}

// ====================== Helper Functions ======================

// intFromBig converts *big.Int to int with basic bounds checking.
func intFromBig(v *big.Int, name string) (int, error) {
	if v == nil {
		return 0, fmt.Errorf("%s is nil", name)
	}
	if !v.IsInt64() {
		return 0, fmt.Errorf("%s overflows int64", name)
	}
	x := v.Int64()
	if x > int64(int(^uint(0)>>1)) || x < -int64(int(^uint(0)>>1))-1 {
		return 0, fmt.Errorf("%s overflows int", name)
	}
	return int(x), nil
}

// bigIntToFr converts a big.Int to bls12381.Fr
func bigIntToFr(n *big.Int) *bls12381.Fr {
	fr := bls12381.NewFr()
	// Convert big.Int to bytes and then to Fr
	bytes := n.Bytes()
	// Pad to 48 bytes (Fr size for BLS12-381) if needed
	if len(bytes) < 48 {
		padded := make([]byte, 48)
		copy(padded[48-len(bytes):], bytes)
		bytes = padded
	} else if len(bytes) > 48 {
		bytes = bytes[:48]
	}
	fr.FromBytes(bytes)
	return fr
}

// hashToG1 / hashToG1Unlocked / hashToScalar live in hashseam_pure.go:
// they are seam-internal primitives (H1/H2) shared by the protocol seam.

// generateNIZKProof generates a NIZK proof for DH tuple
// 调用方必须已持有 pairingMu（内部直接使用 pairing.G1 运算 + serializeForChallenge）。
func generateNIZKProof(key *NITCHKey, y, c *bls12381.PointG1) (*NIZKProof, error) {
	// Σ-protocol for equality of discrete logs:
	// Prove knowledge of sk_i such that vk_i = g1^{sk_i} AND c_i = y^{sk_i}

	// 1. Prover chooses random τ
	τ := randomScalar(key.PP.p)
	τFr := bigIntToFr(τ)

	// 2. Compute commitments: T1 = g1^τ, T2 = y^τ
	T1 := pairing.G1.New()
	pairing.G1.MulScalar(T1, key.PP.g1, τFr)

	T2 := pairing.G1.New()
	pairing.G1.MulScalar(T2, y, τFr)

	// 3. Compute challenge: ω = H(g1, y, vk_i, c_i, T1, T2)
	challengeData := serializeForChallenge(key.PP.g1, y, key.VerificationKey, c, T1, T2)
	ω := hashToScalar(challengeData, key.PP.p)

	// 4. Compute response: z = τ + ω * sk_i mod p
	z := new(big.Int).Mul(ω, key.SecretShare)
	z.Add(z, τ)
	z.Mod(z, key.PP.p)

	return &NIZKProof{
		Omega: ω,
		Z:     z,
	}, nil
}

// verifyNIZKProof verifies a NIZK proof
func verifyNIZKProof(key *NITCHKey, share *AdaptationShare, m, mPrime Message) bool {
	// 以下所有 pairing.G1/G2 运算（含 serializeForChallenge，其内部调用 ToBytes）在 pairingMu 保护下进行。
	pairingMu.Lock()
	defer pairingMu.Unlock()
	// Recompute y
	h1m, err1 := hashToG1Unlocked(m)
	h1mPrime, err2 := hashToG1Unlocked(mPrime)
	if err1 != nil || err2 != nil {
		return false
	}

	y := pairing.G1.New()
	invH1mPrime := pairing.G1.New()
	pairing.G1.Neg(invH1mPrime, h1mPrime)
	pairing.G1.Add(y, h1m, invH1mPrime)

	// Reconstruct T1 and T2 from the proof
	// T1' = g1^z / vk_i^ω
	// T2' = y^z / c_i^ω

	// Compute g1^z
	g1z := pairing.G1.New()
	zFr := bigIntToFr(share.Proof.Z)
	pairing.G1.MulScalar(g1z, key.PP.g1, zFr)

	// Compute vk_i^ω
	vkOmega := pairing.G1.New()
	omegaFr := bigIntToFr(share.Proof.Omega)
	pairing.G1.MulScalar(vkOmega, key.VerificationKey, omegaFr)

	// Compute T1' = g1^z / vk_i^ω
	T1Prime := pairing.G1.New()
	pairing.G1.Sub(T1Prime, g1z, vkOmega) // Division in additive notation

	// Compute y^z
	yz := pairing.G1.New()
	pairing.G1.MulScalar(yz, y, zFr)

	// Compute c_i^ω
	cOmega := pairing.G1.New()
	pairing.G1.MulScalar(cOmega, share.Share, omegaFr)

	// Compute T2' = y^z / c_i^ω
	T2Prime := pairing.G1.New()
	pairing.G1.Sub(T2Prime, yz, cOmega)

	// Recompute challenge
	challengeData := serializeForChallenge(key.PP.g1, y, key.VerificationKey, share.Share, T1Prime, T2Prime)
	expectedOmega := hashToScalar(challengeData, key.PP.p)

	// Verify challenge matches
	return share.Proof.Omega.Cmp(expectedOmega) == 0
}

// lagrangeCoefficientsAtZeroGnarkFFT computes coefficients using gnark-crypto
// FFT for root polynomial construction.
func lagrangeCoefficientsAtZeroGnarkFFT(indices []*big.Int, p *big.Int) ([]*big.Int, error) {
	n := len(indices)
	if n == 0 {
		return nil, fmt.Errorf("empty indices")
	}

	points := make([]gnarkfr.Element, n)
	for i, v := range indices {
		if v == nil {
			return nil, fmt.Errorf("nil index point")
		}
		points[i] = bigIntToGnarkFr(v)
		if points[i].IsZero() {
			return nil, fmt.Errorf("evaluation point is zero")
		}
	}

	poly, err := buildRootPolynomialFFT(points)
	if err != nil {
		return nil, err
	}
	if len(poly) == 0 {
		return nil, fmt.Errorf("empty root polynomial")
	}

	derivative := derivativePolynomial(poly)
	p0 := poly[0]
	p0.Neg(&p0) // λ_i(0) = -P(0)/(x_i * P'(x_i))

	denoms := make([]gnarkfr.Element, n)
	for i := range points {
		dpi := evalPolynomialGnark(derivative, &points[i])
		if dpi.IsZero() {
			return nil, fmt.Errorf("zero derivative at index %d", i)
		}
		denoms[i].Mul(&points[i], &dpi)
		if denoms[i].IsZero() {
			return nil, fmt.Errorf("non-invertible denominator at index %d", i)
		}
	}

	invDenoms, err := batchInverseGnark(denoms)
	if err != nil {
		return nil, err
	}

	coeffs := make([]*big.Int, n)
	var checksum gnarkfr.Element
	checksum.SetZero()
	for i := range points {
		var coeff gnarkfr.Element
		coeff.Mul(&p0, &invDenoms[i])
		coeffs[i] = gnarkFrToBigInt(&coeff)
		checksum.Add(&checksum, &coeff)
	}
	if !checksum.IsOne() {
		return nil, fmt.Errorf("lagrange checksum mismatch")
	}

	return coeffs, nil
}

func buildRootPolynomialFFT(points []gnarkfr.Element) ([]gnarkfr.Element, error) {
	if len(points) == 0 {
		return nil, fmt.Errorf("no points")
	}
	polys := make([][]gnarkfr.Element, len(points))
	for i := range points {
		polys[i] = make([]gnarkfr.Element, 2)
		polys[i][0].Neg(&points[i])
		polys[i][1].SetOne()
	}

	for len(polys) > 1 {
		next := make([][]gnarkfr.Element, 0, (len(polys)+1)/2)
		for i := 0; i < len(polys); i += 2 {
			if i+1 == len(polys) {
				next = append(next, polys[i])
				continue
			}
			prod := multiplyPolynomialsFFT(polys[i], polys[i+1])
			next = append(next, prod)
		}
		polys = next
	}

	return polys[0], nil
}

func multiplyPolynomialsFFT(a, b []gnarkfr.Element) []gnarkfr.Element {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	resultSize := len(a) + len(b) - 1
	n := 1
	for n < resultSize {
		n <<= 1
	}

	A := make([]gnarkfr.Element, n)
	B := make([]gnarkfr.Element, n)
	copy(A, a)
	copy(B, b)

	domain := gnarkfft.NewDomain(uint64(n))
	domain.FFT(A, gnarkfft.DIF)
	domain.FFT(B, gnarkfft.DIF)

	for i := range A {
		A[i].Mul(&A[i], &B[i])
	}

	domain.FFTInverse(A, gnarkfft.DIT)
	return A[:resultSize]
}

func derivativePolynomial(poly []gnarkfr.Element) []gnarkfr.Element {
	if len(poly) <= 1 {
		return []gnarkfr.Element{{}}
	}
	derivative := make([]gnarkfr.Element, len(poly)-1)
	for i := 1; i < len(poly); i++ {
		var k gnarkfr.Element
		k.SetUint64(uint64(i))
		derivative[i-1].Mul(&poly[i], &k)
	}
	return derivative
}

func evalPolynomialGnark(coeffs []gnarkfr.Element, x *gnarkfr.Element) gnarkfr.Element {
	if len(coeffs) == 0 {
		var z gnarkfr.Element
		z.SetZero()
		return z
	}
	res := coeffs[len(coeffs)-1]
	for i := len(coeffs) - 2; i >= 0; i-- {
		res.Mul(&res, x)
		res.Add(&res, &coeffs[i])
	}
	return res
}

func batchInverseGnark(values []gnarkfr.Element) ([]gnarkfr.Element, error) {
	n := len(values)
	if n == 0 {
		return nil, fmt.Errorf("empty batch")
	}
	prefix := make([]gnarkfr.Element, n)
	prefix[0] = values[0]
	if prefix[0].IsZero() {
		return nil, fmt.Errorf("non-invertible zero value")
	}
	for i := 1; i < n; i++ {
		if values[i].IsZero() {
			return nil, fmt.Errorf("non-invertible zero value")
		}
		prefix[i].Mul(&prefix[i-1], &values[i])
	}

	var invAll gnarkfr.Element
	invAll.Inverse(&prefix[n-1])

	res := make([]gnarkfr.Element, n)
	for i := n - 1; i >= 0; i-- {
		if i == 0 {
			res[i] = invAll
		} else {
			res[i].Mul(&invAll, &prefix[i-1])
		}
		invAll.Mul(&invAll, &values[i])
	}

	return res, nil
}

func bigIntToGnarkFr(v *big.Int) gnarkfr.Element {
	var e gnarkfr.Element
	e.SetBigInt(v)
	return e
}

func gnarkFrToBigInt(v *gnarkfr.Element) *big.Int {
	out := new(big.Int)
	v.BigInt(out)
	return out
}

// isPowerOfTwo returns true if n is a positive power of two.
func isPowerOfTwo(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// getPrimitiveNthRootOfUnity finds and caches a primitive n-th root of unity in F_p.
func getPrimitiveNthRootOfUnity(p *big.Int, n int) (*big.Int, error) {
	if !isPowerOfTwo(n) {
		return nil, fmt.Errorf("n must be a power of two")
	}

	rootCache.mu.RLock()
	if w, ok := rootCache.byN[n]; ok {
		rootCache.mu.RUnlock()
		return new(big.Int).Set(w), nil
	}
	rootCache.mu.RUnlock()

	one := big.NewInt(1)
	pMinusOne := new(big.Int).Sub(p, one)
	nBig := big.NewInt(int64(n))
	if new(big.Int).Mod(pMinusOne, nBig).Sign() != 0 {
		return nil, fmt.Errorf("n does not divide p-1")
	}

	exp := new(big.Int).Div(pMinusOne, nBig)
	half := n / 2
	halfBig := big.NewInt(int64(half))

	for g := int64(2); g < 1<<20; g++ {
		candidate := new(big.Int).Exp(big.NewInt(g), exp, p)
		if candidate.Sign() == 0 {
			continue
		}
		if new(big.Int).Exp(candidate, nBig, p).Cmp(one) != 0 {
			continue
		}
		if half > 0 && new(big.Int).Exp(candidate, halfBig, p).Cmp(one) == 0 {
			continue
		}

		rootCache.mu.Lock()
		rootCache.byN[n] = new(big.Int).Set(candidate)
		rootCache.mu.Unlock()
		return candidate, nil
	}

	return nil, fmt.Errorf("failed to find primitive %d-th root of unity", n)
}

// evaluationPointForIndex maps a 1-based party index j to x_j = omega^j.
func evaluationPointForIndex(p *big.Int, n int, index int) (*big.Int, error) {
	if index < 1 || index > n {
		return nil, fmt.Errorf("index must be in [1,n]")
	}
	omega, err := getPrimitiveNthRootOfUnity(p, n)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Exp(omega, big.NewInt(int64(index)), p), nil
}

// evaluatePolynomial evaluates polynomial at point x
func evaluatePolynomial(coeffs []*big.Int, x *big.Int, p *big.Int) *big.Int {
	result := big.NewInt(0)
	xPower := big.NewInt(1)

	for _, coeff := range coeffs {
		// term = coeff * x^i
		term := new(big.Int).Mul(coeff, xPower)
		term.Mod(term, p)

		result.Add(result, term)
		result.Mod(result, p)

		// Update xPower for next iteration
		xPower.Mul(xPower, x)
		xPower.Mod(xPower, p)
	}

	return result
}

// randomScalar generates a random scalar in [1, p-1] using crypto/rand
func randomScalar(p *big.Int) *big.Int {
	bitLen := p.BitLen()
	for {
		b := make([]byte, (bitLen+7)/8+1)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		n := new(big.Int).SetBytes(b)
		n.Mod(n, p)
		if n.Sign() == 0 {
			continue
		}
		return n
	}
}

// randomPointG1 generates a random point in G1
func randomPointG1(pp *PublicParameters) *bls12381.PointG1 {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	scalar := randomScalar(pp.p)
	scalarFr := bigIntToFr(scalar)
	point := pairing.G1.New()
	pairing.G1.MulScalar(point, pp.g1, scalarFr)
	return point
}
func RandomPointG1(pp *PublicParameters) *big.Int {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	scalar := randomScalar(pp.p)
	scalarFr := bigIntToFr(scalar)
	point := pairing.G1.New()
	pairing.G1.MulScalar(point, pp.g1, scalarFr)
	return g1PointToBigIntUnlocked(point)
}

// serializeForChallenge serializes points for challenge computation
// 调用方必须已持有 pairingMu（内部使用 pairing.G1/G2.ToBytes）。
func serializeForChallenge(points ...interface{}) []byte {
	var data []byte
	for _, p := range points {
		switch v := p.(type) {
		case *bls12381.PointG1:
			bytes := pairing.G1.ToBytes(v)
			data = append(data, bytes...)
		case *bls12381.PointG2:
			bytes := pairing.G2.ToBytes(v)
			data = append(data, bytes...)
		default:
			// For simplicity
			data = append(data, []byte(fmt.Sprintf("%v", v))...)
		}
	}
	return data
}

// equalBytes compares two byte slices
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ComputeInverseShare 计算逆元份额 inv_i = sk^{-1}/n (mod q)
// 用于非交互式阈值变色龙哈希的委员会配置
func ComputeInverseShare(sk *big.Int, n int) *big.Int {
	if sk == nil || n <= 0 {
		return nil
	}
	// 获取群阶（BLS12-381 scalar field order）
	q := pairing.G1.Q()

	// 计算 sk^{-1} mod q
	skInv := new(big.Int).ModInverse(sk, q)
	if skInv == nil {
		return nil
	}

	// 计算 n^{-1} mod q
	nBig := big.NewInt(int64(n))
	nInv := new(big.Int).ModInverse(nBig, q)
	if nInv == nil {
		return nil
	}

	// 计算 inv_i = sk^{-1} * n^{-1} mod q = sk^{-1}/n mod q
	invShare := new(big.Int).Mul(skInv, nInv)
	invShare.Mod(invShare, q)

	return invShare
}

// fe12ToBigInt 将 fe12（GT 群的内部表示）转换为 big.Int
func fe12ToBigInt(fe *bls12381.E) *big.Int {
	// fe12 是 E 类型，包含 2 个 fe6 元素，每个 fe6 包含 2 个 fe2 元素
	// 使用 GT 的 ToBytes 方法序列化
	gt := pairing.GT()
	gtBytes := gt.ToBytes(fe)
	return new(big.Int).SetBytes(gtBytes)
}

// g1PointToBigIntUnlocked 将 G1 点转换为 big.Int（调用方必须已持有 pairingMu）。
//
// kilic 的 G1.ToCompressed 内部会调用 Affine，使用引擎的共享 scratch 缓冲（G1.t），
// 因此必须在 pairingMu 保护下调用，否则并发会覆盖 scratch 导致序列化结果错误。
//
// ToCompressed 返回标准 48 字节编码；compression flag 位保证 big.Int.Bytes()
// 不会丢失编码宽度。
func g1PointToBigIntUnlocked(point *bls12381.PointG1) *big.Int {
	bytes := pairing.G1.ToCompressed(point)
	return new(big.Int).SetBytes(bytes)
}

// g1PointToBigInt 将 G1 点转换为 big.Int（线程安全：内部加 pairingMu）。
// 已持有 pairingMu 的调用方应直接使用 g1PointToBigIntUnlocked 以避免自死锁。
func g1PointToBigInt(point *bls12381.PointG1) *big.Int {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return g1PointToBigIntUnlocked(point)
}

// bigIntToG1PointUnlocked 将 big.Int 反序列化为 G1 点（调用方必须已持有 pairingMu）。
//
// kilic 的 G1.FromCompressed 会执行曲线与子群检查，并读取引擎共享 scratch 缓冲。
// 若不持锁，并发 goroutine 的其它 G1 运算（MulScalar/Add/Affine/...）会覆盖 G1.t，
// 使 IsOnCurve 对“合法在曲线上的点”返回 false，进而抛出虚假的 "point is not on curve"。
// 这正是 edit 校验路径偶发失败的根因。
func bigIntToG1PointUnlocked(n *big.Int) (*bls12381.PointG1, error) {
	if n == nil || n.Sign() <= 0 {
		return nil, fmt.Errorf("invalid compressed G1 point: nil or non-positive value")
	}
	bytes := n.Bytes()
	if len(bytes) != g1CompressedPointByteSize {
		return nil, fmt.Errorf("invalid compressed G1 point length: got %d, want %d", len(bytes), g1CompressedPointByteSize)
	}

	// 从字节反序列化
	point, err := pairing.G1.FromCompressed(bytes)
	if err != nil {
		// 提供更详细的错误信息，包括前16字节用于调试。
		firstBytes := []byte{}
		if len(bytes) >= 16 {
			firstBytes = bytes[:16]
		} else {
			firstBytes = bytes
		}
		return nil, fmt.Errorf("failed to deserialize compressed G1 point (bytes: %d, first 16 bytes: %x): %v",
			len(bytes), firstBytes, err)
	}

	return point, nil
}

// bigIntToG1Point 将 big.Int 反序列化为 G1 点（线程安全：内部加 pairingMu）。
// 已持有 pairingMu 的调用方应直接使用 bigIntToG1PointUnlocked 以避免自死锁。
func bigIntToG1Point(n *big.Int) (*bls12381.PointG1, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return bigIntToG1PointUnlocked(n)
}

// ====================== Serialization ======================

// SerializedPublicParameters 用于序列化的公开参数结构
type SerializedPublicParameters struct {
	G1Bytes []byte `json:"g1_bytes"`
	G2Bytes []byte `json:"g2_bytes"`
	P       string `json:"p"` // big.Int 的十六进制字符串
}

// SerializedNITCHKey 用于序列化的NITCH密钥结构
type SerializedNITCHKey struct {
	Threshold       int    `json:"threshold"`
	NumParties      int    `json:"num_parties"`
	SecretShare     string `json:"secret_share"`     // big.Int 的十六进制字符串
	VerificationKey []byte `json:"verification_key"` // G1 点序列化
	Index           int    `json:"index"`
	GlobalPKBytes   []byte `json:"global_pk_bytes"`    // G2 点序列化
	GlobalPKG1Bytes []byte `json:"global_pk_g1_bytes"` // G1 点序列化
}

// SerializedKeySet 用于序列化的完整密钥集合
type SerializedKeySet struct {
	PublicParams SerializedPublicParameters `json:"public_params"`
	Keys         []SerializedNITCHKey       `json:"keys"`
	Threshold    int                        `json:"threshold"`
	NumParties   int                        `json:"num_parties"`
}

// SerializeKeySet 序列化密钥集合到JSON
func SerializeKeySet(pp *PublicParameters, keys []*NITCHKey) ([]byte, error) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	// 序列化公开参数
	g1Bytes := pairing.G1.ToBytes(pp.g1)
	g2Bytes := pairing.G2.ToBytes(pp.g2)

	serializedPP := SerializedPublicParameters{
		G1Bytes: g1Bytes,
		G2Bytes: g2Bytes,
		P:       pp.p.Text(16),
	}

	// 序列化所有密钥
	serializedKeys := make([]SerializedNITCHKey, len(keys))
	for i, key := range keys {
		vkBytes := pairing.G1.ToBytes(key.VerificationKey)
		pkBytes := pairing.G2.ToBytes(key.GlobalPK)
		pkG1Bytes := pairing.G1.ToBytes(key.GlobalPKG1)

		serializedKeys[i] = SerializedNITCHKey{
			Threshold:       key.Threshold,
			NumParties:      key.NumParties,
			SecretShare:     key.SecretShare.Text(16),
			VerificationKey: vkBytes,
			Index:           key.Index,
			GlobalPKBytes:   pkBytes,
			GlobalPKG1Bytes: pkG1Bytes,
		}
	}

	keySet := SerializedKeySet{
		PublicParams: serializedPP,
		Keys:         serializedKeys,
		Threshold:    keys[0].Threshold,
		NumParties:   keys[0].NumParties,
	}

	return json.Marshal(keySet)
}

// DeserializeKeySet 从JSON反序列化密钥集合
func DeserializeKeySet(data []byte) (*PublicParameters, []*NITCHKey, error) {
	var keySet SerializedKeySet
	if err := json.Unmarshal(data, &keySet); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal key set: %v", err)
	}

	// 反序列化涉及全局 pairing 的 FromBytes，需持 pairingMu
	pairingMu.Lock()
	defer pairingMu.Unlock()

	// 反序列化公开参数
	g1, err := pairing.G1.FromBytes(keySet.PublicParams.G1Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to deserialize G1: %v", err)
	}

	g2, err := pairing.G2.FromBytes(keySet.PublicParams.G2Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to deserialize G2: %v", err)
	}

	p, ok := new(big.Int).SetString(keySet.PublicParams.P, 16)
	if !ok {
		return nil, nil, fmt.Errorf("failed to parse prime order")
	}

	pp := &PublicParameters{
		g1: g1,
		g2: g2,
		gt: pairing.GT(),
		p:  p,
	}

	// 反序列化所有密钥
	keys := make([]*NITCHKey, len(keySet.Keys))
	for i, sk := range keySet.Keys {
		vk, err := pairing.G1.FromBytes(sk.VerificationKey)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to deserialize verification key %d: %v", i, err)
		}

		pk, err := pairing.G2.FromBytes(sk.GlobalPKBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to deserialize global PK %d: %v", i, err)
		}

		pkG1, err := pairing.G1.FromBytes(sk.GlobalPKG1Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to deserialize global PK G1 %d: %v", i, err)
		}

		secretShare, ok := new(big.Int).SetString(sk.SecretShare, 16)
		if !ok {
			return nil, nil, fmt.Errorf("failed to parse secret share %d", i)
		}

		keys[i] = &NITCHKey{
			PP:              pp,
			GlobalPK:        pk,
			GlobalPKG1:      pkG1,
			Threshold:       sk.Threshold,
			NumParties:      sk.NumParties,
			SecretShare:     secretShare,
			VerificationKey: vk,
			Index:           sk.Index,
		}
	}

	return pp, keys, nil
}
