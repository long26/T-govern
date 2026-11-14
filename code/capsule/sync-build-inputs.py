#!/usr/bin/env python3
"""Keep environment-only Go manifests consistent with the runtime source tree."""
import argparse
from pathlib import Path
import shutil
import sys

from experiment_config import EXPERIMENTS

CODE = Path(__file__).resolve().parents[1]
ENVIRONMENT = CODE.parent / 'environment'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    pairs = [(CODE / module / name, ENVIRONMENT / 'manifests' / module / name)
             for module in sorted({m for p in EXPERIMENTS.values() for m in p.modules})
             for name in ('go.mod', 'go.sum')]
    pairs.append((CODE / 'capsule/prepare-go-cache.sh', ENVIRONMENT / 'prepare-go-cache.sh'))
    pairs.append((CODE / 'capsule/experiment_config.py', ENVIRONMENT / 'experiment_config.py'))
    stale = []
    for source, destination in pairs:
        if args.check:
            if not destination.is_file() or source.read_bytes() != destination.read_bytes():
                stale.append(str(destination.relative_to(CODE.parent)))
        else:
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, destination)
    if stale:
        print('Stale environment build inputs: ' + ', '.join(stale), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
