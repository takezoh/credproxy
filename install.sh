#!/usr/bin/env bash
# credproxy: build and install this repository's own executables.
#
#   credproxyd  — shared credential broker daemon
#   credproxy   — client / delivery helper
#
# Both are built from the checkout this script lives in and installed onto the
# user's PATH. Consumer wiring (config, hooks, wrappers, service) is owned by the
# caller's environment, not by this script.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
BINDIR="${BINDIR:-$HOME/.local/bin}"

log() {
    printf '%s\n' "credproxy: $*" >&2
}

if ! command -v go >/dev/null 2>&1; then
    log "ERROR go unavailable; credproxy binaries were not built"
    exit 2
fi

for source in ./cmd/credproxyd ./cmd/credproxy; do
    if [ ! -d "$REPO_DIR/$source" ]; then
        log "ERROR source incomplete (missing $source); credproxy binaries were not built"
        exit 2
    fi
done

mkdir -p "$BINDIR"
log "building credproxyd + credproxy -> $BINDIR"
( cd "$REPO_DIR" && go build -o "$BINDIR/credproxyd" ./cmd/credproxyd )
( cd "$REPO_DIR" && go build -o "$BINDIR/credproxy" ./cmd/credproxy )
log "install done ($BINDIR/credproxyd, $BINDIR/credproxy)"
