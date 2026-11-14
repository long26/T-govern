#pragma once

#include <vector>

#include "miracl_bls12381.hpp"

namespace nitch {

PartialShare partial_adapt(
    MiraclBls12381& ctx,
    const Scalar& secret_share,
    const G2Point& verification_key,
    int index,
    const std::string& source_message,
    const std::string& target_message);

bool partial_verify(
    MiraclBls12381& ctx,
    const G2Point& verification_key,
    const std::string& source_message,
    const std::string& target_message,
    const PartialShare& partial_share);

G1Point combine_adapted_randomness(
    MiraclBls12381& ctx,
    const EvaluationDomain& domain,
    const G1Point& original_randomness,
    const std::string& source_message,
    const std::string& target_message,
    const std::vector<PartialShare>& shares,
    int threshold);

} // namespace nitch
