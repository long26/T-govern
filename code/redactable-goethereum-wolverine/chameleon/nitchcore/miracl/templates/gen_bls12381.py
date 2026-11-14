#!/usr/bin/env python3
"""Regenerate the per-curve MIRACL .cpp sources for BLS12381 (B384_58).

Upstream MIRACL's config64.py generates per-curve .cpp files (big_B384_58.cpp,
fp_BLS12381.cpp, ...) from the XXX/YYY/ZZZ templates in this directory, compiles
them into core.a, and then DELETES them (see config64.py main(): "clean up"
loop over generated_files). The tee-connect repo therefore ships only the
templates and the generated headers, plus the prebuilt core.a/libcore.a.

This script vendors those per-curve sources as real files. It imports the
unmodified upstream config64.py and replays its exact curveset() substitution
sequence for BLS12381 (option: field BLS12381, curve BLS12381, base 58,
pf=BLS12_CURVE), minus the algorithm families nitchcore drops (ecdh, eddsa,
hpke, mpin, bls wrapper). The result is byte-identical to what upstream
compiled into libcore.a (modulo the NITCHCORE thread_local patch applied to
big.cpp beforehand).

Usage: python3 gen_bls12381.py   (from this directory, or anywhere - paths are
resolved relative to this script). Writes the .cpp files into the parent
directory (miracl/).
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
os.chdir(HERE)

import config64  # noqa: E402  (upstream, unmodified; main() only runs under __main__)

# curveset("381", "BLS12381", "BLS12381", "58", ...) parameters, from
# miracl_crypto.pf_curves in config64.py:
#   nbt="381", tf="BLS12381", tc="BLS12381", base="58", pf="BLS12_CURVE"
NBT, TF, TC, BASE = "381", "BLS12381", "BLS12381", "58"

itb = int(NBT) + (8 - int(NBT) % 8) % 8   # 384
nb = itb // 8                              # 48
bd = "B%d_%s" % (itb, BASE)                # B384_58
nlen = 1 + ((8 * nb - 1) // int(BASE))     # 7


def emit(template, target, zzz=False):
    config64.copy_temp_file(template, target)
    if zzz:
        config64.replace(target, "ZZZ", TC)
    config64.replace(target, "YYY", TF)
    config64.replace(target, "XXX", bd)
    os.replace(target, os.path.join("..", target))
    print("generated ../" + target)


def main():
    print("bd=%s tf=%s tc=%s nlen=%d" % (bd, TF, TC, nlen))

    # big_<bd>.cpp : XXX -> bd, plus inlined unrolled mul/sqr/redc bodies
    target = "big_%s.cpp" % bd
    config64.copy_temp_file("big.cpp", target)
    config64.replace(target, "XXX", bd)
    config64.replace(target, "INLINE_MUL2", config64.inline_mul2(nlen, bd))
    config64.replace(target, "INLINE_MUL1", config64.inline_mul1(nlen, bd))
    config64.replace(target, "INLINE_SQR", config64.inline_sqr(nlen, bd))
    config64.replace(target, "INLINE_REDC2", config64.inline_redc2(nlen, bd))
    config64.replace(target, "INLINE_REDC1", config64.inline_redc1(nlen, bd))
    os.replace(target, os.path.join("..", target))
    print("generated ../" + target)

    # fp_<tf>.cpp
    emit("fp.cpp", "fp_%s.cpp" % TF)
    # ecp_<tc>.cpp
    emit("ecp.cpp", "ecp_%s.cpp" % TC, zzz=True)
    # pairing-friendly (BLS12): fp2, fp4, fp12, ecp2, pair
    emit("fp2.cpp", "fp2_%s.cpp" % TF)
    emit("fp4.cpp", "fp4_%s.cpp" % TF, zzz=True)
    emit("fp12.cpp", "fp12_%s.cpp" % TF, zzz=True)
    emit("ecp2.cpp", "ecp2_%s.cpp" % TC, zzz=True)
    emit("pair.cpp", "pair_%s.cpp" % TC, zzz=True)

    # Dropped from the upstream curveset sequence on purpose (unused by the
    # nitch layer, provably unrelated families): ecdh, eddsa, hpke, mpin, bls.


if __name__ == "__main__":
    main()
