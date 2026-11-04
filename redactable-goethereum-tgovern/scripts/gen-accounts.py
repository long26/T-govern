#!/usr/bin/env python3
"""Generate all_privatekeys.json for local T-govern dev chains."""

from __future__ import annotations

import argparse
import json
import time
from pathlib import Path

from eth_account import Account


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("-n", "--nodes", type=int, default=18)
    parser.add_argument("-o", "--out", default="/opt/tgovern/config/all_privatekeys.json")
    args = parser.parse_args()

    if args.nodes <= 0:
        raise SystemExit("--nodes must be > 0")

    payload: dict[str, dict[str, dict[str, str]]] = {"nodes": {}}
    for i in range(1, args.nodes + 1):
        account = Account.create(f"tgovern-node-{i}-{time.time_ns()}")
        payload["nodes"][f"node{i}"] = {
            "address": account.address,
            "privateKey": account.key.hex().removeprefix("0x"),
        }

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(payload, indent=2) + "\n")
    print(f"generated {args.nodes} accounts: {out}")


if __name__ == "__main__":
    main()
