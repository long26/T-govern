#include "../nitch/miracl_bls12381.hpp"

#include <algorithm>
#include <array>
#include <stdexcept>
#include <utility>
#include <vector>

#include "core.h"
#include "ecp_BLS12381.h"
#include "ecp2_BLS12381.h"
#include "fp12_BLS12381.h"
#include "pair_BLS12381.h"
#include "randapi.h"
#include "../nitch/serialization.hpp"

#include "sgx_trts.h"

namespace nitch {
namespace {

using core::octet;
using B384_58::BIG_comp;
using B384_58::BIG_copy;
using B384_58::BIG_dec;
using B384_58::BIG_fromBytes;
using B384_58::BIG_invmodp;
using B384_58::BIG_mod;
using B384_58::BIG_modadd;
using B384_58::BIG_modmul;
using B384_58::BIG_modneg;
using B384_58::BIG_norm;
using B384_58::BIG_bit;
using B384_58::BIG_nbits;
using B384_58::BIG_randomnum;
using B384_58::BIG_rcopy;
using B384_58::BIG_shr;
using B384_58::BIG_sub;
using B384_58::BIG_toBytes;
using BLS12381::CURVE_Order;
using BLS12381::ECP;
using BLS12381::ECP2;
using BLS12381::ECP2_add;
using BLS12381::ECP2_copy;
using BLS12381::ECP2_equals;
using BLS12381::ECP2_generator;
using BLS12381::ECP2_inf;
using BLS12381::ECP2_isinf;
using BLS12381::ECP2_neg;
using BLS12381::ECP2_sub;
using BLS12381::ECP2_toOctet;
using BLS12381::ECP_add;
using BLS12381::ECP_copy;
using BLS12381::ECP_equals;
using BLS12381::ECP_generator;
using BLS12381::ECP_inf;
using BLS12381::ECP_isinf;
using BLS12381::ECP_map2point;
using BLS12381::ECP_neg;
using BLS12381::ECP_sub;
using BLS12381::ECP_toOctet;
using BLS12381::FP;
using BLS12381::FP12;
using BLS12381::FP12_copy;
using BLS12381::FP12_equals;
using BLS12381::FP12_inv;
using BLS12381::FP12_mul;
using BLS12381::FP12_one;
using BLS12381::FP12_toOctet;
using BLS12381::PAIR_G1mul;
using BLS12381::PAIR_G2mul;
using BLS12381::PAIR_GTpow;
using BLS12381::PAIR_ate;
using BLS12381::PAIR_fexp;

constexpr int kScalarBytes = 48;
constexpr int kG1CompressedBytes = 49;
constexpr int kG2CompressedBytes = 97;
constexpr int kGtBytes = 12 * kScalarBytes;

void load_curve_order(MiraclBig out) {
    BIG_rcopy(out, CURVE_Order);
}

octet make_octet(char* buffer, int capacity) {
    octet out;
    out.len = 0;
    out.max = capacity;
    out.val = buffer;
    return out;
}

std::vector<uint8_t> octet_to_vector(const octet& value) {
    return std::vector<uint8_t>(value.val, value.val + value.len);
}

bool is_power_of_two(int value) {
    return value > 0 && (value & (value - 1)) == 0;
}

int integer_log2(int value) {
    int log_size = 0;
    while (value > 1) {
        value >>= 1;
        ++log_size;
    }
    return log_size;
}

int compute_max_fft_log_size() {
    MiraclBig order_minus_one;
    load_curve_order(order_minus_one);
    BIG_dec(order_minus_one, 1);
    BIG_norm(order_minus_one);

    int log_size = 0;
    while (BIG_bit(order_minus_one, log_size) == 0) {
        ++log_size;
    }
    return log_size;
}

Scalar scalar_pow_u64(MiraclBls12381& ctx, const Scalar& base, uint64_t exponent) {
    Scalar result = ctx.scalar_from_u64(1);
    Scalar power = base;

    while (exponent > 0) {
        if ((exponent & 1U) != 0U) {
            result = ctx.scalar_mul(result, power);
        }
        exponent >>= 1U;
        if (exponent > 0) {
            power = ctx.scalar_mul(power, power);
        }
    }

    return result;
}

Scalar scalar_pow_big(MiraclBls12381& ctx, const Scalar& base, const MiraclBig& exponent) {
    Scalar result = ctx.scalar_from_u64(1);
    Scalar power = base;

    const int bits = BIG_nbits(const_cast<MiraclBig&>(exponent));
    for (int bit = 0; bit < bits; ++bit) {
        if (BIG_bit(const_cast<MiraclBig&>(exponent), bit) != 0) {
            result = ctx.scalar_mul(result, power);
        }
        power = ctx.scalar_mul(power, power);
    }

    return result;
}

Scalar find_primitive_root_of_unity(MiraclBls12381& ctx, int size) {
    if (size == 1) {
        return ctx.scalar_from_u64(1);
    }

    const int log_size = integer_log2(size);

    MiraclBig order_minus_one;
    load_curve_order(order_minus_one);
    BIG_dec(order_minus_one, 1);
    BIG_norm(order_minus_one);

    MiraclBig cofactor;
    BIG_copy(cofactor, order_minus_one);
    BIG_shr(cofactor, log_size);

    const Scalar one = ctx.scalar_from_u64(1);
    const std::array<uint64_t, 10> deterministic_candidates{2, 3, 5, 7, 11, 13, 17, 19, 23, 29};
    for (uint64_t candidate_value : deterministic_candidates) {
        const Scalar candidate = ctx.scalar_from_u64(candidate_value);
        const Scalar root = scalar_pow_big(ctx, candidate, cofactor);
        if (!ctx.scalar_equal(scalar_pow_u64(ctx, root, static_cast<uint64_t>(size / 2)), one)) {
            return root;
        }
    }

    for (int attempt = 0; attempt < 64; ++attempt) {
        const Scalar candidate = ctx.random_scalar();
        if (ctx.scalar_equal(candidate, one)) {
            continue;
        }

        const Scalar root = scalar_pow_big(ctx, candidate, cofactor);
        if (!ctx.scalar_equal(scalar_pow_u64(ctx, root, static_cast<uint64_t>(size / 2)), one)) {
            return root;
        }
    }

    throw std::runtime_error("failed to find a primitive root of unity");
}

} // namespace

MiraclBls12381::MiraclBls12381() {
    char raw_seed[32];
    sgx_read_rand((uint8_t*)raw_seed, 32);

    octet seed = make_octet(raw_seed, 32);
    seed.len = 32;
    core::CREATE_CSPRNG(&rng_, &seed);
    rng_initialized_ = true;
    max_fft_log_size_ = compute_max_fft_log_size();
}

MiraclBls12381::~MiraclBls12381() {
    if (rng_initialized_) {
        core::KILL_CSPRNG(&rng_);
    }
}

const PublicParameters& MiraclBls12381::params() const {
    return params_;
}

Scalar MiraclBls12381::random_scalar() {
    Scalar scalar;
    MiraclBig order;
    load_curve_order(order);
    BIG_randomnum(scalar.value, order, &rng_);
    return scalar;
}

Scalar MiraclBls12381::scalar_from_u64(uint64_t value) {
    std::array<char, kScalarBytes> bytes{};
    for (int i = 0; i < 8; ++i) {
        bytes[kScalarBytes - 1 - i] = static_cast<char>((value >> (8 * i)) & 0xff);
    }

    Scalar scalar;
    BIG_fromBytes(scalar.value, bytes.data());
    MiraclBig order;
    load_curve_order(order);
    BIG_mod(scalar.value, order);
    return scalar;
}

Scalar MiraclBls12381::scalar_from_bytes_mod_order(const std::vector<uint8_t>& bytes) const {
    std::array<char, kScalarBytes> fixed{};
    const size_t copy_len = std::min(bytes.size(), fixed.size());
    std::copy(bytes.end() - copy_len, bytes.end(), fixed.end() - copy_len);

    Scalar scalar;
    BIG_fromBytes(scalar.value, fixed.data());
    MiraclBig order;
    load_curve_order(order);
    BIG_mod(scalar.value, order);
    return scalar;
}

Scalar MiraclBls12381::scalar_add(const Scalar& a, const Scalar& b) const {
    Scalar out;
    MiraclBig lhs;
    MiraclBig rhs;
    MiraclBig order;
    BIG_copy(lhs, const_cast<MiraclBig&>(a.value));
    BIG_copy(rhs, const_cast<MiraclBig&>(b.value));
    load_curve_order(order);
    BIG_modadd(out.value, lhs, rhs, order);
    return out;
}

Scalar MiraclBls12381::scalar_sub(const Scalar& a, const Scalar& b) const {
    MiraclBig rhs;
    MiraclBig neg_rhs;
    MiraclBig order;
    BIG_copy(rhs, const_cast<MiraclBig&>(b.value));
    load_curve_order(order);
    BIG_modneg(neg_rhs, rhs, order);

    Scalar out;
    MiraclBig lhs;
    BIG_copy(lhs, const_cast<MiraclBig&>(a.value));
    BIG_modadd(out.value, lhs, neg_rhs, order);
    return out;
}

Scalar MiraclBls12381::scalar_mul(const Scalar& a, const Scalar& b) const {
    Scalar out;
    MiraclBig lhs;
    MiraclBig rhs;
    MiraclBig order;
    BIG_copy(lhs, const_cast<MiraclBig&>(a.value));
    BIG_copy(rhs, const_cast<MiraclBig&>(b.value));
    load_curve_order(order);
    BIG_modmul(out.value, lhs, rhs, order);
    return out;
}

Scalar MiraclBls12381::scalar_neg(const Scalar& a) const {
    Scalar out;
    MiraclBig value;
    MiraclBig order;
    BIG_copy(value, const_cast<MiraclBig&>(a.value));
    load_curve_order(order);
    BIG_modneg(out.value, value, order);
    return out;
}

Scalar MiraclBls12381::scalar_inv(const Scalar& a) const {
    Scalar out;
    MiraclBig order;
    load_curve_order(order);
    BIG_copy(out.value, const_cast<MiraclBig&>(a.value));
    BIG_invmodp(out.value, out.value, order);
    return out;
}

bool MiraclBls12381::scalar_equal(const Scalar& a, const Scalar& b) const {
    MiraclBig lhs;
    MiraclBig rhs;
    BIG_copy(lhs, const_cast<MiraclBig&>(a.value));
    BIG_copy(rhs, const_cast<MiraclBig&>(b.value));
    return BIG_comp(lhs, rhs) == 0;
}

std::string MiraclBls12381::scalar_to_hex(const Scalar& scalar) const {
    return bytes_to_hex(scalar_to_bytes(scalar));
}

G1Point MiraclBls12381::g1_generator() const {
    G1Point point;
    ECP_generator(&point.value);
    return point;
}

G2Point MiraclBls12381::g2_generator() const {
    G2Point point;
    ECP2_generator(&point.value);
    return point;
}

G1Point MiraclBls12381::g1_identity() const {
    G1Point point;
    ECP_inf(&point.value);
    return point;
}

G2Point MiraclBls12381::g2_identity() const {
    G2Point point;
    ECP2_inf(&point.value);
    return point;
}

GTPoint MiraclBls12381::gt_identity() const {
    GTPoint point;
    FP12_one(&point.value);
    return point;
}

G1Point MiraclBls12381::g1_add(const G1Point& a, const G1Point& b) const {
    G1Point out;
    ECP_copy(&out.value, const_cast<ECP*>(&a.value));
    ECP other;
    ECP_copy(&other, const_cast<ECP*>(&b.value));
    ECP_add(&out.value, &other);
    return out;
}

G1Point MiraclBls12381::g1_sub(const G1Point& a, const G1Point& b) const {
    G1Point out;
    ECP_copy(&out.value, const_cast<ECP*>(&a.value));
    ECP other;
    ECP_copy(&other, const_cast<ECP*>(&b.value));
    ECP_sub(&out.value, &other);
    return out;
}

G1Point MiraclBls12381::g1_neg(const G1Point& a) const {
    G1Point out;
    ECP_copy(&out.value, const_cast<ECP*>(&a.value));
    ECP_neg(&out.value);
    return out;
}

G1Point MiraclBls12381::g1_mul(const G1Point& point, const Scalar& scalar) const {
    G1Point out;
    ECP_copy(&out.value, const_cast<ECP*>(&point.value));
    PAIR_G1mul(&out.value, const_cast<MiraclBig&>(scalar.value));
    return out;
}

bool MiraclBls12381::g1_equal(const G1Point& a, const G1Point& b) const {
    ECP lhs;
    ECP rhs;
    ECP_copy(&lhs, const_cast<ECP*>(&a.value));
    ECP_copy(&rhs, const_cast<ECP*>(&b.value));
    return ECP_equals(&lhs, &rhs) == 1;
}

bool MiraclBls12381::g1_is_identity(const G1Point& point) const {
    ECP value;
    ECP_copy(&value, const_cast<ECP*>(&point.value));
    return ECP_isinf(&value) == 1;
}

G2Point MiraclBls12381::g2_add(const G2Point& a, const G2Point& b) const {
    G2Point out;
    ECP2_copy(&out.value, const_cast<ECP2*>(&a.value));
    ECP2 other;
    ECP2_copy(&other, const_cast<ECP2*>(&b.value));
    ECP2_add(&out.value, &other);
    return out;
}

G2Point MiraclBls12381::g2_sub(const G2Point& a, const G2Point& b) const {
    G2Point out;
    ECP2_copy(&out.value, const_cast<ECP2*>(&a.value));
    ECP2 other;
    ECP2_copy(&other, const_cast<ECP2*>(&b.value));
    ECP2_sub(&out.value, &other);
    return out;
}

G2Point MiraclBls12381::g2_neg(const G2Point& a) const {
    G2Point out;
    ECP2_copy(&out.value, const_cast<ECP2*>(&a.value));
    ECP2_neg(&out.value);
    return out;
}

G2Point MiraclBls12381::g2_mul(const G2Point& point, const Scalar& scalar) const {
    G2Point out;
    ECP2_copy(&out.value, const_cast<ECP2*>(&point.value));
    PAIR_G2mul(&out.value, const_cast<MiraclBig&>(scalar.value));
    return out;
}

bool MiraclBls12381::g2_equal(const G2Point& a, const G2Point& b) const {
    ECP2 lhs;
    ECP2 rhs;
    ECP2_copy(&lhs, const_cast<ECP2*>(&a.value));
    ECP2_copy(&rhs, const_cast<ECP2*>(&b.value));
    return ECP2_equals(&lhs, &rhs) == 1;
}

bool MiraclBls12381::g2_is_identity(const G2Point& point) const {
    ECP2 value;
    ECP2_copy(&value, const_cast<ECP2*>(&point.value));
    return ECP2_isinf(&value) == 1;
}

GTPoint MiraclBls12381::pairing(const G1Point& g1, const G2Point& g2) const {
    GTPoint out;
    ECP lhs;
    ECP2 rhs;
    ECP_copy(&lhs, const_cast<ECP*>(&g1.value));
    ECP2_copy(&rhs, const_cast<ECP2*>(&g2.value));
    PAIR_ate(&out.value, &rhs, &lhs);
    PAIR_fexp(&out.value);
    return out;
}

GTPoint MiraclBls12381::gt_mul(const GTPoint& a, const GTPoint& b) const {
    GTPoint out;
    FP12_copy(&out.value, const_cast<FP12*>(&a.value));
    FP12 other;
    FP12_copy(&other, const_cast<FP12*>(&b.value));
    FP12_mul(&out.value, &other);
    return out;
}

GTPoint MiraclBls12381::gt_inv(const GTPoint& a) const {
    GTPoint out;
    FP12 value;
    FP12_copy(&value, const_cast<FP12*>(&a.value));
    FP12_inv(&value, &value);
    FP12_copy(&out.value, &value);
    return out;
}

GTPoint MiraclBls12381::gt_pow(const GTPoint& value, const Scalar& scalar) const {
    GTPoint out;
    FP12 base;
    FP12_copy(&base, const_cast<FP12*>(&value.value));
    PAIR_GTpow(&base, const_cast<MiraclBig&>(scalar.value));
    FP12_copy(&out.value, &base);
    return out;
}

bool MiraclBls12381::gt_equal(const GTPoint& a, const GTPoint& b) const {
    FP12 lhs;
    FP12 rhs;
    FP12_copy(&lhs, const_cast<FP12*>(&a.value));
    FP12_copy(&rhs, const_cast<FP12*>(&b.value));
    return FP12_equals(&lhs, &rhs) == 1;
}

G1Point MiraclBls12381::hash_to_g1(const std::string& message) const {
    std::array<char, 256> dst_buffer{};
    std::vector<char> data(message.begin(), message.end());

    octet dst = make_octet(dst_buffer.data(), static_cast<int>(dst_buffer.size()));
    core::OCT_jstring(&dst, const_cast<char*>(params_.h1_dst.c_str()));

    octet msg = make_octet(data.data(), static_cast<int>(data.size()));
    msg.len = static_cast<int>(data.size());

    MiraclBig modulus;
    B384_58::BIG_rcopy(modulus, BLS12381::Modulus);
    const int modulus_bits = B384_58::BIG_nbits(modulus);
    const int expand_len = (modulus_bits + CURVE_SECURITY_BLS12381 + 7) / 8;

    std::array<FP, 2> field_elements{};
    std::array<char, 256> okm_buffer{};
    octet okm = make_octet(okm_buffer.data(), static_cast<int>(okm_buffer.size()));
    core::XMD_Expand(MC_SHA2, SHA256, &okm, expand_len * static_cast<int>(field_elements.size()), &dst, &msg);

    for (size_t i = 0; i < field_elements.size(); ++i) {
        B384_58::DBIG wide;
        B384_58::BIG reduced;
        B384_58::BIG_dfromBytesLen(wide, okm.val + (i * expand_len), expand_len);
        B384_58::BIG_ctdmod(reduced, wide, modulus, 8 * expand_len - modulus_bits);
        BLS12381::FP_nres(&field_elements[i], reduced);
    }

    G1Point point;
    G1Point other;
    ECP_map2point(&point.value, &field_elements[0]);
    ECP_map2point(&other.value, &field_elements[1]);
    ECP_add(&point.value, &other.value);
    BLS12381::ECP_cfp(&point.value);
    BLS12381::ECP_affine(&point.value);
    return point;
}

Scalar MiraclBls12381::hash_to_scalar(const std::vector<uint8_t>& transcript) const {
    std::array<char, 256> dst_buffer{};
    std::vector<char> data(transcript.begin(), transcript.end());
    octet dst = make_octet(dst_buffer.data(), static_cast<int>(dst_buffer.size()));
    octet msg = make_octet(data.data(), static_cast<int>(data.size()));
    msg.len = static_cast<int>(data.size());

    core::OCT_jstring(&dst, const_cast<char*>(params_.h2_dst.c_str()));

    std::array<char, 64> digest_buffer{};
    octet digest = make_octet(digest_buffer.data(), static_cast<int>(digest_buffer.size()));
    core::XMD_Expand(MC_SHA2, SHA256, &digest, 64, &dst, &msg);

    return scalar_from_bytes_mod_order(std::vector<uint8_t>(digest.val, digest.val + digest.len));
}

const EvaluationDomain& MiraclBls12381::evaluation_domain(int size) const {
    MiraclBls12381& mutable_ctx = const_cast<MiraclBls12381&>(*this);

    if (!is_power_of_two(size)) {
        throw std::invalid_argument("evaluation domain size must be a power of two");
    }

    const int log_size = integer_log2(size);
    if (log_size > max_fft_log_size_) {
        throw std::invalid_argument("evaluation domain size exceeds supported root order");
    }

    const auto cached = evaluation_domain_cache_.find(size);
    if (cached != evaluation_domain_cache_.end()) {
        return cached->second;
    }

    EvaluationDomain domain;
    domain.size = size;
    domain.log_size = log_size;
    domain.root_of_unity = find_primitive_root_of_unity(mutable_ctx, size);
    domain.inverse_root_of_unity = mutable_ctx.scalar_inv(domain.root_of_unity);
    domain.inverse_size = mutable_ctx.scalar_inv(mutable_ctx.scalar_from_u64(static_cast<uint64_t>(size)));
    domain.powers.resize(static_cast<size_t>(size));
    domain.inverse_powers.resize(static_cast<size_t>(size));
    domain.powers[0] = mutable_ctx.scalar_from_u64(1);
    domain.inverse_powers[0] = mutable_ctx.scalar_from_u64(1);

    for (int i = 1; i < size; ++i) {
        domain.powers[static_cast<size_t>(i)] =
            mutable_ctx.scalar_mul(domain.powers[static_cast<size_t>(i - 1)], domain.root_of_unity);
        domain.inverse_powers[static_cast<size_t>(i)] =
            mutable_ctx.scalar_mul(domain.inverse_powers[static_cast<size_t>(i - 1)], domain.inverse_root_of_unity);
    }

    const auto inserted = evaluation_domain_cache_.emplace(size, std::move(domain));
    return inserted.first->second;
}

std::vector<uint8_t> MiraclBls12381::g1_to_bytes(const G1Point& point, bool compressed) const {
    std::array<char, kG1CompressedBytes * 2> buffer{};
    octet oct = make_octet(buffer.data(), static_cast<int>(buffer.size()));
    ECP value;
    ECP_copy(&value, const_cast<ECP*>(&point.value));
    ECP_toOctet(&oct, &value, compressed);
    return octet_to_vector(oct);
}

std::vector<uint8_t> MiraclBls12381::g2_to_bytes(const G2Point& point, bool compressed) const {
    std::array<char, kG2CompressedBytes * 2> buffer{};
    octet oct = make_octet(buffer.data(), static_cast<int>(buffer.size()));
    ECP2 value;
    ECP2_copy(&value, const_cast<ECP2*>(&point.value));
    ECP2_toOctet(&oct, &value, compressed);
    return octet_to_vector(oct);
}

std::vector<uint8_t> MiraclBls12381::gt_to_bytes(const GTPoint& point) const {
    std::array<char, kGtBytes> buffer{};
    octet oct = make_octet(buffer.data(), static_cast<int>(buffer.size()));
    FP12 value;
    FP12_copy(&value, const_cast<FP12*>(&point.value));
    FP12_toOctet(&oct, &value);
    return octet_to_vector(oct);
}

std::vector<uint8_t> MiraclBls12381::scalar_to_bytes(const Scalar& scalar) const {
    std::array<char, kScalarBytes> buffer{};
    BIG_toBytes(buffer.data(), const_cast<MiraclBig&>(scalar.value));
    return std::vector<uint8_t>(buffer.begin(), buffer.end());
}

} // namespace nitch
