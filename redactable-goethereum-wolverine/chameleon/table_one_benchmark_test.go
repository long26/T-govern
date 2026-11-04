package chameleon

import (
	"fmt"
	"math/big"
	"testing"
)

var (
	tableOneKeySink        *NITCHKey
	tableOneHashOutputSink *HashOutput
	tableOneRPrimeSink     *big.Int
)

// tableOneKeyGen reconstructs the most likely KeyGen measured by Table I.
// Unlike the NITCH DistKG protocol, this is the underlying centralized
// chameleon-hash key generation: sample sk and compute pk = g2^sk. Public
// parameter Setup is a separate algorithm and is therefore not included.
func tableOneKeyGen(pp *PublicParameters) (*NITCHKey, error) {
	if pp == nil || pp.p == nil || pp.g2 == nil {
		return nil, ErrInvalidParameters
	}
	sk := randomScalar(pp.p)

	pairingMu.Lock()
	pk := pairing.G2.New()
	pairing.G2.MulScalar(pk, pp.g2, bigIntToFr(sk))
	pairingMu.Unlock()

	return &NITCHKey{
		PP:          pp,
		GlobalPK:    pk,
		SecretShare: sk,
	}, nil
}

// tableOneHash is the Hash(pk, m) algorithm from Construction 1. It samples the
// random check value r internally, so both r generation and h computation are
// included in the Table I timing boundary.
func tableOneHash(key *NITCHKey, message Message) (*HashOutput, error) {
	if key == nil || key.PP == nil {
		return nil, ErrInvalidParameters
	}
	r := RandomPointG1(key.PP)
	if r == nil {
		return nil, fmt.Errorf("generate random check value")
	}
	return Hash(key, message, r)
}

// tableOneAdapt implements the centralized Adapt algorithm with the minimum
// number of group operations. A holder of the complete trapdoor sk computes
//
//   r' = r + (H1(m) - H1(m'))^sk
//
// and returns the final check value. Compared with the algebraically equivalent
// expanded form r + H1(m)^sk - H1(m')^sk, this uses one rather than two
// trapdoor scalar multiplications. It does not create a NIZK proof and does not
// call PartialVerify or Combine; those belong to the threshold NITCH path
// measured separately in Table II.
func tableOneAdapt(key *NITCHKey, hashOutput *HashOutput, oldMessage, newMessage Message) (*big.Int, error) {
	if key == nil || key.PP == nil || key.SecretShare == nil || hashOutput == nil || hashOutput.CheckValue == nil {
		return nil, ErrInvalidParameters
	}

	pairingMu.Lock()
	defer pairingMu.Unlock()

	h1Old, err := hashToG1Unlocked(oldMessage)
	if err != nil {
		return nil, fmt.Errorf("hash old message: %w", err)
	}
	h1New, err := hashToG1Unlocked(newMessage)
	if err != nil {
		return nil, fmt.Errorf("hash new message: %w", err)
	}

	negH1New := pairing.G1.New()
	pairing.G1.Neg(negH1New, h1New)
	y := pairing.G1.New()
	pairing.G1.Add(y, h1Old, negH1New)
	yToSK := pairing.G1.New()
	pairing.G1.MulScalar(yToSK, y, bigIntToFr(key.SecretShare))
	r, err := bigIntToG1PointUnlocked(hashOutput.CheckValue)
	if err != nil {
		return nil, fmt.Errorf("decode old check value: %w", err)
	}
	rPrime := pairing.G1.New()
	pairing.G1.Add(rPrime, r, yToSK)
	return g1PointToBigIntUnlocked(rPrime), nil
}

func TestTableOneAlgorithms(t *testing.T) {
	pp, err := Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	key, err := tableOneKeyGen(pp)
	if err != nil {
		t.Fatalf("KeyGen: %v", err)
	}
	oldMessage := Message("table-one original message")
	newMessage := Message("table-one edited message")
	hashOutput, err := tableOneHash(key, oldMessage)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !Verify(key, hashOutput.Hash, oldMessage, hashOutput.CheckValue) {
		t.Fatal("original hash verification failed")
	}
	rPrime, err := tableOneAdapt(key, hashOutput, oldMessage, newMessage)
	if err != nil {
		t.Fatalf("Adapt: %v", err)
	}
	if !Verify(key, hashOutput.Hash, newMessage, rPrime) {
		t.Fatal("adapted hash verification failed")
	}
}

func BenchmarkTableOneKeyGen(b *testing.B) {
	pp, err := Setup()
	if err != nil {
		b.Fatalf("Setup: %v", err)
	}
	if _, err := tableOneKeyGen(pp); err != nil {
		b.Fatalf("KeyGen warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key, err := tableOneKeyGen(pp)
		if err != nil {
			b.Fatalf("KeyGen: %v", err)
		}
		tableOneKeySink = key
	}
}

func BenchmarkTableOneHash(b *testing.B) {
	pp, err := Setup()
	if err != nil {
		b.Fatalf("Setup: %v", err)
	}
	key, err := tableOneKeyGen(pp)
	if err != nil {
		b.Fatalf("prepare key: %v", err)
	}
	message := Message("table-one hash message")
	if _, err := tableOneHash(key, message); err != nil {
		b.Fatalf("Hash warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hashOutput, err := tableOneHash(key, message)
		if err != nil {
			b.Fatalf("Hash: %v", err)
		}
		tableOneHashOutputSink = hashOutput
	}
}

func BenchmarkTableOneAdapt(b *testing.B) {
	pp, err := Setup()
	if err != nil {
		b.Fatalf("Setup: %v", err)
	}
	key, err := tableOneKeyGen(pp)
	if err != nil {
		b.Fatalf("prepare key: %v", err)
	}
	oldMessage := Message("table-one original message")
	newMessage := Message("table-one edited message")
	hashOutput, err := tableOneHash(key, oldMessage)
	if err != nil {
		b.Fatalf("prepare hash: %v", err)
	}
	rPrime, err := tableOneAdapt(key, hashOutput, oldMessage, newMessage)
	if err != nil {
		b.Fatalf("Adapt warmup: %v", err)
	}
	if !Verify(key, hashOutput.Hash, newMessage, rPrime) {
		b.Fatal("Adapt warmup did not preserve the hash")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rPrime, err := tableOneAdapt(key, hashOutput, oldMessage, newMessage)
		if err != nil {
			b.Fatalf("Adapt: %v", err)
		}
		tableOneRPrimeSink = rPrime
	}
}
