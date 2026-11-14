#pragma once

#include <string>
#include <vector>

namespace nitch {

struct Scalar;
struct G1Point;
struct G2Point;
struct GTPoint;

std::string bytes_to_hex(const std::vector<uint8_t>& bytes);
std::vector<uint8_t> hex_to_bytes(const std::string& hex);

} // namespace nitch
