#include "../nitch/serialization.hpp"

#include <stdexcept>

namespace nitch {

std::string bytes_to_hex(const std::vector<uint8_t>& bytes) {
    static const char hex_chars[] = "0123456789abcdef";
    std::string result;
    result.reserve(bytes.size() * 2);
    for (uint8_t byte : bytes) {
        result.push_back(hex_chars[(byte >> 4) & 0x0f]);
        result.push_back(hex_chars[byte & 0x0f]);
    }
    return result;
}

std::vector<uint8_t> hex_to_bytes(const std::string& hex) {
    if (hex.size() % 2 != 0) {
        throw std::invalid_argument("hex string must have even length");
    }

    std::vector<uint8_t> bytes;
    bytes.reserve(hex.size() / 2);
    for (size_t i = 0; i < hex.size(); i += 2) {
        uint8_t hi = hex[i];
        uint8_t lo = hex[i + 1];
        uint8_t byte = 0;

        if (hi >= '0' && hi <= '9') byte = (hi - '0') << 4;
        else if (hi >= 'a' && hi <= 'f') byte = (hi - 'a' + 10) << 4;
        else if (hi >= 'A' && hi <= 'F') byte = (hi - 'A' + 10) << 4;
        else throw std::invalid_argument("invalid hex character");

        if (lo >= '0' && lo <= '9') byte |= (lo - '0');
        else if (lo >= 'a' && lo <= 'f') byte |= (lo - 'a' + 10);
        else if (lo >= 'A' && lo <= 'F') byte |= (lo - 'A' + 10);
        else throw std::invalid_argument("invalid hex character");

        bytes.push_back(byte);
    }
    return bytes;
}

} // namespace nitch
