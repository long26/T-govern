package chameleon

import (
	"encoding/hex"
	"math/big"
	"testing"
)

// hashseam_pure_test.go pins the H1/H2 primitives on the pure-Go seam to the
// cross-language equivalence contract with the C++/MIRACL implementation
// (sgx/enclave/src/miracl_bls12381.cpp), and pins expand_message_xmd to the
// RFC 9380 Appendix K.1 test vectors.

// TestExpandMessageXMDRFC9380Vectors checks expandMessageXMD against the
// official RFC 9380 Appendix K.1 vectors for expand_message_xmd(SHA-256)
// with DST "QUUX-V01-CS02-with-expander-SHA256-128".
func TestExpandMessageXMDRFC9380Vectors(t *testing.T) {
	dst := []byte("QUUX-V01-CS02-with-expander-SHA256-128")
	vectors := []struct {
		name     string
		msg      string
		lenBytes int
		expected string
	}{
		{"empty/0x20", "", 0x20,
			"68a985b87eb6b46952128911f2a4412bbc302a9d759667f87f7a21d803f07235"},
		{"abc/0x20", "abc", 0x20,
			"d8ccab23b5985ccea865c6c97b6e5b8350e794e603b4b97902f53a8a0d605615"},
		{"abcdef0123456789/0x20", "abcdef0123456789", 0x20,
			"eff31487c770a893cfb36f912fbfcbff40d5661771ca4b2cb4eafe524333f5c1"},
		{"empty/0x80", "", 0x80,
			"af84c27ccfd45d41914fdff5df25293e221afc53d8ad2ac06d5e3e29485dadbe" +
				"e0d121587713a3e0dd4d5e69e93eb7cd4f5df4cd103e188cf60cb02edc3edf18" +
				"eda8576c412b18ffb658e3dd6ec849469b979d444cf7b26911a08e63cf31f9dc" +
				"c541708d3491184472c2c29bb749d4286b004ceb5ee6b9a7fa5b646c993f0ced"},
		{"abc/0x80", "abc", 0x80,
			"abba86a6129e366fc877aab32fc4ffc70120d8996c88aee2fe4b32d6c7b6437a" +
				"647e6c3163d40b76a73cf6a5674ef1d890f95b664ee0afa5359a5c4e0798563" +
				"5bbecbac65d747d3d2da7ec2b8221b17b0ca9dc8a1ac1c07ea6a1e60583e2cb0" +
				"0058e77b7b72a298425cd1b941ad4ec65e8afc50303a22c0f99b0509b4c895f40"},
		{"abcdef0123456789/0x80", "abcdef0123456789", 0x80,
			"ef904a29bffc4cf9ee82832451c946ac3c8f8058ae97d8d629831a74c6572bd9" +
				"ebd0df635cd1f208e2038e760c4994984ce73f0d55ea9f22af83ba4734569d4b" +
				"c95e18350f740c07eef653cbb9f87910d833751825f0ebefa1abe5420bb52be1" +
				"4cf489b37fe1a72f7de2d10be453b2c9d9eb20c7e3f6edc5a60629178d9478df"},
	}
	for _, v := range vectors {
		v := v
		t.Run(v.name, func(t *testing.T) {
			got, err := expandMessageXMD([]byte(v.msg), dst, v.lenBytes)
			if err != nil {
				t.Fatalf("expandMessageXMD error: %v", err)
			}
			if len(got) != v.lenBytes {
				t.Fatalf("output length = %d, want %d", len(got), v.lenBytes)
			}
			if hex.EncodeToString(got) != v.expected {
				t.Fatalf("expandMessageXMD mismatch:\n got %s\nwant %s", hex.EncodeToString(got), v.expected)
			}
		})
	}

	if _, err := expandMessageXMD([]byte("m"), make([]byte, 256), 32); err == nil {
		t.Fatal("expected error for DST longer than 255 bytes")
	}
}

// TestHashToScalarGoldenCXX pins hashToScalar (H2) to the byte-exact scalars
// produced by the C++ MIRACL implementation
// (MiraclBls12381::hash_to_scalar: XMD_Expand(SHA256, 64, DST="NITCH_BLS12381_H2")
// then scalar_from_bytes_mod_order, whose right-aligned 48-byte window is the
// final 48 of the 64 output bytes).
func TestHashToScalarGoldenCXX(t *testing.T) {
	// BLS12-381 scalar field order r.
	r, ok := new(big.Int).SetString("73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)
	if !ok {
		t.Fatal("failed to parse BLS12-381 order")
	}
	vectors := []struct{ msg, scalar string }{
		// okm[16:64] reduced mod r, captured from the C++ implementation.
		{"old", "3b735593aa3ba2bdf3ead3f9d57a86bbf8ea8e68e28afc6b347e5c30b1175b84"},
		{"new", "6fb3d7088d2948e0a3f4588dc1653cac83fac32f6c3a01c7670eeeb3669c345b"},
		{"abc", "2fe83f3392191884373e05116800d8e29e8529144a6221025f2d9073a4fd3e75"},
	}
	for _, v := range vectors {
		v := v
		t.Run(v.msg, func(t *testing.T) {
			got := hashToScalar([]byte(v.msg), r)
			if fmtSprintf64(got) != v.scalar {
				t.Fatalf("hashToScalar(%q) = %s, want %s", v.msg, fmtSprintf64(got), v.scalar)
			}
		})
	}
}

// TestHashToG1GoldenCXX pins hashToG1Unlocked (H1) to the byte-exact G1
// encodings produced by the C++ MIRACL implementation
// (MiraclBls12381::hash_to_g1, RFC 9380 SSWU-RO with DST "NITCH_BLS12381_H1").
func TestHashToG1GoldenCXX(t *testing.T) {
	vectors := []struct{ msg, point string }{
		// G1 uncompressed (96-byte) serializations captured from the C++ implementation.
		{"old", "05a35f6748df468540f8e9a49cbb66ce582981133fda63f5b9efc9baff12fd05" +
			"d6231e2b4d5332a03945ed1be21eec940364f362e40cde58ad92c9b309d2911c" +
			"3521475c10df371e7b8b2d546f5b59e7189e751cb345353e2f6402c0556671a7"},
		{"new", "0a85fa3f8b52173f824487430b791589c44201224543567b7f1dd33072c9c92a" +
			"b31110f9fb24c28bb9b81d0c1ae158e3101b413357abb2d5ae28e70d1619a2f7b" +
			"c77c631b18b7a5f649c7e4abc3c6c61723bf28b615eaf89724e43c85f509bc8"},
		{"abc", "17bb9267908210caf8e96eaa7d2794e279578753fc05eda580b728eeff9f18ed" +
			"2297a34da1d2e452447ec6edbe2e02a611682af257894dcdbfea9a570aaee5d66" +
			"f0376cc7395010cd6ab3c48855b89d67fc1f5a80ea0c9045d5471d98f81ba9e"},
	}
	pairingMu.Lock()
	defer pairingMu.Unlock()
	for _, v := range vectors {
		v := v
		t.Run(v.msg, func(t *testing.T) {
			point, err := hashToG1Unlocked(Message(v.msg))
			if err != nil {
				t.Fatalf("hashToG1Unlocked error: %v", err)
			}
			got := hex.EncodeToString(pairing.G1.ToBytes(point))
			if got != v.point {
				t.Fatalf("H1(%q) =\n %s\nwant\n %s", v.msg, got, v.point)
			}
		})
	}
}

// fmtSprintf64 formats a non-negative big.Int as exactly 64 hex characters.
func fmtSprintf64(v *big.Int) string {
	out := v.Text(16)
	for len(out) < 64 {
		out = "0" + out
	}
	return out
}
