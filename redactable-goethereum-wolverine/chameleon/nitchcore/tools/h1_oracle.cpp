// Standalone oracle: prints, for each argv message, the hex of the
// uncompressed-kilic (prefix-stripped) H1 point and the H2 scalar.
// Usage: h1_oracle <message>...   (message taken as raw bytes of the argv string)
#include <cstdio>
#include <string>
#include <vector>
#include "nitch/miracl_bls12381.hpp"

static std::vector<uint8_t> strip_prefix(const std::vector<uint8_t>& b) {
    if (b.size() > 1 && (b[0] == 0x04 || b[0] == 0x03 || b[0] == 0x02)) {
        return std::vector<uint8_t>(b.begin() + 1, b.end());
    }
    return b;
}

int main(int argc, char** argv) {
    nitch::MiraclBls12381 ctx;
    for (int i = 1; i < argc; ++i) {
        const std::string msg(argv[i]);
        const auto h1 = strip_prefix(ctx.g1_to_bytes(ctx.hash_to_g1(msg), false));
        const auto h2 = ctx.scalar_to_bytes(ctx.hash_to_scalar(std::vector<uint8_t>(msg.begin(), msg.end())));
        printf("msg=%s\nH1=", msg.c_str());
        for (auto c : h1) printf("%02x", c);
        printf("\nH2=");
        for (auto c : h2) printf("%02x", c);
        printf("\n");
    }
    return 0;
}
