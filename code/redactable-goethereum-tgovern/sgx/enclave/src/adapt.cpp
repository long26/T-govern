#include "../nitch/adapt.hpp"

#include <stdexcept>

#include "../nitch/distkg.hpp"

namespace nitch {
namespace {

G1Point delta_hash(MiraclBls12381& ctx, const std::string& source_message, const std::string& target_message) {
    return ctx.g1_sub(ctx.hash_to_g1(source_message), ctx.hash_to_g1(target_message));
}

static std::vector<uint8_t> strip_miracl_prefix(const std::vector<uint8_t>& miracl_bytes) {
    if (miracl_bytes.size() > 1 && (miracl_bytes[0] == 0x04 || miracl_bytes[0] == 0x03 || miracl_bytes[0] == 0x02)) {
        return std::vector<uint8_t>(miracl_bytes.begin() + 1, miracl_bytes.end());
    }
    return miracl_bytes;
}

std::vector<uint8_t> build_transcript(
    MiraclBls12381& ctx,
    const G1Point& y,
    const G2Point& verification_key,
    const G1Point& contribution,
    const G2Point& t1,
    const G1Point& t2) {
    std::vector<uint8_t> transcript;
    const auto append = [&](const std::vector<uint8_t>& bytes) {
        transcript.insert(transcript.end(), bytes.begin(), bytes.end());
    };

    append(strip_miracl_prefix(ctx.g2_to_bytes(ctx.g2_generator())));
    append(strip_miracl_prefix(ctx.g1_to_bytes(y)));
    append(strip_miracl_prefix(ctx.g2_to_bytes(verification_key)));
    append(strip_miracl_prefix(ctx.g1_to_bytes(contribution)));
    append(strip_miracl_prefix(ctx.g2_to_bytes(t1)));
    append(strip_miracl_prefix(ctx.g1_to_bytes(t2)));
    return transcript;
}

} // namespace

PartialShare partial_adapt(
    MiraclBls12381& ctx,
    const Scalar& secret_share,
    const G2Point& verification_key,
    int index,
    const std::string& source_message,
    const std::string& target_message) {
    PartialShare share;
    share.index = index;

    const G1Point y = delta_hash(ctx, source_message, target_message);
    share.contribution = ctx.g1_mul(y, secret_share);

    const Scalar nonce = ctx.random_scalar();
    const G2Point t1 = ctx.g2_mul(ctx.g2_generator(), nonce);
    const G1Point t2 = ctx.g1_mul(y, nonce);

    share.proof.challenge = ctx.hash_to_scalar(build_transcript(ctx, y, verification_key, share.contribution, t1, t2));
    share.proof.response = ctx.scalar_add(nonce, ctx.scalar_mul(share.proof.challenge, secret_share));
    return share;
}

bool partial_verify(
    MiraclBls12381& ctx,
    const G2Point& verification_key,
    const std::string& source_message,
    const std::string& target_message,
    const PartialShare& partial_share) {
    const G1Point y = delta_hash(ctx, source_message, target_message);
    const G2Point left_g2 = ctx.g2_mul(ctx.g2_generator(), partial_share.proof.response);
    const G2Point right_g2 = ctx.g2_mul(verification_key, partial_share.proof.challenge);
    const G2Point t1 = ctx.g2_sub(left_g2, right_g2);

    const G1Point left_g1 = ctx.g1_mul(y, partial_share.proof.response);
    const G1Point right_g1 = ctx.g1_mul(partial_share.contribution, partial_share.proof.challenge);
    const G1Point t2 = ctx.g1_sub(left_g1, right_g1);

    const Scalar challenge = ctx.hash_to_scalar(build_transcript(ctx, y, verification_key, partial_share.contribution, t1, t2));
    return ctx.scalar_equal(challenge, partial_share.proof.challenge);
}

G1Point combine_adapted_randomness(
    MiraclBls12381& ctx,
    const EvaluationDomain& domain,
    const G1Point& original_randomness,
    const std::string& source_message,
    const std::string& target_message,
    const std::vector<PartialShare>& shares,
    int threshold) {
    if (threshold < 0) {
        throw std::invalid_argument("threshold must be non-negative");
    }
    if (domain.size <= 0) {
        throw std::invalid_argument("evaluation domain must be positive");
    }
    if (threshold >= domain.size) {
        throw std::invalid_argument("threshold must be smaller than the evaluation domain size");
    }
    if (static_cast<int>(shares.size()) < threshold + 1) {
        throw std::invalid_argument("insufficient partial shares");
    }

    const std::vector<PartialShare> selected(shares.begin(), shares.begin() + threshold + 1);
    std::vector<int> indices;
    indices.reserve(selected.size());
    for (const auto& share : selected) {
        indices.push_back(share.index);
    }

    const auto lambdas = lagrange_coefficients_at_zero(ctx, domain, indices);
    G1Point aggregate = ctx.g1_identity();
    for (size_t i = 0; i < selected.size(); ++i) {
        aggregate = ctx.g1_add(aggregate, ctx.g1_mul(selected[i].contribution, lambdas[i]));
    }

    return ctx.g1_add(original_randomness, aggregate);
}

} // namespace nitch
