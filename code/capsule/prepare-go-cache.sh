#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT=${CACHE_SOURCE_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}
export PATH="/opt/tgovern/go/bin:$PATH"
experiment=sgx
if [[ "${1:-}" == --experiment ]]; then
    [[ $# -ge 2 ]] || { printf 'Missing experiment name\n' >&2; exit 2; }
    experiment=$2
    shift 2
fi
[[ $# -le 1 ]] || { printf 'Usage: prepare-go-cache.sh [--experiment sgx|nitch] [output]\n' >&2; exit 2; }
configuration=$(python3 - "$SCRIPT_DIR" "$experiment" <<'PY'
import sys
sys.path.insert(0, sys.argv[1])
from experiment_config import EXPERIMENTS
if sys.argv[2] not in EXPERIMENTS:
    raise SystemExit('unknown experiment')
profile = EXPERIMENTS[sys.argv[2]]
print(profile.cache_name)
print('\n'.join(profile.modules))
PY
)
mapfile -t profile <<< "$configuration"
modules=("${profile[@]:1}")
[[ "$(go env GOOS)/$(go env GOARCH)" == linux/amd64 ]] || { printf 'Prepare the cache inside the Linux amd64 environment\n' >&2; exit 1; }
[[ "$(go env GOVERSION)" == go1.20.14 ]] || { printf 'Requires Go 1.20.14\n' >&2; exit 1; }
output=${1:-/data/${profile[0]}}
mkdir -p "$(dirname "$output")"
[[ ! -e "$output" && ! -e "$output.sha256" ]] || { printf 'Refusing to overwrite %s\n' "$output" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export GOMODCACHE="$work/modules" GOPATH="$work/gopath" GOCACHE="$work/build-cache" GOTOOLCHAIN=local
export CGO_ENABLED=1
for module in "${modules[@]}"; do
    printf 'Preparing dependencies for %s\n' "$module"
    mkdir -p "$work/$module"
    cp "$ROOT/$module/go.mod" "$ROOT/$module/go.sum" "$work/$module/"
    (cd "$work/$module" && go mod download all)
done
python3 - "$ROOT" "$work/modules/manifest.json" "${modules[@]}" <<'PY'
import hashlib, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
files = [f'{module}/{name}' for module in sys.argv[3:] for name in ('go.mod', 'go.sum')]
pathlib.Path(sys.argv[2]).write_text(json.dumps({
    'go_version': 'go1.20.14', 'target': 'linux/amd64',
    'inputs': {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in files},
}, indent=2) + '\n')
PY
tar -czf "$work/go-mod-cache.tar.gz" -C "$work/modules" manifest.json cache/download
# A hard link publishes a complete archive without replacing an existing one.
cp "$work/go-mod-cache.tar.gz" "$output.partial.$$"
ln "$output.partial.$$" "$output"
rm "$output.partial.$$"
(cd "$(dirname "$output")" && sha256sum "$(basename "$output")" > "$(basename "$output").sha256")
printf 'Prepared %s\n' "$output"
