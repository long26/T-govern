package sgx

import (
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Table 6.2 TEE/REE benchmark driver.
//
// This exercises the enclave instrumentation added in commit fbe8732
// ("sgx: add table 6.2 benchmark instrumentation"): the empty ECALL
// (BenchmarkNoop) and the seven aggregate RDTSC counters exposed through
// ResetBenchmarkStats / GetBenchmarkStats. It reports, per operation:
//   - REE_ms : algorithm-only host baseline from table_6_2_ree_bench (no SGX)
//   - TEE_alg_ms : in-enclave algorithm time isolated by the RDTSC scope,
//                  converted via an in-run TSC frequency calibration
//   - TEE_ecall_ms : end-to-end host wall-clock of the wrapping ECALL
//   - overhead% : SGX algorithm premium = (TEE_alg_ms - REE_ms) / REE_ms
//
// The committee authorization certificate for op 1 (verify_cert_req_auth) is
// produced by the REE bench's --committee-cert mode, which signs with the exact
// MIRACL BLS implementation the enclave verifies with, so the bytes are
// accepted as-is. TEE runs in HW mode (go test -tags sgx_hw).

const (
	table62N = 8 // num_parties, must satisfy the enclave DKG 2^k constraint
	table62T = 4 // threshold; matches the REE baseline (n=8, t=4)
	table62Warmup = 10 // untimed warmup iterations, mirrors REE kWarmupIterations
)

// active = threshold + 1, encoded as benchConfig.ActiveTEEs (see sgx_bench_test.go).
var table62Cfg = benchConfig{TotalNodes: table62N, ActiveTEEs: table62T + 1}

var table62REEBin = filepath.Join("enclave", "bench", "table_6_2_ree_bench")

// committee_cert_helper: standalone MIRACL BLS signer that emits a committee
// keypair + authorization certificate the enclave accepts verbatim (op 1).
var table62CertHelper = filepath.Join("enclave", "bench", "committee_cert_helper")

var table62OpName = map[TEEBenchmarkOperation]string{
	TEEBenchmarkDKGLocal:                "TCH.DKG local",
	TEEBenchmarkAuthorizationCertVerify: "Authorization cert",
	TEEBenchmarkPCollide:                "TCH.PCollide",
	TEEBenchmarkSignature:               "TEE signature",
	TEEBenchmarkVRFKeyGen:               "VRF.KeyGen",
	TEEBenchmarkVRFComp:                 "VRF.Comp",
	TEEBenchmarkShareErase:              "Share erase",
}

var table62OpOrder = []TEEBenchmarkOperation{
	TEEBenchmarkDKGLocal,
	TEEBenchmarkAuthorizationCertVerify,
	TEEBenchmarkPCollide,
	TEEBenchmarkSignature,
	TEEBenchmarkVRFKeyGen,
	TEEBenchmarkVRFComp,
	TEEBenchmarkShareErase,
}

type table62Row struct {
	op        TEEBenchmarkOperation
	reeMs     float64
	algMs     float64
	ecallMs   float64
	calls     uint64
	avgCycles uint64
	minCycles uint64
	maxCycles uint64
	note      string
}

func (r table62Row) overheadPct() string {
	if r.reeMs <= 0 || r.algMs <= 0 {
		return "-"
	}
	return fmt.Sprintf("%+.1f%%", (r.algMs-r.reeMs)/r.reeMs*100)
}

// runCounted runs `warmup` untimed iterations, resets the enclave counters,
// then runs iter() N times while the host clock spans the loop, and reads the
// aggregate cycles for op. setup (if any) runs once before the warmup and is
// not charged to the measurement.
func runCounted(op TEEBenchmarkOperation, N, warmup int, setup func(), iter func()) (table62Row, error) {
	if setup != nil {
		setup()
	}
	for i := 0; i < warmup; i++ {
		iter()
	}
	if err := ResetBenchmarkStats(); err != nil {
		return table62Row{}, fmt.Errorf("reset: %w", err)
	}
	t0 := time.Now()
	for i := 0; i < N; i++ {
		iter()
	}
	elapsed := time.Since(t0)
	st, err := GetBenchmarkStats(op)
	if err != nil {
		return table62Row{}, fmt.Errorf("get_stats(%d): %w", op, err)
	}
	r := table62Row{
		op:        op,
		calls:     st.Calls,
		minCycles: st.MinCycles,
		maxCycles: st.MaxCycles,
		ecallMs:   elapsed.Seconds() * 1000 / float64(N),
	}
	if st.Calls > 0 {
		r.avgCycles = st.TotalCycles / st.Calls
	}
	return r, nil
}

// prepareChameleonSecret runs DistKG for the configured (n,t) and extracts
// node 1's secret share into the enclave, so PartialAdapt / EraseShare can run.
// It reuses the DKG harness in sgx_bench_test.go (same test compilation unit).
func prepareChameleonSecret() ([]int32, []byte, []byte, []byte, error) {
	_, qualIndices, b0Data, a0Data, sharesData, err := runDKGFlow(table62Cfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if _, err := DistKGExtract(1, table62Cfg.threshold(), table62Cfg.TotalNodes,
		qualIndices, b0Data, nil, a0Data, nil, sharesData, nil, nil, nil, nil, nil, 0, 0); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("distkg extract: %w", err)
	}
	return qualIndices, b0Data, a0Data, sharesData, nil
}

// committee cert fields, emitted by `table_6_2_ree_bench --committee-cert`.
type committeeCert struct {
	pubkey, digest, sig []byte
	dreq, dpolicy       []byte
	cpkv, expires       []byte
}

func loadCommitteeCert(t *testing.T) *committeeCert {
	out, err := exec.Command(table62CertHelper).Output()
	if err != nil {
		t.Fatalf("run %s: %v", table62CertHelper, err)
	}
	c := &committeeCert{}
	dec := func(line, prefix string, dst *[]byte) bool {
		if !strings.HasPrefix(line, prefix) {
			return false
		}
		b, err := hex.DecodeString(strings.TrimPrefix(line, prefix))
		if err != nil {
			t.Fatalf("decode %s: %v", prefix, err)
		}
		*dst = b
		return true
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		_ = dec(line, "PUBKEY=", &c.pubkey) ||
			dec(line, "DIGEST=", &c.digest) ||
			dec(line, "SIG=", &c.sig) ||
			dec(line, "DREQ=", &c.dreq) ||
			dec(line, "DPOLICY=", &c.dpolicy) ||
			dec(line, "CPKV=", &c.cpkv) ||
			dec(line, "EXPIRES=", &c.expires)
	}
	if c.pubkey == nil || c.sig == nil || c.digest == nil {
		t.Fatalf("incomplete committee cert output: %s", out)
	}
	return c
}

// runREEBench executes the REE baseline binary and parses us/op per operation.
func runREEBench(iters int) map[TEEBenchmarkOperation]float64 {
	out, err := exec.Command(table62REEBin, strconv.Itoa(iters)).Output()
	if err != nil {
		return nil
	}
	m := map[TEEBenchmarkOperation]float64{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "us/op") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		// New format: "<label>  <mean> ± <stddev> us/op" -> mean is the token
		// before "±". Old format: "<label>  <mean> us/op" -> token before us/op.
		valStr := f[len(f)-2]
		for i, tok := range f {
			if tok == "±" && i > 0 {
				valStr = f[i-1]
				break
			}
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "TCH.DKG local"):
			m[TEEBenchmarkDKGLocal] = val
		case strings.HasPrefix(line, "Authorization cert"):
			m[TEEBenchmarkAuthorizationCertVerify] = val
		case strings.HasPrefix(line, "TCH.PCollide"):
			m[TEEBenchmarkPCollide] = val
		case strings.HasPrefix(line, "TEE signature"):
			m[TEEBenchmarkSignature] = val
		case strings.HasPrefix(line, "VRF.KeyGen"):
			m[TEEBenchmarkVRFKeyGen] = val
		case strings.HasPrefix(line, "VRF.Comp"):
			m[TEEBenchmarkVRFComp] = val
		case strings.HasPrefix(line, "Share erase"):
			m[TEEBenchmarkShareErase] = val
		}
	}
	return m
}

// invariantTSCMHz returns this machine's invariant TSC rate. The invariant TSC
// ticks at a fixed frequency independent of turbo/scaling, so it is the correct
// divisor for the enclave's RDTSC cycle counters. Read from the kernel log
// ("tsc: Detected NNN MHz"); falls back to a measured constant for this host.
func invariantTSCMHz() float64 {
	if out, err := exec.Command("dmesg").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			i := strings.Index(line, "tsc: Detected")
			if i < 0 {
				continue
			}
			rest := strings.TrimSpace(line[i+len("tsc: Detected"):])
			if j := strings.Index(rest, " MHz"); j > 0 {
				if v, err := strconv.ParseFloat(rest[:j], 64); err == nil && v > 0 {
					return v
				}
			}
		}
	}
	return 2699.95 // fallback: this host's measured invariant TSC (Xeon 8369B)
}

// measureEmptyECALL times the empty ECALL (ecall_benchmark_noop) over a host
// wall-clock loop (with a short warmup). The TSC frequency for cycle->ms
// conversion comes from invariantTSCMHz(); no host-side RDTSC helper is needed.
func measureEmptyECALL(noopN, warmup int) float64 {
	for i := 0; i < warmup; i++ {
		_ = BenchmarkNoop()
	}
	t0 := time.Now()
	for i := 0; i < noopN; i++ {
		_ = BenchmarkNoop()
	}
	elapsed := time.Since(t0).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return elapsed * 1000 / float64(noopN)
}

func TestTable6_2(t *testing.T) {
	if testing.Short() {
		t.Skip("Table 6.2 benchmark is long; skipped in -short")
	}

	// ---- REE baseline (algorithm-only host numbers) ----
	reeUs := runREEBench(100)
	if reeUs == nil {
		t.Logf("WARN: %s not runnable; REE column will be empty. Run `make -C sgx run-table-6-2-ree-bench`.", table62REEBin)
	}

	// ---- committee cert for op 1 (valid BLS authorization certificate) ----
	cert := loadCommitteeCert(t)

	// ---- enclave session ----
	if err := Init(); err != nil {
		t.Fatalf("sgx Init: %v", err)
	}
	// Generate sig+vrf identity keys once, so subsequent ECALLs that call
	// create_sig_and_vrf_key return early and do not contaminate op 4. Capture
	// the derived pubkeys: ecall_vrf_eval requires hardware_id_input==sig addr
	// and vrf_pubkey==vrf addr.
	_, _, gSigPk, gVrfPk, _, err := GetIdentity()
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}

	const freqN = 5000
	const warmup = 10
	noopMs := measureEmptyECALL(freqN, warmup)
	freqMHz := invariantTSCMHz()
	freqHz := freqMHz * 1e6
	cycToMs := func(cyc uint64) float64 { return float64(cyc) / freqHz * 1000 }

	t.Logf("HW SGX enclave. Invariant TSC = %.3f MHz (from dmesg).", freqMHz)
	t.Logf("Empty ECALL (ecall_benchmark_noop): %.4f ms (over %d iters, %d warmup)", noopMs, freqN, warmup)

	rows := make(map[TEEBenchmarkOperation]table62Row)

	// ---- op 0: TCH.DKG local (DistKGCreateDeal, no cert) ----
	r0, err := runCounted(TEEBenchmarkDKGLocal, 100, table62Warmup, nil, func() {
		if _, err := DistKGCreateDeal(1, table62T, table62N, nil, nil, nil, nil, 0, 0); err != nil {
			t.Fatalf("DistKGCreateDeal: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("op0: %v", err)
	}
	rows[TEEBenchmarkDKGLocal] = r0

	// ---- op 2/3: PartialAdapt without cert (PCollide + TEE signature) ----
	qual, b0, a0, shares, err := prepareChameleonSecret()
	if err != nil {
		t.Fatalf("chameleon setup: %v", err)
	}
	src := []byte("table-6-2 source transaction payload")
	tgt := []byte("table-6-2 target transaction payload")
	dReq := []byte("table-6-2 redaction request digest")
	paNoCert := func() {
		if _, err := PartialAdapt(src, tgt, nil, nil, nil, nil, dReq, nil, nil, nil, nil, nil, nil, 0, 0); err != nil {
			t.Fatalf("PartialAdapt: %v", err)
		}
	}
	r2, err := runCounted(TEEBenchmarkPCollide, 100, table62Warmup, func() {
		// secret may have been consumed by an earlier erase; re-establish it.
		if _, e := DistKGExtract(1, table62Cfg.threshold(), table62Cfg.TotalNodes,
			qual, b0, nil, a0, nil, shares, nil, nil, nil, nil, nil, 0, 0); e != nil {
			t.Fatalf("re-extract: %v", e)
		}
	}, paNoCert)
	if err != nil {
		t.Fatalf("op2: %v", err)
	}
	r3, err := runCounted(TEEBenchmarkSignature, 100, table62Warmup, func() {
		if _, e := DistKGExtract(1, table62Cfg.threshold(), table62Cfg.TotalNodes,
			qual, b0, nil, a0, nil, shares, nil, nil, nil, nil, nil, 0, 0); e != nil {
			t.Fatalf("re-extract: %v", e)
		}
	}, paNoCert)
	if err != nil {
		t.Fatalf("op3: %v", err)
	}
	rows[TEEBenchmarkPCollide] = r2
	rows[TEEBenchmarkSignature] = r3

	// ---- op 1: Authorization cert verify (PartialAdapt WITH a valid cert) ----
	if err := SetCommitteePubKey(cert.pubkey); err != nil {
		t.Fatalf("SetCommitteePubKey: %v", err)
	}
	paWithCert := func() {
		if _, err := PartialAdapt(src, tgt, cert.digest, cert.sig, nil, nil,
			cert.dreq, cert.dpolicy, cert.cpkv, cert.expires,
			nil, nil, nil, 0, 0); err != nil {
			t.Fatalf("PartialAdapt(cert): %v", err)
		}
	}
	r1, err := runCounted(TEEBenchmarkAuthorizationCertVerify, 100, table62Warmup, func() {
		if _, e := DistKGExtract(1, table62Cfg.threshold(), table62Cfg.TotalNodes,
			qual, b0, nil, a0, nil, shares, nil, nil, nil, nil, nil, 0, 0); e != nil {
			t.Fatalf("re-extract: %v", e)
		}
	}, paWithCert)
	if err != nil {
		t.Fatalf("op1: %v", err)
	}
	// Host cost of cert verification ≈ PartialAdapt(cert) - PartialAdapt(no cert).
	r1.ecallMs = r1.ecallMs - r2.ecallMs
	if r1.ecallMs < 0 {
		r1.ecallMs = 0
	}
	r1.note = "host=Δ(PartialAdapt±cert)"
	rows[TEEBenchmarkAuthorizationCertVerify] = r1

	// ---- op 5: VRF.Comp (VRFEval) ----
	chainID := make([]byte, 32)
	contract := make([]byte, 32)
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	r5, err := runCounted(TEEBenchmarkVRFComp, 100, table62Warmup, nil, func() {
		// hardware_id_input must equal g_sig_key.addr32 and vrf_pubkey must
		// equal g_vrf_key.addr32 (both derived in GetIdentity above).
		if _, err := VRFEval(chainID, contract, 1, gSigPk, gVrfPk, seed); err != nil {
			t.Fatalf("VRFEval: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("op5: %v", err)
	}
	rows[TEEBenchmarkVRFComp] = r5

	// ---- op 6: Share erase (re-establish secret before each timed erase) ----
	const eraseN = 100
	reextract := func() {
		if _, err := DistKGExtract(1, table62Cfg.threshold(), table62Cfg.TotalNodes,
			qual, b0, nil, a0, nil, shares, nil, nil, nil, nil, nil, 0, 0); err != nil {
			t.Fatalf("re-extract before erase: %v", err)
		}
	}
	eraseOnce := func(epoch uint64) {
		cID := make([]byte, 32)
		cAddr := make([]byte, 32)
		owner := make([]byte, 32)
		if _, err := EraseShare(epoch, 1, cID, cAddr, owner); err != nil {
			t.Fatalf("EraseShare: %v", err)
		}
	}
	// warmup (untimed, uncounted): each erase consumes the secret, so re-setup.
	for i := 0; i < table62Warmup; i++ {
		reextract()
		eraseOnce(uint64(i + 1))
	}
	if err := ResetBenchmarkStats(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	var eraseHost time.Duration
	for i := 0; i < eraseN; i++ {
		reextract() // untimed re-setup (DistKGExtract does not touch op0/op6 counters)
		te0 := time.Now()
		eraseOnce(uint64(i + 1))
		eraseHost += time.Since(te0)
	}
	st6, err := GetBenchmarkStats(TEEBenchmarkShareErase)
	if err != nil {
		t.Fatalf("get_stats(erase): %v", err)
	}
	r6 := table62Row{
		op:        TEEBenchmarkShareErase,
		calls:     st6.Calls,
		avgCycles: 0,
		minCycles: st6.MinCycles,
		maxCycles: st6.MaxCycles,
		ecallMs:   eraseHost.Seconds() * 1000 / float64(eraseN),
	}
	if st6.Calls > 0 {
		r6.avgCycles = st6.TotalCycles / st6.Calls
	}
	rows[TEEBenchmarkShareErase] = r6

	// ---- op 4: VRF.KeyGen (fires once per enclave session) ----
	// Close the main session, then open K fresh sessions: the
	// g_sig_key_generated guard re-arms on each Init, so each session records
	// exactly one VRF keygen. Host wall-clock is not isolable (keygen is
	// one-shot and bundled with the rest of the first ECALL), so op 4 is
	// reported from the enclave cycle counter.
	Close()

	const keygenSessions = 100
	var kgTotalCyc, kgMin, kgMax uint64
	var kgCalls uint64
	for s := 0; s < keygenSessions; s++ {
		if err := Init(); err != nil {
			t.Fatalf("keygen session init: %v", err)
		}
		if err := ResetBenchmarkStats(); err != nil {
			t.Fatalf("reset: %v", err)
		}
		if _, _, _, _, _, err := GetIdentity(); err != nil {
			t.Fatalf("GetIdentity (keygen): %v", err)
		}
		st, err := GetBenchmarkStats(TEEBenchmarkVRFKeyGen)
		if err != nil {
			t.Fatalf("get_stats(keygen): %v", err)
		}
		kgCalls += st.Calls
		kgTotalCyc += st.TotalCycles
		if st.Calls > 0 {
			if kgMin == 0 || (st.MinCycles > 0 && st.MinCycles < kgMin) {
				kgMin = st.MinCycles
			}
			if st.MaxCycles > kgMax {
				kgMax = st.MaxCycles
			}
		}
		Close()
	}
	r4 := table62Row{
		op:        TEEBenchmarkVRFKeyGen,
		calls:     kgCalls,
		minCycles: kgMin,
		maxCycles: kgMax,
		ecallMs:   0, // not isolable: keygen is one-shot per session
	}
	if kgCalls > 0 {
		r4.avgCycles = kgTotalCyc / kgCalls
	}
	r4.note = fmt.Sprintf("%d sessions, host not isolable", keygenSessions)
	rows[TEEBenchmarkVRFKeyGen] = r4

	// ---- assemble + print ----
	for op, r := range rows {
		if reeUs[op] > 0 {
			r.reeMs = reeUs[op] / 1000
		}
		if r.avgCycles > 0 {
			r.algMs = cycToMs(r.avgCycles)
		}
		rows[op] = r
	}

	t.Logf("")
	t.Logf("=== Table 6.2: TEE (HW) vs REE  (n=%d, t=%d, TSC=%.1f MHz) ===", table62N, table62T, freqMHz)
	t.Logf("%-20s %8s %12s %16s %12s %9s", "Operation", "REE_ms", "TEE_alg_ms", "cyc/op(min..max)", "TEE_ecall_ms", "overhead")
	t.Logf("%-20s %8s %12s %16s %12s %9s", "-------------------", "------", "------------", "----------------", "------------", "--------")
	for _, op := range table62OpOrder {
		r := rows[op]
		cyc := "-"
		if r.avgCycles > 0 {
			cyc = fmt.Sprintf("%d (%d..%d)", r.avgCycles, r.minCycles, r.maxCycles)
		}
		alg := "-"
		if r.algMs > 0 {
			alg = fmt.Sprintf("%.4f", r.algMs)
		}
		ree := "-"
		if r.reeMs > 0 {
			ree = fmt.Sprintf("%.4f", r.reeMs)
		}
		ecall := "-"
		if r.ecallMs > 0 {
			ecall = fmt.Sprintf("%.4f", r.ecallMs)
		}
		t.Logf("%-20s %8s %12s %16s %12s %9s", table62OpName[op], ree, alg, cyc, ecall, r.overheadPct())
	}
	t.Logf("overhead = (TEE_alg_ms - REE_ms)/REE_ms ; TEE_alg_ms = avg_cycles / TSC_freq.")
	t.Logf("TEE_ecall_ms is end-to-end host wall-clock of the wrapping ECALL (incl. ECALL boundary).")

	// ---- correctness: every instrumented op must have fired ----
	for _, op := range table62OpOrder {
		if rows[op].calls == 0 {
			t.Errorf("op %d (%s) recorded 0 calls — instrumentation did not fire", op, table62OpName[op])
		}
	}
}
