#include "../nitch/distkg.hpp"

#include <algorithm>
#include <stdexcept>
#include <unordered_set>

namespace nitch {
namespace {

bool is_power_of_two(int value) {
    return value > 0 && (value & (value - 1)) == 0;
}

Scalar evaluation_point_for_index(const EvaluationDomain& domain, int participant_index) {
    if (domain.size <= 0) {
        throw std::invalid_argument("evaluation domain must be positive");
    }
    if (participant_index <= 0 || participant_index > domain.size) {
        throw std::invalid_argument("participant_index out of range");
    }
    return domain.powers[static_cast<size_t>(participant_index % domain.size)];
}

Scalar evaluate_polynomial(MiraclBls12381& ctx, const std::vector<Scalar>& coefficients, const Scalar& x_scalar) {
    Scalar result = ctx.scalar_from_u64(0);
    for (auto it = coefficients.rbegin(); it != coefficients.rend(); ++it) {
        result = ctx.scalar_mul(result, x_scalar);
        result = ctx.scalar_add(result, *it);
    }
    return result;
}

void validate_unique_indices(const std::vector<int>& indices) {
    std::unordered_set<int> seen;
    seen.reserve(indices.size());
    for (int index : indices) {
        if (!seen.insert(index).second) {
            throw std::invalid_argument("duplicate index in qualified set");
        }
    }
}

void validate_indices_in_domain(const EvaluationDomain& domain, const std::vector<int>& indices) {
    for (int index : indices) {
        if (index <= 0 || index > domain.size) {
            throw std::invalid_argument("index out of range for evaluation domain");
        }
    }
}

std::vector<Scalar> batch_inverse(MiraclBls12381& ctx, const std::vector<Scalar>& values) {
    if (values.empty()) {
        return {};
    }

    const Scalar zero = ctx.scalar_from_u64(0);
    std::vector<Scalar> prefix(values.size(), zero);
    prefix[0] = values[0];
    if (ctx.scalar_equal(prefix[0], zero)) {
        throw std::invalid_argument("cannot invert zero value");
    }

    for (size_t i = 1; i < values.size(); ++i) {
        if (ctx.scalar_equal(values[i], zero)) {
            throw std::invalid_argument("cannot invert zero value");
        }
        prefix[i] = ctx.scalar_mul(prefix[i - 1], values[i]);
    }

    Scalar inverse_total = ctx.scalar_inv(prefix.back());
    std::vector<Scalar> inverses(values.size(), zero);

    for (size_t i = values.size(); i-- > 0;) {
        const Scalar before = (i == 0) ? ctx.scalar_from_u64(1) : prefix[i - 1];
        inverses[i] = ctx.scalar_mul(inverse_total, before);
        inverse_total = ctx.scalar_mul(inverse_total, values[i]);
    }

    return inverses;
}

} // namespace

DistributedKeyMaterial simulate_distkg(MiraclBls12381& ctx, int participant_count, int threshold) {
    if (!is_power_of_two(participant_count)) {
        throw std::invalid_argument("participant_count must be a power of two");
    }
    if (threshold < 0 || threshold >= participant_count) {
        throw std::invalid_argument("threshold must satisfy 0 <= threshold < participant_count");
    }

    DistributedKeyMaterial material;
    material.participant_count = participant_count;
    material.threshold = threshold;
    material.domain = ctx.evaluation_domain(participant_count);
    material.secret_shares.assign(participant_count, ctx.scalar_from_u64(0));
    material.verification_keys.assign(participant_count, ctx.g2_identity());
    material.public_key = ctx.g2_identity();

    for (int dealer = 1; dealer <= participant_count; ++dealer) {
        ParticipantDeal deal;
        deal.dealer_index = dealer;
        deal.polynomial_coefficients.reserve(threshold + 1);
        deal.commitments_g1.reserve(threshold + 1);
        deal.commitments_g2.reserve(threshold + 1);
        deal.evaluations.reserve(participant_count);

        for (int degree = 0; degree <= threshold; ++degree) {
            Scalar coefficient = ctx.random_scalar();
            deal.polynomial_coefficients.push_back(coefficient);
            deal.commitments_g1.push_back(ctx.g1_mul(ctx.g1_generator(), coefficient));
            deal.commitments_g2.push_back(ctx.g2_mul(ctx.g2_generator(), coefficient));
        }

        for (int participant = 1; participant <= participant_count; ++participant) {
            Scalar evaluation = evaluate_polynomial(
                ctx,
                deal.polynomial_coefficients,
                evaluation_point_for_index(material.domain, participant));
            deal.evaluations.push_back(evaluation);
            material.secret_shares[participant - 1] = ctx.scalar_add(material.secret_shares[participant - 1], evaluation);
        }

        material.public_key = ctx.g2_add(material.public_key, deal.commitments_g2.front());
        material.deals.push_back(deal);
    }

    for (int participant = 1; participant <= participant_count; ++participant) {
        material.verification_keys[participant - 1] = ctx.g2_mul(ctx.g2_generator(), material.secret_shares[participant - 1]);
    }

    return material;
}

DistributedKeyMaterial simulate_dkrs(
    MiraclBls12381& ctx,
    const DistributedKeyMaterial& old_material,
    int new_participant_count,
    int new_threshold,
    const std::vector<int>& qualified_old_indices) {
    if (old_material.participant_count <= 0) {
        throw std::invalid_argument("old_material participant_count must be positive");
    }
    if (old_material.domain.size != old_material.participant_count) {
        throw std::invalid_argument("old_material domain size mismatch");
    }
    if (static_cast<int>(old_material.secret_shares.size()) != old_material.participant_count) {
        throw std::invalid_argument("old_material secret_shares size mismatch");
    }
    if (static_cast<int>(old_material.verification_keys.size()) != old_material.participant_count) {
        throw std::invalid_argument("old_material verification_keys size mismatch");
    }

    if (!is_power_of_two(new_participant_count)) {
        throw std::invalid_argument("new_participant_count must be a power of two");
    }
    if (new_threshold < 0 || new_threshold >= new_participant_count) {
        throw std::invalid_argument("new_threshold must satisfy 0 <= new_threshold < new_participant_count");
    }

    if (qualified_old_indices.size() < static_cast<size_t>(old_material.threshold + 1)) {
        throw std::invalid_argument("qualified_old_indices must contain at least old_threshold + 1 members");
    }

    validate_unique_indices(qualified_old_indices);
    validate_indices_in_domain(old_material.domain, qualified_old_indices);

    const auto lambdas = lagrange_coefficients_at_zero(ctx, old_material.domain, qualified_old_indices);

    DistributedKeyMaterial material;
    material.participant_count = new_participant_count;
    material.threshold = new_threshold;
    material.domain = ctx.evaluation_domain(new_participant_count);
    material.public_key = old_material.public_key;
    material.secret_shares.assign(new_participant_count, ctx.scalar_from_u64(0));
    material.verification_keys.assign(new_participant_count, ctx.g2_identity());

    material.deals.reserve(qualified_old_indices.size());
    for (int old_dealer_index : qualified_old_indices) {
        ParticipantDeal deal;
        deal.dealer_index = old_dealer_index;
        deal.polynomial_coefficients.reserve(new_threshold + 1);
        deal.commitments_g1.reserve(new_threshold + 1);
        deal.commitments_g2.reserve(new_threshold + 1);
        deal.evaluations.reserve(new_participant_count);

        deal.polynomial_coefficients.push_back(old_material.secret_shares[old_dealer_index - 1]);
        for (int degree = 1; degree <= new_threshold; ++degree) {
            deal.polynomial_coefficients.push_back(ctx.random_scalar());
        }

        for (int degree = 0; degree <= new_threshold; ++degree) {
            const Scalar& coefficient = deal.polynomial_coefficients[static_cast<size_t>(degree)];
            deal.commitments_g1.push_back(ctx.g1_mul(ctx.g1_generator(), coefficient));
            deal.commitments_g2.push_back(ctx.g2_mul(ctx.g2_generator(), coefficient));
        }

        if (!ctx.g2_equal(deal.commitments_g2.front(), old_material.verification_keys[old_dealer_index - 1])) {
            throw std::runtime_error("DKRS constant term commitment does not match old verification key");
        }

        for (int new_participant = 1; new_participant <= new_participant_count; ++new_participant) {
            Scalar evaluation = evaluate_polynomial(
                ctx,
                deal.polynomial_coefficients,
                evaluation_point_for_index(material.domain, new_participant));
            deal.evaluations.push_back(evaluation);
        }

        for (int new_participant = 1; new_participant <= new_participant_count; ++new_participant) {
            if (!verify_deal_for_participant(ctx, material.domain, deal, new_participant)) {
                throw std::runtime_error("DKRS deal verification failed");
            }
        }

        material.deals.push_back(deal);
    }

    for (int new_participant = 1; new_participant <= new_participant_count; ++new_participant) {
        Scalar aggregate = ctx.scalar_from_u64(0);
        for (size_t i = 0; i < material.deals.size(); ++i) {
            aggregate = ctx.scalar_add(
                aggregate,
                ctx.scalar_mul(lambdas[i], material.deals[i].evaluations[new_participant - 1]));
        }
        material.secret_shares[new_participant - 1] = aggregate;
        material.verification_keys[new_participant - 1] = ctx.g2_mul(ctx.g2_generator(), aggregate);
    }

    return material;
}

bool verify_deal_for_participant(
    MiraclBls12381& ctx,
    const EvaluationDomain& domain,
    const ParticipantDeal& deal,
    int participant_index) {
    if (domain.size <= 0) {
        throw std::invalid_argument("evaluation domain must be positive");
    }
    if (static_cast<int>(deal.evaluations.size()) != domain.size) {
        throw std::invalid_argument("deal evaluation count does not match domain size");
    }
    if (participant_index <= 0 || participant_index > static_cast<int>(deal.evaluations.size())) {
        throw std::invalid_argument("participant_index out of range");
    }

    G1Point expected_g1 = ctx.g1_identity();
    G2Point expected_g2 = ctx.g2_identity();
    Scalar power = ctx.scalar_from_u64(1);
    Scalar participant_scalar = evaluation_point_for_index(domain, participant_index);

    for (size_t k = 0; k < deal.commitments_g1.size(); ++k) {
        expected_g1 = ctx.g1_add(expected_g1, ctx.g1_mul(deal.commitments_g1[k], power));
        expected_g2 = ctx.g2_add(expected_g2, ctx.g2_mul(deal.commitments_g2[k], power));
        power = ctx.scalar_mul(power, participant_scalar);
    }

    const Scalar& evaluation = deal.evaluations[participant_index - 1];
    const G1Point actual_g1 = ctx.g1_mul(ctx.g1_generator(), evaluation);
    const G2Point actual_g2 = ctx.g2_mul(ctx.g2_generator(), evaluation);

    return ctx.g1_equal(actual_g1, expected_g1) && ctx.g2_equal(actual_g2, expected_g2);
}

std::vector<Scalar> lagrange_coefficients_at_zero(
    MiraclBls12381& ctx,
    const EvaluationDomain& domain,
    const std::vector<int>& indices) {
    if (domain.size <= 0) {
        throw std::invalid_argument("evaluation domain must be positive");
    }
    if (indices.empty()) {
        throw std::invalid_argument("indices must not be empty");
    }
    if (static_cast<int>(indices.size()) > domain.size) {
        throw std::invalid_argument("indices size exceeds domain size");
    }

    validate_unique_indices(indices);
    validate_indices_in_domain(domain, indices);

    if (static_cast<int>(indices.size()) == domain.size) {
        return std::vector<Scalar>(indices.size(), domain.inverse_size);
    }

    std::vector<Scalar> points;
    points.reserve(indices.size());
    for (int index : indices) {
        points.push_back(evaluation_point_for_index(domain, index));
    }

    std::vector<Scalar> coefficients;
    coefficients.reserve(indices.size());
    std::vector<Scalar> denominators;
    denominators.reserve(indices.size());

    for (size_t i = 0; i < indices.size(); ++i) {
        Scalar numerator = ctx.scalar_from_u64(1);
        Scalar denominator = ctx.scalar_from_u64(1);
        const Scalar& i_scalar = points[i];

        for (size_t j = 0; j < indices.size(); ++j) {
            if (i == j) {
                continue;
            }
            const Scalar minus_j = ctx.scalar_neg(points[j]);
            numerator = ctx.scalar_mul(numerator, minus_j);
            const Scalar& j_scalar = points[j];
            denominator = ctx.scalar_mul(denominator, ctx.scalar_sub(i_scalar, j_scalar));
        }

        coefficients.push_back(numerator);
        denominators.push_back(denominator);
    }

    const std::vector<Scalar> denominator_inverses = batch_inverse(ctx, denominators);
    for (size_t i = 0; i < coefficients.size(); ++i) {
        coefficients[i] = ctx.scalar_mul(coefficients[i], denominator_inverses[i]);
    }

    return coefficients;
}

} // namespace nitch
