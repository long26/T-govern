#pragma once

#include "miracl_bls12381.hpp"

namespace nitch {

PublicParameters setup_public_parameters();
MiraclBls12381 create_context();

} // namespace nitch
