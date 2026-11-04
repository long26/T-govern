#pragma once

#include "nitch/miracl_bls12381.hpp"

namespace nitch {

DistributedKeyMaterial simulate_distkg(MiraclBls12381& ctx, int participant_count, int threshold);

// Distributed Key ReSharing (DKRS): resharing the *same* global secret from an old committee to a new one.
//
// - qualified_old_indices corresponds to dkrs.md's U_final (1-based indices into the old committee).
// - new_threshold corresponds to t^(e+1) (degree of each dealer's resharing polynomial).
// - The returned public_key is preserved from old_material.
DistributedKeyMaterial simulate_dkrs(
    MiraclBls12381& ctx,
    const DistributedKeyMaterial& old_material,
    int new_participant_count,
    int new_threshold,
    const std::vector<int>& qualified_old_indices);

bool verify_deal_for_participant(
    MiraclBls12381& ctx,
    const EvaluationDomain& domain,
    const ParticipantDeal& deal,
    int participant_index);
std::vector<Scalar> lagrange_coefficients_at_zero(
    MiraclBls12381& ctx,
    const EvaluationDomain& domain,
    const std::vector<int>& indices);

} // namespace nitch
