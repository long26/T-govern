#pragma once

#include <array>
#include <cstdint>
#include <string>
#include <vector>

#include "core.h"
#include "big_B384_58.h"
#include "ecp_BLS12381.h"
#include "ecp2_BLS12381.h"
#include "fp12_BLS12381.h"

namespace nitch {

using MiraclBig = B384_58::BIG;

struct Scalar {
    MiraclBig value{};
};

struct G1Point {
    BLS12381::ECP value{};
};

struct G2Point {
    BLS12381::ECP2 value{};
};

struct GTPoint {
    BLS12381::FP12 value{};
};

struct PublicParameters {
    std::string h1_dst = "NITCH_BLS12381_H1";
    std::string h2_dst = "NITCH_BLS12381_H2";
};

struct EvaluationDomain {
    int size = 0;
    int log_size = 0;
    Scalar root_of_unity;
    Scalar inverse_root_of_unity;
    Scalar inverse_size;
    std::vector<Scalar> powers;
    std::vector<Scalar> inverse_powers;
};

struct ParticipantDeal {
    int dealer_index = 0;
    std::vector<Scalar> polynomial_coefficients;
    std::vector<G1Point> commitments_g1;
    std::vector<G2Point> commitments_g2;
    std::vector<Scalar> evaluations;
};

struct DistributedKeyMaterial {
    int participant_count = 0;
    int threshold = 0;
    EvaluationDomain domain;
    std::vector<ParticipantDeal> deals;
    G2Point public_key;
    std::vector<Scalar> secret_shares;
    std::vector<G2Point> verification_keys;
};

struct HashOutput {
    GTPoint h;
    G1Point r;
};

struct PartialAdaptProof {
    Scalar challenge;
    Scalar response;
};

struct PartialShare {
    int index = 0;
    G1Point contribution;
    PartialAdaptProof proof;
};

} // namespace nitch
