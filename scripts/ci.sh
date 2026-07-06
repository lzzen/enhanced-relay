#!/usr/bin/env bash
# Self-contained CI pipeline. Runs the same gates as .github/workflows/verify.yml
# but with zero dependency on GitHub. Run it locally via `make ci` (Linux/WSL)
# or `make ci-docker` (Windows, inside a Linux container for race parity).
set -euo pipefail

echo "== [1/3] gofmt check =="
unformatted="$(gofmt -l . || true)"
if [ -n "$unformatted" ]; then
  echo "not gofmt-clean:"
  echo "$unformatted"
  exit 1
fi
echo "gofmt: OK"

echo "== [2/3] make verify (race + requirement traceability) =="
make verify

echo "== [3/3] mutation testing (anti-gaming) =="
export PATH="$PATH:$(go env GOPATH)/bin"
# Pin the version for reproducibility; @latest makes CI non-deterministic.
GREMLINS_VERSION="${GREMLINS_VERSION:-v0.6.0}"
if ! command -v gremlins >/dev/null 2>&1; then
  echo "installing gremlins ${GREMLINS_VERSION}..."
  # Retry: the checksum DB (sum.golang.org) occasionally returns EOF.
  n=0
  until go install "github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}"; do
    n=$((n + 1))
    if [ "$n" -ge 3 ]; then
      echo "ci: could not install gremlins after ${n} attempts (network?)"
      exit 1
    fi
    echo "install failed, retrying (${n}/3)..."
    sleep 3
  done
fi

# MUTATION_THRESHOLD is the minimum efficacy % each core package must reach.
# Default 0 = report-only (baseline). Raise it once a baseline is known to make
# mutation a real blocking gate (docs/ai-testing-acceptance.md §5.1).
THRESH="${MUTATION_THRESHOLD:-0}"
CORE_PKGS="./internal/hook ./internal/event ./internal/plugin ./internal/plugin/builtin ./internal/pipeline"

# Force fresh test runs: a cached go-build/test result makes gremlins gather
# empty coverage (0 mutants -> false pass). -count=1 disables the test cache.
export GOFLAGS="${GOFLAGS:-} -count=1"

# MCOVER is a floor on mutator coverage. It also guards against a degenerate
# "0 mutants -> 0% -> false pass" state (e.g. from a poisoned build cache).
MCOVER="${MCOVER_THRESHOLD:-50}"

mkdir -p build
mut_json="build/mutation.json"
printf '{\n  "threshold": %s,\n  "mcover_threshold": %s,\n  "packages": [\n' "$THRESH" "$MCOVER" > "$mut_json"

rc=0
first=1
for p in $CORE_PKGS; do
  echo "--- gremlins: $p (efficacy>=${THRESH}%, mcover>=${MCOVER}%) ---"
  out="$(gremlins unleash "$p" --threshold-efficacy "$THRESH" --threshold-mcover "$MCOVER" 2>&1)" || rc=$?
  echo "$out"
  killed=$(printf '%s' "$out"     | grep -oE 'Killed: [0-9]+'          | grep -oE '[0-9]+' | head -1); killed=${killed:-0}
  lived=$(printf '%s' "$out"      | grep -oE 'Lived: [0-9]+'           | grep -oE '[0-9]+' | head -1); lived=${lived:-0}
  notcov=$(printf '%s' "$out"     | grep -oE 'Not covered: [0-9]+'     | grep -oE '[0-9]+' | head -1); notcov=${notcov:-0}
  eff=$(printf '%s' "$out"        | grep -oE 'Test efficacy: [0-9.]+'  | grep -oE '[0-9.]+' | head -1); eff=${eff:-0}
  mcov=$(printf '%s' "$out"       | grep -oE 'Mutator coverage: [0-9.]+'| grep -oE '[0-9.]+' | head -1); mcov=${mcov:-0}
  [ "$first" -eq 1 ] || printf ',\n' >> "$mut_json"; first=0
  printf '    {"package":"%s","killed":%s,"lived":%s,"notCovered":%s,"efficacy":%s,"mcover":%s}' \
    "$p" "$killed" "$lived" "$notcov" "$eff" "$mcov" >> "$mut_json"
done

pass_bool=true; [ "$rc" -eq 0 ] || pass_bool=false
printf '\n  ],\n  "pass": %s\n}\n' "$pass_bool" >> "$mut_json"

# Regenerate the dashboard so it now includes mutation results.
go run ./cmd/dashboard || true

if [ "$rc" -ne 0 ]; then
  echo "ci: mutation gate failed (efficacy below ${THRESH}% or mcover below ${MCOVER}%)"
  exit "$rc"
fi
echo "ci: OK  (dashboard: build/dashboard.html)"
