#!/usr/bin/env bash
# CI on this machine: the same tools at the same versions, the same commands, in the same order as .github/workflows/ci.yml, so that a
# green run here is not a different test from the green run there. What it fetches it reads from the workflow and the chart (the helm
# version, the collector version and its checksum), so there is nothing here to keep in step by hand.
#
#   scripts/ci-local.sh tools      fetch helm and the collector into the cache and print what to export; run nothing
#   scripts/ci-local.sh backend    build, vet and test the Go code
#   scripts/ci-local.sh frontend   lint, type-check and build, unit tests, component tests (E2E=1 adds the browser tests)
#   scripts/ci-local.sh            both
#
#   RACE=1               go test -race, as CI does (slower; default off)
#   PKGS='./internal/chart'   only these Go packages (default ./...)
#   IKHNOS_TEST_NEO4J=http://127.0.0.1:7474  a Neo4j that answers there (and NEO4J_PASSWORD) makes the graph tests run and the
#                        "tools are here" test required, as in CI; without it the graph tests skip, and this says so
#   CI_TOOLS=dir         where the tools are kept (default ~/.cache/ikhnos-ci)
#
# Linux amd64 and arm64 and macOS can fetch helm; the collector the chart tests validate against is pinned (with its checksum) for linux
# amd64 only, so elsewhere those tests skip and this says so rather than letting a skip pass for a run.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
ci="$root/.github/workflows/ci.yml"
tools="${CI_TOOLS:-$HOME/.cache/ikhnos-ci}"
mkdir -p "$tools"

say() { printf '\n== %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) arch="$(uname -m)" ;;
esac

# --- helm, at the version CI installs ---------------------------------------------------------------------------------------------
helm_want="$(awk '/azure\/setup-helm/{f=1} f&&/version:/{print $2; exit}' "$ci")"
[ -n "$helm_want" ] || { echo "no helm version found in $ci" >&2; exit 1; }

fetch_helm() {
  local dir="$tools/helm-$helm_want"
  if [ ! -x "$dir/helm" ]; then
    say "fetching helm $helm_want"
    local t
    t="$(mktemp -d)"
    curl -fsSL -o "$t/h.tgz" "https://get.helm.sh/helm-$helm_want-$os-$arch.tar.gz"
    tar -xzf "$t/h.tgz" -C "$t"
    mkdir -p "$dir"
    mv "$t/$os-$arch/helm" "$dir/helm"
    rm -rf "$t"
  fi
  export PATH="$dir:$PATH"
  local have
  have="$(helm version --short | cut -d+ -f1)"
  [ "$have" = "$helm_want" ] || { echo "helm on the PATH is $have, CI uses $helm_want" >&2; exit 1; }
}

# --- the collector the chart pins, with the checksum CI holds it to -----------------------------------------------------------------
fetch_collector() {
  collector=""
  if [ "$os-$arch" != "linux-amd64" ]; then
    warn "the pinned collector is for linux amd64: the chart tests that run it will skip here"
    return 0
  fi
  local want sum have
  want="$(awk '/want=/{sub(/want=/, ""); print $1; exit}' "$ci")"
  sum="$(awk '/sum=/{sub(/sum=/, ""); print $1; exit}' "$ci")"
  have="$(awk '/collectorImage:/{f=1} f&&/tag:/{gsub(/"/, "", $2); print $2; exit}' "$root/backend/internal/chart/ikhnos-agent/values.yaml")"
  if [ "$have" != "$want" ]; then
    echo "the agent chart pins collector $have and ci.yml downloads $want: change the version and the checksum there together" >&2
    exit 1
  fi
  local bin="$tools/otelcol-contrib-$want"
  if [ ! -x "$bin" ]; then
    say "fetching otelcol-contrib $want"
    local t
    t="$(mktemp -d)"
    curl -fsSL -o "$t/o.tgz" "https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v${want}/otelcol-contrib_${want}_linux_amd64.tar.gz"
    echo "${sum}  $t/o.tgz" | sha256sum -c -
    tar -xzf "$t/o.tgz" -C "$t" otelcol-contrib
    mv "$t/otelcol-contrib" "$bin"
    rm -rf "$t"
  fi
  collector="$bin"
}

# --- the pod identity the collector's kubelet receiver reads (a fixed path, so it needs the rights to write there) ----------------------
pod_identity() {
  local d=/var/run/secrets/kubernetes.io/serviceaccount
  [ "$os" = linux ] || return 0
  if [ -s "$d/token" ] && [ -s "$d/ca.crt" ]; then return 0; fi
  local sudo=""
  [ "$(id -u)" -eq 0 ] || sudo="sudo -n"
  if $sudo mkdir -p "$d" 2>/dev/null; then
    echo ci-token | $sudo tee "$d/token" >/dev/null
    $sudo openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -keyout /dev/null -out "$d/ca.crt" -subj "/CN=ci" -days 2 2>/dev/null
  else
    warn "cannot write $d (needs root): the tests that ask the real collector to validate a host config will fail or skip"
  fi
}

backend() {
  fetch_helm
  fetch_collector
  pod_identity
  local env=()
  if [ -n "${collector:-}" ]; then env+=("IKHNOS_OTELCOL=$collector" "OTELCOL_CONTRIB=$collector"); fi
  if [ -n "${IKHNOS_TEST_NEO4J:-}" ]; then
    env+=("IKHNOS_REQUIRE_TOOLS=1" "IKHNOS_TEST_NEO4J=$IKHNOS_TEST_NEO4J" "IKHNOS_TEST_NEO4J_PASSWORD=${NEO4J_PASSWORD:-}")
  else
    warn "no IKHNOS_TEST_NEO4J: the graph tests skip, and the test that holds the tools to be here is not required"
  fi
  cd "$root/backend"
  say "go build"
  go build ./...
  say "go vet"
  go vet ./...
  say "go test${RACE:+ -race}"
  # shellcheck disable=SC2086
  env ${env[@]+"${env[@]}"} go test ${PKGS:-./...} ${RACE:+-race} -count=1
}

frontend() {
  cd "$root"
  [ -d node_modules ] || { say "npm ci"; npm ci; }
  say "lint";            npm run lint
  say "type-check and build"; npm run build
  say "unit tests";      npm run test:unit
  say "component tests"; npm run test:ui
  if [ "${E2E:-}" = 1 ]; then
    say "e2e tests"
    npx playwright install --with-deps chromium
    npm run test:e2e
  else
    warn "E2E=1 runs the browser tests; CI always does"
  fi
}

case "${1:-all}" in
  tools)
    fetch_helm
    fetch_collector
    echo "export PATH=\"$(dirname "$(command -v helm)"):\$PATH\""
    [ -z "${collector:-}" ] || echo "export IKHNOS_OTELCOL=$collector OTELCOL_CONTRIB=$collector"
    ;;
  backend) backend ;;
  frontend) frontend ;;
  all) backend; frontend ;;
  *) echo "usage: $0 [tools|backend|frontend|all]" >&2; exit 2 ;;
esac
say "done"
