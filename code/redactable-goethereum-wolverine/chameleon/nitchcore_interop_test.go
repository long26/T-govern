// nitchcore_interop_test.go — pure-side differential and algebra layer of the
// nitchcore interop test suite.
//
// This file carries NO build tag: every test here runs under both the default
// (pure seam) build and the nitchcgo build. It pins three things the cgo A/B
// layer (nitchcore_interop_cgo_test.go) builds on:
//
//  1. the root-of-unity domain math (evaluationPointForIndex vs omega^j) that
//     the Go Combine/extract paths share with the C++ deal creation;
//  2. the H1/H2 hash primitives against the live C++ oracle binary, extending
//     the three fixed goldens in hashseam_pure_test.go to ten varied messages;
//  3. the threshold algebra end-to-end in pure Go (g1^sk == vk, and Lagrange
//     interpolation of any t+1 verification keys reproduces GlobalPKG1).
package chameleon

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// ====================== 1. evaluation-domain consistency ======================

// TestEvalPointEquivalence pins evaluationPointForIndex to x_j = omega^j for
// every committee size the protocol admits (n a power of two up to 128) and
// every 1-based party index. Both deal creation (pure and C++) and the Go
// Combine/extract interpolation consume the same domain points; this test
// pins the Go-side helper against an independently computed power of the
// primitive root, plus the defining primitivity conditions.
func TestEvalPointEquivalence(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	for _, n := range []int{2, 4, 8, 16, 32, 64, 128} {
		n := n
		t.Run(itoa(n), func(t *testing.T) {
			omega, err := getPrimitiveNthRootOfUnity(pp.p, n)
			if err != nil {
				t.Fatalf("getPrimitiveNthRootOfUnity(n=%d) failed: %v", n, err)
			}
			one := big.NewInt(1)
			// Primitivity sanity: omega^n == 1 and omega^(n/2) != 1.
			if new(big.Int).Exp(omega, big.NewInt(int64(n)), pp.p).Cmp(one) != 0 {
				t.Fatalf("omega^n != 1 for n=%d", n)
			}
			if new(big.Int).Exp(omega, big.NewInt(int64(n/2)), pp.p).Cmp(one) == 0 {
				t.Fatalf("omega^(n/2) == 1 for n=%d: root is not primitive", n)
			}
			for j := 1; j <= n; j++ {
				xj, err := evaluationPointForIndex(pp.p, n, j)
				if err != nil {
					t.Fatalf("evaluationPointForIndex(n=%d, j=%d) failed: %v", n, j, err)
				}
				want := new(big.Int).Exp(omega, big.NewInt(int64(j)), pp.p)
				if xj.Cmp(want) != 0 {
					t.Fatalf("evaluationPointForIndex(n=%d, j=%d) = %s, want omega^j = %s",
						n, j, xj.String(), want.String())
				}
			}
		})
	}
}

// itoa is a tiny local strconv.Itoa stand-in kept inline to limit imports.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ====================== 2. live H1/H2 oracle vectors ======================

// h1OraclePath resolves the standalone C++ oracle binary
// (chameleon/nitchcore/tools/h1_oracle, built by `make smoke`). It returns ""
// when the binary is absent so callers can skip gracefully.
func h1OraclePath(t *testing.T) string {
	t.Helper()
	var candidates []string
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(file), "nitchcore", "tools", "h1_oracle"))
	}
	candidates = append(candidates, filepath.Join("nitchcore", "tools", "h1_oracle"))
	for _, c := range candidates {
		st, err := os.Stat(c)
		if err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return c
		}
	}
	return ""
}

// h1OracleVector is one parsed oracle record: the raw message plus the C++
// H1 point (96B kilic-uncompressed) and H2 scalar (48B, i.e. MIRACL
// scalar_to_bytes width with 16 leading zero bytes).
type h1OracleVector struct {
	msg Message
	h1  []byte
	h2  []byte
}

var h1OracleRe = regexp.MustCompile(`(?s)msg=(.*?)\nH1=([0-9a-f]+)\nH2=([0-9a-f]+)`)

// runH1Oracle executes the oracle for the given messages and parses its output
// in argument order.
func runH1Oracle(t *testing.T, binary string, msgs []Message) []h1OracleVector {
	t.Helper()
	args := make([]string, len(msgs))
	for i, m := range msgs {
		args[i] = string(m)
	}
	out, err := exec.Command(binary, args...).Output()
	if err != nil {
		t.Fatalf("h1_oracle execution failed: %v", err)
	}
	matches := h1OracleRe.FindAllStringSubmatch(string(out), -1)
	if len(matches) != len(msgs) {
		t.Fatalf("h1_oracle produced %d records, want %d (raw output:\n%s)", len(matches), len(msgs), out)
	}
	vectors := make([]h1OracleVector, len(msgs))
	for i, m := range matches {
		h1, err := hex.DecodeString(m[2])
		if err != nil {
			t.Fatalf("record %d: bad H1 hex: %v", i, err)
		}
		h2, err := hex.DecodeString(m[3])
		if err != nil {
			t.Fatalf("record %d: bad H2 hex: %v", i, err)
		}
		vectors[i] = h1OracleVector{msg: Message(m[1]), h1: h1, h2: h2}
	}
	return vectors
}

// interopPlainMsg builds the deterministic plain-text message of length L used
// by the extended golden set (varied lengths around the SHA-256 block and
// XMD boundaries).
func interopPlainMsg(l int) Message {
	m := make(Message, l)
	for i := range m {
		m[i] = byte('a' + (i*13+l)%26)
	}
	return m
}

// TestGoldenH1H2MoreVectors extends the three fixed golden vectors in
// hashseam_pure_test.go to ten messages of varied lengths (0, 1, 31, 32, 33,
// 64, 100, 1000 bytes and two high-byte patterns) by consulting the live C++
// oracle binary. This keeps the H1/H2 cross-language equivalence a live check
// rather than a frozen snapshot; the oracle is the same MIRACL core the cgo
// seam links against.
func TestGoldenH1H2MoreVectors(t *testing.T) {
	binary := h1OraclePath(t)
	if binary == "" {
		t.Skipf("h1_oracle binary not found (build with: cd chameleon/nitchcore && make smoke)")
	}

	allHigh := make(Message, 32)
	for i := range allHigh {
		allHigh[i] = 0xFF
	}
	altHigh := make(Message, 100)
	for i := range altHigh {
		if i%2 == 0 {
			altHigh[i] = 0xAA
		} else {
			altHigh[i] = 0x55
		}
	}
	msgs := []Message{
		{},               // 0 bytes
		{'A'},            // 1 byte
		interopPlainMsg(31),
		interopPlainMsg(32),
		interopPlainMsg(33),
		interopPlainMsg(64),
		interopPlainMsg(100),
		interopPlainMsg(1000),
		allHigh, // 32 bytes, all high bits set
		altHigh, // 100 bytes, alternating high-byte pattern
	}
	if len(msgs) != 10 {
		t.Fatalf("internal: expected 10 golden messages, got %d", len(msgs))
	}

	vectors := runH1Oracle(t, binary, msgs)

	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	for i, v := range vectors {
		if !bytes.Equal(Message(v.msg), msgs[i]) {
			t.Fatalf("record %d: oracle echoed message %q, want %q", i, v.msg, msgs[i])
		}
		if len(v.h1) != 96 {
			t.Fatalf("record %d (len %d): oracle H1 length = %d, want 96", i, len(msgs[i]), len(v.h1))
		}
		if len(v.h2) != 48 {
			t.Fatalf("record %d (len %d): oracle H2 length = %d, want 48", i, len(msgs[i]), len(v.h2))
		}
		i, v := i, v
		t.Run(itoa(len(msgs[i])), func(t *testing.T) {
			// H1: hashToG1Unlocked must reproduce the C++ point byte for byte.
			pairingMu.Lock()
			point, err := hashToG1Unlocked(msgs[i])
			var gotH1 []byte
			if err == nil {
				gotH1 = pairing.G1.ToBytes(point)
			}
			pairingMu.Unlock()
			if err != nil {
				t.Fatalf("hashToG1Unlocked error: %v", err)
			}
			if !bytes.Equal(gotH1, v.h1) {
				t.Fatalf("H1 mismatch for %d-byte message:\n got %s\nwant %s",
					len(msgs[i]), hex.EncodeToString(gotH1), hex.EncodeToString(v.h1))
			}

			// H2: hashToScalar must reproduce the C++ scalar exactly. The
			// oracle emits MIRACL's 48-byte scalar window whose 16 leading
			// bytes are zero, so big.Int comparison is exact.
			oracleScalar := new(big.Int).SetBytes(v.h2)
			gotScalar := hashToScalar(msgs[i], pp.p)
			if fmtSprintf64(gotScalar) != fmtSprintf64(oracleScalar) {
				t.Fatalf("H2 mismatch for %d-byte message:\n got %s\nwant %s",
					len(msgs[i]), fmtSprintf64(gotScalar), fmtSprintf64(oracleScalar))
			}
		})
	}
}

// ====================== 3. pure threshold-algebra invariants ======================

// TestPureAlgebraInvariants pins the (t=2, n=4) threshold algebra end-to-end
// in pure Go:
//
// (a) every extracted key satisfies g1^sk == VerificationKey;
// (b) Lagrange-combining ANY t+1 verification keys with the same
//     evaluationPointForIndex domain weights Combine uses (mirroring Combine's
//     math over vk points instead of contributions) reproduces GlobalPKG1.
//
// Together these say the shares really lie on a degree-t polynomial over the
// root-of-unity domain whose free coefficient is the global secret — the
// property that makes Go's Combine interpolate C++-produced contributions.
func TestPureAlgebraInvariants(t *testing.T) {
	const tThreshold = 2
	const nParties = 4
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	keys, err := SimulatedDistKG(pp, tThreshold, nParties)
	if err != nil {
		t.Fatalf("SimulatedDistKG failed: %v", err)
	}
	if len(keys) != nParties {
		t.Fatalf("expected %d keys, got %d", nParties, len(keys))
	}

	// (a) g1^sk_i == vk_i for every party.
	pairingMu.Lock()
	for i, key := range keys {
		g1sk := pairing.G1.New()
		pairing.G1.MulScalar(g1sk, pp.g1, bigIntToFr(key.SecretShare))
		if !bytes.Equal(pairing.G1.ToBytes(g1sk), pairing.G1.ToBytes(key.VerificationKey)) {
			pairingMu.Unlock()
			t.Fatalf("party %d: g1^sk != VerificationKey", i+1)
		}
	}
	wantPKG1 := pairing.G1.ToBytes(keys[0].GlobalPKG1)
	pairingMu.Unlock()

	// (b) every C(n, t+1) subset of verification keys interpolates to g1^alpha.
	combos := [][]int{
		{1, 2, 3}, {1, 2, 4}, {1, 3, 4}, {2, 3, 4},
	}
	for _, subset := range combos {
		subset := subset
		t.Run("vk"+itoa(subset[0])+itoa(subset[1])+itoa(subset[2]), func(t *testing.T) {
			domainPoints := make([]*big.Int, len(subset))
			for i, idx := range subset {
				xi, err := evaluationPointForIndex(pp.p, nParties, idx)
				if err != nil {
					t.Fatalf("evaluationPointForIndex failed for index %d: %v", idx, err)
				}
				domainPoints[i] = xi
			}
			coeffs, err := lagrangeCoefficientsAtZeroGnarkFFT(domainPoints, pp.p)
			if err != nil {
				t.Fatalf("lagrangeCoefficientsAtZeroGnarkFFT failed: %v", err)
			}
			pairingMu.Lock()
			defer pairingMu.Unlock()
			interpolated := pairing.G1.Zero()
			for i, idx := range subset {
				term := pairing.G1.New()
				pairing.G1.MulScalar(term, keys[idx-1].VerificationKey, bigIntToFr(coeffs[i]))
				pairing.G1.Add(interpolated, interpolated, term)
			}
			got := pairing.G1.ToBytes(interpolated)
			if !bytes.Equal(got, wantPKG1) {
				t.Fatalf("Lagrange(vk %v) != GlobalPKG1 for subset %v:\n got %s\nwant %s",
					subset, subset, hex.EncodeToString(got), hex.EncodeToString(wantPKG1))
			}
		})
	}
}
