#!/usr/bin/env python3
"""Generate background ON transactions through one geth RPC endpoint.

The script expects accounts to be already imported and unlocked on the target
node. It rotates sender accounts and submits transactions with eth_sendTransaction.
"""

from __future__ import annotations

import argparse
import json
import signal
import sys
import time
import urllib.request
from pathlib import Path
from typing import Any


STOP = False
URL_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def handle_stop(_signum: int, _frame: Any) -> None:
    global STOP
    STOP = True


def load_addresses(path: Path) -> list[str]:
    raw = json.loads(path.read_text())
    if not isinstance(raw, list):
        raise ValueError(f"{path} must contain a JSON list")
    addresses: list[str] = []
    for item in raw:
        if isinstance(item, str):
            address = item
        elif isinstance(item, dict):
            address = str(item.get("address", ""))
        else:
            raise ValueError(f"unsupported account item in {path}: {item!r}")
        if not address.startswith("0x") or len(address) != 42:
            raise ValueError(f"invalid address in {path}: {address!r}")
        addresses.append(address)
    if not addresses:
        raise ValueError(f"{path} contains no addresses")
    return addresses


def rpc(url: str, method: str, params: Any, timeout: int = 20) -> Any:
    payload = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})
    with URL_OPENER.open(req, timeout=timeout) as resp:
        out = json.loads(resp.read().decode())
    if out.get("error"):
        raise RuntimeError(f"{method} failed: {out['error']}")
    return out.get("result")


def hex_value(value: int) -> str:
    if value < 0:
        raise ValueError("value must be non-negative")
    return hex(value)


def tx_data(seq: int, data: str) -> str | None:
    if not data:
        return None
    if data.startswith("0x"):
        return data
    return "0x" + data.format(seq=seq).encode().hex()


def main() -> int:
    parser = argparse.ArgumentParser(description="Send ON background transactions")
    parser.add_argument("--rpc", required=True, help="node1 ON RPC URL")
    parser.add_argument("--accounts", required=True, type=Path, help="JSON list of unlocked ON load accounts")
    parser.add_argument("--receivers", required=True, type=Path, help="JSON list of receiver addresses")
    parser.add_argument("--rate", required=True, type=float, help="ON_TX_PERS: transactions per second")
    parser.add_argument("--tx-per-block", type=float, default=0.0,
                        help="closed-loop pacing target: keep ~N txs per mined block by adapting "
                             "the send rate to the measured block interval (0 disables)")
    parser.add_argument("--duration", type=float, default=0.0, help="seconds to run; 0 means until SIGTERM")
    parser.add_argument("--value", default="0x1", help="transaction value, hex string or decimal integer")
    parser.add_argument("--gas", default="0x5208", help="transaction gas, hex string or decimal integer")
    parser.add_argument("--data", default="", help="optional tx input; text may use {seq}")
    parser.add_argument("--log", type=Path, default=None, help="JSONL output path")
    parser.add_argument("--status-interval", type=float, default=30.0, help="seconds between status prints")
    args = parser.parse_args()

    if args.rate <= 0:
        raise ValueError("--rate must be > 0")
    senders = load_addresses(args.accounts)
    receivers = load_addresses(args.receivers)
    value = args.value if str(args.value).startswith("0x") else hex_value(int(args.value))
    gas = args.gas if str(args.gas).startswith("0x") else hex_value(int(args.gas))

    signal.signal(signal.SIGTERM, handle_stop)
    signal.signal(signal.SIGINT, handle_stop)

    log_fp = args.log.open("a") if args.log else None
    interval = 1.0 / args.rate
    tx_per_block = args.tx_per_block
    # Closed-loop pacing state (per-block reactive control):
    #  - aim interval = latest_block_interval / tx_per_block;
    #  - empty block (pool drained) => halve the interval;
    #  - overshoot beyond target => proportional backoff;
    #  - clamp to [min_iv, max_iv] derived from --rate so one bad sample
    #    (e.g. the genesis->block1 gap) can never freeze the sender;
    #  - RPC hiccups decay toward the base rate instead of keeping a
    #    possibly stale huge interval.
    last_block = 0
    last_block_at = time.monotonic()
    measured_interval = 0.0
    last_tx_count = 0
    budget = tx_per_block if tx_per_block > 0 else None
    sent_since_block = 0
    rpc_fail_streak = 0
    start = time.monotonic()
    next_at = start
    last_status = start
    sent = 0
    failures = 0
    print(
        f"[tx.py] start rpc={args.rpc} accounts={len(senders)} receivers={len(receivers)} "
        f"rate={args.rate:.3f}/s tx_per_block={tx_per_block or 'off'} duration={args.duration}",
        flush=True,
    )

    try:
        while not STOP:
            now = time.monotonic()
            if args.duration > 0 and now - start >= args.duration:
                break

            if tx_per_block > 0:
                min_iv = 0.05
                max_spacing = max(2.0 / args.rate, 1.0)
                try:
                    tip_hex = rpc(args.rpc, "eth_blockNumber", [], timeout=10)
                    tip = int(tip_hex, 16)
                    rpc_fail_streak = 0
                    if tip > last_block:
                        blk = rpc(args.rpc, "eth_getBlockByNumber", [hex(tip), False], timeout=10)
                        tx_count = len(blk.get("transactions") or []) if blk else 0
                        now2 = time.monotonic()
                        # Measure the true block interval — no clamp. A giant
                        # block genuinely takes long to seal, and pretending
                        # otherwise is what fed the pool into a positive
                        # feedback loop. The send cap below makes a long true
                        # interval safe to observe.
                        block_iv = now2 - last_block_at
                        last_block_at = now2
                        last_block = tip
                        last_tx_count = tx_count
                        measured_interval = block_iv
                        # Strict per-block budget: this window may send only
                        # what the NEXT block should carry, corrected once for
                        # what the last block actually carried (deficit adds,
                        # overshoot subtracts; floor 1, ceiling 2x target).
                        budget = min(max(2 * tx_per_block - tx_count, 1.0), 2 * tx_per_block)
                        sent_since_block = 0
                        # Spacing only spreads the budget inside the window;
                        # it can never authorize extra sends.
                        interval = min(max(block_iv / tx_per_block, min_iv), max_spacing)
                except Exception:
                    rpc_fail_streak += 1
                    if rpc_fail_streak > 5:
                        # Cannot observe blocks at all: fail open to plain
                        # base-rate pacing so the run does not starve.
                        budget = None
                        interval = max(1.0 / args.rate, min_iv)

            # Budget gate: once this block window's allowance is spent, wait
            # for the next real block instead of sending on a timer.
            if tx_per_block > 0 and budget is not None and sent_since_block >= budget:
                time.sleep(0.2)
                continue

            if now < next_at:
                time.sleep(min(next_at - now, 0.2))
                continue

            seq = sent + failures
            sender = senders[seq % len(senders)]
            receiver = receivers[seq % len(receivers)]
            payload: dict[str, str] = {
                "from": sender,
                "to": receiver,
                "value": value,
                "gas": gas,
            }
            data = tx_data(seq, args.data)
            if data:
                payload["data"] = data

            record: dict[str, Any] = {
                "seq": seq,
                "sender": sender,
                "receiver": receiver,
                "start_ns": time.time_ns(),
            }
            try:
                tx_hash = rpc(args.rpc, "eth_sendTransaction", [payload], timeout=20)
                sent += 1
                sent_since_block += 1
                record["tx_hash"] = tx_hash
                record["ok"] = True
            except Exception as err:
                failures += 1
                record["ok"] = False
                record["error"] = str(err)
                if failures <= 5 or failures % 20 == 0:
                    print(f"[tx.py] send failed failures={failures} err={err}", flush=True)
            finally:
                record["end_ns"] = time.time_ns()
                if log_fp:
                    log_fp.write(json.dumps(record, separators=(",", ":")) + "\n")
                    log_fp.flush()

            next_at += interval
            if time.monotonic() - last_status >= args.status_interval:
                elapsed = max(time.monotonic() - start, 1e-9)
                rate_note = (
                    f" effective_rate={1.0 / interval:.3f}/s blk_iv={measured_interval:.2f}s"
                    f" last_blk_txs={last_tx_count} budget={budget} inflight={sent_since_block}"
                    if tx_per_block > 0 and measured_interval > 0 else ""
                )
                print(
                    f"[tx.py] status sent={sent} failures={failures} "
                    f"elapsed={elapsed:.1f}s actual_rate={sent / elapsed:.3f}/s{rate_note}",
                    flush=True,
                )
                last_status = time.monotonic()
    finally:
        if log_fp:
            log_fp.close()

    elapsed = max(time.monotonic() - start, 1e-9)
    print(
        f"[tx.py] stopped sent={sent} failures={failures} "
        f"elapsed={elapsed:.1f}s actual_rate={sent / elapsed:.3f}/s",
        flush=True,
    )
    return 0 if failures == 0 else 2


if __name__ == "__main__":
    raise SystemExit(main())
