#pragma once

#include <map>
#include <string>
#include <vector>

#include "types.hpp"

namespace nitch {

class MiraclBls12381 {
public:
    MiraclBls12381();
    ~MiraclBls12381();

    const PublicParameters& params() const;

    Scalar random_scalar();
    Scalar scalar_from_u64(uint64_t value);
    Scalar scalar_from_bytes_mod_order(const std::vector<uint8_t>& bytes) const;
    Scalar scalar_add(const Scalar& a, const Scalar& b) const;
    Scalar scalar_sub(const Scalar& a, const Scalar& b) const;
    Scalar scalar_mul(const Scalar& a, const Scalar& b) const;
    Scalar scalar_neg(const Scalar& a) const;
    Scalar scalar_inv(const Scalar& a) const;
    bool scalar_equal(const Scalar& a, const Scalar& b) const;
    std::string scalar_to_hex(const Scalar& scalar) const;

    G1Point g1_generator() const;
    G2Point g2_generator() const;
    G1Point g1_identity() const;
    G2Point g2_identity() const;
    GTPoint gt_identity() const;

    G1Point g1_add(const G1Point& a, const G1Point& b) const;
    G1Point g1_sub(const G1Point& a, const G1Point& b) const;
    G1Point g1_neg(const G1Point& a) const;
    G1Point g1_mul(const G1Point& point, const Scalar& scalar) const;
    bool g1_equal(const G1Point& a, const G1Point& b) const;
    bool g1_is_identity(const G1Point& point) const;

    G2Point g2_add(const G2Point& a, const G2Point& b) const;
    G2Point g2_sub(const G2Point& a, const G2Point& b) const;
    G2Point g2_neg(const G2Point& a) const;
    G2Point g2_mul(const G2Point& point, const Scalar& scalar) const;
    bool g2_equal(const G2Point& a, const G2Point& b) const;
    bool g2_is_identity(const G2Point& point) const;

    GTPoint pairing(const G1Point& g1, const G2Point& g2) const;
    GTPoint gt_mul(const GTPoint& a, const GTPoint& b) const;
    GTPoint gt_inv(const GTPoint& a) const;
    GTPoint gt_pow(const GTPoint& value, const Scalar& scalar) const;
    bool gt_equal(const GTPoint& a, const GTPoint& b) const;

    G1Point hash_to_g1(const std::string& message) const;
    Scalar hash_to_scalar(const std::vector<uint8_t>& transcript) const;
    const EvaluationDomain& evaluation_domain(int size) const;

    std::vector<uint8_t> g1_to_bytes(const G1Point& point, bool compressed = true) const;
    std::vector<uint8_t> g2_to_bytes(const G2Point& point, bool compressed = true) const;
    std::vector<uint8_t> gt_to_bytes(const GTPoint& point) const;
    std::vector<uint8_t> scalar_to_bytes(const Scalar& scalar) const;

private:
    PublicParameters params_{};
    core::csprng rng_{};
    bool rng_initialized_ = false;
    mutable std::map<int, EvaluationDomain> evaluation_domain_cache_;
    int max_fft_log_size_ = 0;
};

} // namespace nitch
