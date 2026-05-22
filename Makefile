export GO111MODULE=on
export GOFLAGS=-mod=vendor

# Packaging / sandbox parameters.
CLI_TOOL_NAME ?= octium
NCLI_TOOL_NAME ?= n$(CLI_TOOL_NAME)

# Build/test tags. Defaults to enabling github.com/negrel/assert assertions.
# Override with e.g.:
#   make TAGS=
#   make TAGS="assert,other"
TAGS ?= assert
GO_TAG_FLAGS :=
ifneq ($(strip $(TAGS)),)
GO_TAG_FLAGS := -tags $(TAGS)
endif

# Go build-time variables.
#
# This is the Go equivalent of C's -D...: we inject string vars via
# `go build -ldflags "-X pkgpath.Name=value"`.
GO_LDFLAGS := \
	-X github.com/mikeb26/octium/internal.CliToolName=$(CLI_TOOL_NAME) \

# Versioning.
#
# cmd/$(NCLI_TOOL_NAME)/version.txt is the source of truth for the build/version.
# For packaging, we derive package versions from version.txt:
#   Debian:
#     - release tags: vMAJOR.MINOR.PATCH  ->  MAJOR.MINOR.PATCH-1
#     - otherwise:                         0.0.0+git
#   RPM:
#     - release tags: vMAJOR.MINOR.PATCH  ->  Version: MAJOR.MINOR.PATCH, Release: 1
#     - otherwise:                         Version: 0.0.0, Release: 0.git
CLI_VERSION_RAW = $(shell cat cmd/$(NCLI_TOOL_NAME)/version.txt 2>/dev/null)
CLI_DEB_VERSION = $(shell \
	v="$(CLI_VERSION_RAW)"; \
	if printf '%s' "$$v" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		printf '%s-1\n' "$${v#v}"; \
	else \
		printf '%s\n' '0.0.0+git'; \
	fi)
CLI_RPM_VERSION = $(shell \
	v="$(CLI_VERSION_RAW)"; \
	if printf '%s' "$$v" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		printf '%s\n' "$${v#v}"; \
	else \
		printf '%s\n' '0.0.0'; \
	fi)
CLI_RPM_RELEASE = $(shell \
	v="$(CLI_VERSION_RAW)"; \
	if printf '%s' "$$v" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		printf '%s\n' '1'; \
	else \
		printf '%s\n' '0.git'; \
	fi)

.PHONY: build
build: cmd/$(NCLI_TOOL_NAME)


cmd/$(NCLI_TOOL_NAME): vendor FORCE
	go build $(GO_TAG_FLAGS) -ldflags "$(GO_LDFLAGS)" -o $(NCLI_TOOL_NAME) ./cmd/$(NCLI_TOOL_NAME)

.PHONY: vendor
vendor: vendor/modules.txt

vendor/modules.txt: go.mod go.sum
	# Ensure we can (re)generate vendor/ even when GOFLAGS forces -mod=vendor.
	GOFLAGS=-mod=mod go mod download
	GOFLAGS=-mod=mod go mod vendor

cmd/$(NCLI_TOOL_NAME)/version.txt:
	git describe --tags --always > cmd/$(NCLI_TOOL_NAME)/version.txt
	truncate -s -1 cmd/$(NCLI_TOOL_NAME)/version.txt

.PHONY: mocks
mocks: vendor
	cd internal/types; go generate

# Enable the race detector by default for `make test`. You can disable with:
#   make test RACE=0
RACE ?= 1
TESTFLAGS ?=
ifeq ($(RACE),1)
TESTFLAGS += -race
endif

.PHONY: test
test: vendor mocks
	go test $(GO_TAG_FLAGS) $(TESTFLAGS) ./...

unit-tests.xml: vendor mocks FORCE
	gotestsum --junitfile unit-tests.xml -- $(GO_TAG_FLAGS) $(TESTFLAGS) ./...

.PHONY: lint
lint:
	golangci-lint run ./...

PKG_COMMON_POSTINSTALL_OUT := pkg/common/libexec/$(CLI_TOOL_NAME)-postinstall-common.sh
# Installed as /usr/libexec/<tool>/octium-provision-user (no .sh extension).
# debhelper's dh_install does not support renaming individual files via
# debian/*.install; if we try, it will create a directory named
# "octium-provision-user" and place the file inside.
PKG_COMMON_PROVISION_USER_OUT := pkg/common/libexec/$(CLI_TOOL_NAME)-provision-user
PKG_COMMON_RUN_AS_TEMPLATE_OUT := pkg/common/libexec/run-as.template
PKG_COMMON_GITCONFIG_OUT := pkg/common/share/gitconfig

PKG_RENDERED_FILES := \
	$(PKG_COMMON_POSTINSTALL_OUT) \
	$(PKG_COMMON_PROVISION_USER_OUT) \
	$(PKG_COMMON_RUN_AS_TEMPLATE_OUT) \
	$(PKG_COMMON_GITCONFIG_OUT) \
	pkg/deb/debian/changelog \
	pkg/deb/debian/octium.install \
	pkg/deb/debian/octium.postinst \
	pkg/deb/debian/octium.postrm \
	pkg/deb/debian/rules \
	pkg/deb/debian/README.Debian \
	pkg/rpm/octium.spec \
	pkg/README.md \
	pkg/deb/README.md \
	pkg/rpm/README.md

# Match the Debian helper's local-build behavior: skip RPM database build-dep
# checks by default because developers may install Go/rpmbuild outside dnf/rpm.
# Override with `make rpm RPMBUILD_FLAGS=` to enforce BuildRequires checks.
RPMBUILD_FLAGS ?= --nodeps

.PHONY: pkg-generate
pkg-generate: $(PKG_RENDERED_FILES)

.PHONY: deb
deb: cmd/$(NCLI_TOOL_NAME)/version.txt pkg-generate
	cd pkg/deb && sh ./build.sh -d

.PHONY: rpm
rpm: cmd/$(NCLI_TOOL_NAME)/version.txt vendor pkg-generate
	cd pkg/rpm && sh ./build.sh $(RPMBUILD_FLAGS)

$(PKG_COMMON_POSTINSTALL_OUT): pkg/common/libexec/octium-postinstall-common.sh.in
$(PKG_COMMON_PROVISION_USER_OUT): pkg/common/libexec/octium-provision-user.sh.in
$(PKG_COMMON_RUN_AS_TEMPLATE_OUT): pkg/common/libexec/run-as.template.in
$(PKG_COMMON_GITCONFIG_OUT): pkg/common/share/gitconfig.in

pkg/deb/debian/changelog: pkg/deb/debian/changelog.in cmd/$(NCLI_TOOL_NAME)/version.txt
pkg/deb/debian/octium.install: pkg/deb/debian/octium.install.in
pkg/deb/debian/octium.postinst: pkg/deb/debian/octium.postinst.in
pkg/deb/debian/octium.postrm: pkg/deb/debian/octium.postrm.in
pkg/deb/debian/rules: pkg/deb/debian/rules.in
pkg/deb/debian/README.Debian: pkg/deb/debian/README.Debian.in
pkg/rpm/octium.spec: pkg/rpm/octium.spec.in cmd/$(NCLI_TOOL_NAME)/version.txt
pkg/README.md: pkg/README.md.in
pkg/deb/README.md: pkg/deb/README.md.in
pkg/rpm/README.md: pkg/rpm/README.md.in

$(PKG_RENDERED_FILES):
	@mkdir -p $(dir $@)
	@sed \
		-e 's|@CLI_TOOL_NAME@|$(CLI_TOOL_NAME)|g' \
		-e 's|@CLI_DEB_VERSION@|$(CLI_DEB_VERSION)|g' \
		-e 's|@CLI_RPM_VERSION@|$(CLI_RPM_VERSION)|g' \
		-e 's|@CLI_RPM_RELEASE@|$(CLI_RPM_RELEASE)|g' \
		"$<" > "$@"
	@chmod --reference="$<" "$@"
	@case "$@" in \
		pkg/common/libexec/*-postinstall-common.sh|pkg/common/libexec/*-provision-user|pkg/deb/debian/*.postinst|pkg/deb/debian/*.postrm) \
			chmod 0755 "$@" ;; \
		esac

.PHONY: clean
clean:
	rm -f $(NCLI_TOOL_NAME) unit-tests.xml /tmp/$(CLI_TOOL_NAME)*.deb /tmp/$(CLI_TOOL_NAME)*.rpm

.PHONY: pkg-clean
pkg-clean:
	rm -f $(PKG_RENDERED_FILES)
	# Back-compat cleanup: older pkg-generate created an extra .sh file.
	rm -f pkg/common/libexec/$(CLI_TOOL_NAME)-provision-user.sh

.PHONY: deps
deps:
	rm -rf go.mod go.sum vendor
	go mod init github.com/mikeb26/octium
	go mod edit -go=1.26.2
	go mod edit -replace=github.com/rthornton128/goncurses=github.com/mikeb26/rthornton128-goncurses@bc9261688f2c003b706dacc3a9437181cb864bbe
	GOPROXY=direct go mod tidy
	go mod vendor

FORCE:
