#!/bin/sh
set -eu

# Helper to build an .rpm from the packaging templates in pkg/rpm/ without
# permanently creating rpmbuild artifacts in the repo root.

if ! command -v rpmbuild >/dev/null 2>&1; then
	printf '%s\n' "rpmbuild not found; install: sudo dnf install rpm-build" >&2
	exit 1
fi

if ! command -v go >/dev/null 2>&1; then
	printf '%s\n' "go not found. Install golang (bootstrap) or install Go from https://go.dev/dl/" >&2
	exit 1
fi

# This repo requires go1.26.2+ via go.mod.
# If your system Go is too old to bootstrap toolchain downloads, install a newer
# Go directly from go.dev.
export GOTOOLCHAIN=auto

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
WORK_DIR=$(mktemp -d)
TOPDIR="$WORK_DIR/rpmbuild"
SPEC_FILE="$ROOT_DIR/pkg/rpm/octium.spec"
README_FILE="$ROOT_DIR/pkg/rpm/README.md"

if [ ! -f "$SPEC_FILE" ]; then
	printf '%s\n' "missing rendered spec: $SPEC_FILE; run: make pkg-generate" >&2
	exit 1
fi
if [ ! -f "$README_FILE" ]; then
	printf '%s\n' "missing rendered README: $README_FILE; run: make pkg-generate" >&2
	exit 1
fi
if [ ! -f "$ROOT_DIR/vendor/modules.txt" ]; then
	printf '%s\n' "missing vendored dependencies: $ROOT_DIR/vendor/modules.txt; run: make vendor" >&2
	exit 1
fi
if grep -q '@CLI_' "$SPEC_FILE"; then
	printf '%s\n' "unrendered placeholders found in $SPEC_FILE; run: make pkg-generate" >&2
	exit 1
fi
if grep -q '@CLI_' "$README_FILE"; then
	printf '%s\n' "unrendered placeholders found in $README_FILE; run: make pkg-generate" >&2
	exit 1
fi

RPM_NAME=$(awk '/^Name:/ { print $2; exit }' "$SPEC_FILE")
RPM_VERSION=$(awk '/^Version:/ { print $2; exit }' "$SPEC_FILE")
if [ -z "$RPM_NAME" ] || [ -z "$RPM_VERSION" ]; then
	printf '%s\n' "unable to determine Name/Version from $SPEC_FILE" >&2
	exit 1
fi
SOURCE_DIR="$WORK_DIR/$RPM_NAME-$RPM_VERSION"
SOURCE_TAR="$TOPDIR/SOURCES/$RPM_NAME-$RPM_VERSION.tar.gz"

cleanup() {
	rm -rf "$WORK_DIR"
}
trap cleanup EXIT

mkdir -p "$TOPDIR/BUILD" "$TOPDIR/BUILDROOT" "$TOPDIR/RPMS" "$TOPDIR/SOURCES" "$TOPDIR/SPECS" "$TOPDIR/SRPMS"
mkdir -p "$SOURCE_DIR"

# Copy source tree into a versioned source directory, excluding VCS/build output.
( cd "$ROOT_DIR" && tar -c --exclude=.git --exclude='*.rpm' . ) | ( cd "$SOURCE_DIR" && tar -x )

# Ensure rendered RPM templates from the working tree are present in the source
# archive even if the copy raced with a template regeneration.
cp -a "$ROOT_DIR/pkg/rpm/octium.spec" "$SOURCE_DIR/pkg/rpm/octium.spec"
cp -a "$README_FILE" "$SOURCE_DIR/pkg/rpm/README.md"

( cd "$WORK_DIR" && tar -czf "$SOURCE_TAR" "$RPM_NAME-$RPM_VERSION" )
cp "$ROOT_DIR/pkg/rpm/octium.spec" "$TOPDIR/SPECS/octium.spec"

rpmbuild \
	--define "_topdir $TOPDIR" \
	-bb "$TOPDIR/SPECS/octium.spec" \
	"$@"

find "$TOPDIR/RPMS" -type f -name '*.rpm' -exec cp {} "${TMPDIR:-/tmp}" \;

printf '%s\n' "Built RPM artifacts:" >&2
find "${TMPDIR:-/tmp}" -maxdepth 1 -type f -name "$RPM_NAME-*.rpm" -print >&2
