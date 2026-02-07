# pkg/deb/

Debian/Ubuntu packaging templates for building a `.deb` that includes:
- `gptcli` binary
- install-time aiagent artifacts (sysusers/tmpfiles, sudoers wrapper)

## Runtime dependencies

The `gptcli` package declares dependencies on:
- `systemd`
- `git`
- `ripgrep`
- `libncursesw6`
- `ncurses-bin`

## Build dependencies

The Debian packaging declares build dependencies on:
- `golang-go` (or `golang-any`; bootstrap Go; see Go toolchain note below)
- `libncurses-dev`
- `ncurses-bin`
- `pkg-config`
- `make`
- `build-essential`
- `git`
- `ca-certificates`

### Go toolchain note (go1.24.12+)

This repo's `go.mod` uses `toolchain go1.24.12`.

- If your host has **Go 1.21+**, `go` can automatically download the required
  toolchain (when `GOTOOLCHAIN=auto`, which the packaging sets).
- If your distro `golang-go` is too old to bootstrap toolchain downloads, install
  a newer Go directly from https://go.dev/dl/ and rebuild.

Note: Some build environments (e.g., Debian buildds) restrict network access,
which can prevent automatic toolchain downloads.

## How to build locally

These templates are stored under `pkg/deb/debian/`. `dpkg-buildpackage` expects
a top-level `debian/` directory.

A simple approach:

```bash
cp -a pkg/deb/debian ./debian
sudo apt-get install --yes devscripts debhelper golang-go libncurses-dev pkg-config make build-essential

dpkg-buildpackage -us -uc -b

### If you installed Go from go.dev (outside apt)

If you have a suitable `go` in `PATH` already (e.g. `go1.24.12`) but you don't
have `golang-go` installed via apt, `dpkg-checkbuilddeps` will fail.

You can still build locally by passing `-d` to skip build-deps checking:

```bash
./pkg/deb/build.sh -d
```

For CI/official builds, it's recommended to satisfy Build-Depends via apt.
```
