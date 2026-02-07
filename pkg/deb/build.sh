#!/bin/sh
set -eu

# Helper to build a .deb from the packaging templates in pkg/deb/debian/
# without permanently copying debian/ into the repo root.

if ! command -v dpkg-buildpackage >/dev/null 2>&1; then
	printf '%s\n' "dpkg-buildpackage not found; install: sudo apt-get install devscripts" >&2
	exit 1
fi

if ! command -v go >/dev/null 2>&1; then
	printf '%s\n' "go not found. Install golang-go (bootstrap) or install Go from https://go.dev/dl/" >&2
	exit 1
fi

# This repo requires go1.24.12+ via the go.mod toolchain directive.
# If your system Go is too old to bootstrap toolchain downloads, install a newer
# Go directly from go.dev.
export GOTOOLCHAIN=auto

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
WORK_DIR=$(mktemp -d)

cleanup() {
	rm -rf "$WORK_DIR"
}
trap cleanup EXIT

# Copy source tree
( cd "$ROOT_DIR" && tar -c --exclude=.git . ) | ( cd "$WORK_DIR" && tar -x )

# Copy debian/ templates
cp -a "$ROOT_DIR/pkg/deb/debian" "$WORK_DIR/debian"

cd "$WORK_DIR"
# Forward any dpkg-buildpackage flags from the caller (e.g. "-d" to ignore
# build-deps when using a Go toolchain installed outside of dpkg/apt).
dpkg-buildpackage -us -uc -b "$@"

printf '%s\n' "Built artifacts are in: $(dirname "$WORK_DIR")" >&2
