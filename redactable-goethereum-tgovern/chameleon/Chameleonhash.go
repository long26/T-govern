package chameleon

import (
	"crypto/rand"
	"encoding/json"
	"github.com/ethereum/go-ethereum/log"
	bls12381 "github.com/kilic/bls12-381"
	"io/ioutil"
	"math/big"
	"os"
)

const (
	T         = 252 // Security parameter, bit length of q
	Certainty = 80  // Primality test certainty
)

var (
	two = big.NewInt(2)
)

var (
	P, Q, G, R      = big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0)
	systemPublicKey = big.NewInt(0)
)

func init() {
	var file *os.File
	var err error
	for _, path := range []string{"helper.json", "../helper.json", "../../helper.json", "../../../helper.json"} {
		file, err = os.Open(path)
		if err == nil {
			break
		}
	}
	if err != nil {
		log.Error("Failed to open helper.json", "err", err)
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Error("Failed to close file")
		}
	}(file)

	byteValue, _ := ioutil.ReadAll(file)
	var pqgData PQGData
	err = json.Unmarshal(byteValue, &pqgData)
	if err != nil {
		log.Error("Failed to unmarshal genesis.json")
	}
	P = pqgData.PValue.Int
	Q = pqgData.QValue.Int
	G = pqgData.GValue.Int
	R = pqgData.RValue.Int
	systemPublicKey = pqgData.PubKey.Int
}

func SystemPublicKey() *big.Int {
	return new(big.Int).Set(systemPublicKey)
}

// getRandomInRange generates a random number in the range 1 to n-1
func getRandomInRange(n *big.Int) *big.Int {
	var v *big.Int
	for {
		v, _ = rand.Int(rand.Reader, n)
		if v.Cmp(big.NewInt(0)) > 0 && v.Cmp(n) < 0 {
			break
		}
	}
	return v
}

// keyGen generates the chameleon hash public and private keys
func KeyGen() (*big.Int, *big.Int) {
	private := getRandomInRange(Q)
	public := new(big.Int).Exp(G, private, P)
	return public.Mod(public, P), private
}

// Hash calculates the chameleon hash value
func Hash(systemPublicKey, tx, r *big.Int) *big.Int {
	if hash, ok := nitchBLSHashBigInt(systemPublicKey, tx, r); ok {
		return hash
	}
	txExp := reduceModQ(tx)
	rExp := reduceModQ(r)
	hash1 := new(big.Int).Exp(G, txExp, P)
	hash2 := new(big.Int).Exp(systemPublicKey, rExp, P)
	hash1.Mul(hash1, hash2)
	hash1.Mod(hash1, P)
	return hash1
}

func reduceModQ(x *big.Int) *big.Int {
	if x == nil {
		return new(big.Int)
	}
	if Q == nil || Q.Sign() == 0 {
		return x
	}
	if x.Sign() >= 0 && x.Cmp(Q) < 0 {
		return x
	}
	return new(big.Int).Mod(x, Q)
}

// Forge constructs a new random number r_new that conforms to the chameleon hash value
func Forge(systemPrivateKey, txBefore, r_old, txAfter *big.Int) *big.Int {
	r_new1 := new(big.Int).Mul(systemPrivateKey, r_old)    // sr
	r_new1.Add(r_new1, txBefore)                           // sr + t
	r_new1.Sub(r_new1, txAfter)                            // sr + t - t_new
	r_new2 := new(big.Int).ModInverse(systemPrivateKey, Q) // s^-1 mod q
	r_new := new(big.Int).Mul(r_new1, r_new2)              // r_new1 * r_new2
	r_new.Mod(r_new, Q)                                    // (r_new1 * r_new2) mod q
	return r_new
}

// Verify checks if the original message and random number match the chameleon hash value
func Verify(hash, systemPublicKey, tx, r *big.Int) bool {
	newHash := Hash(systemPublicKey, tx, r)
	return newHash.Cmp(hash) == 0
}

func GenerateR() (*big.Int, error) {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	engine := bls12381.NewEngine()
	point, err := nitchHashToG1(engine, seed)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(engine.G1.ToCompressed(point)), nil
}
