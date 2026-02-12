export GO111MODULE=on
export GOFLAGS=-mod=vendor

# Packaging / sandbox parameters.
CLI_TOOL_NAME ?= octium
CLI_SANDBOX_USERNAME ?= octium
CLI_SANDBOX_GROUPNAME ?= octium-users
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
	-X github.com/mikeb26/octium/internal.CliSandboxUsername=$(CLI_SANDBOX_USERNAME) \
	-X github.com/mikeb26/octium/internal.CliSandboxGroupname=$(CLI_SANDBOX_GROUPNAME)

# Versioning.
#
# cmd/$(NCLI_TOOL_NAME)/version.txt is the source of truth for the build/version.
# For Debian packaging, we derive the package version from version.txt:
#   - release tags: vMAJOR.MINOR.PATCH  ->  MAJOR.MINOR.PATCH-1
#   - otherwise:                         0.0.0+git
CLI_VERSION_RAW = $(shell cat cmd/$(NCLI_TOOL_NAME)/version.txt 2>/dev/null)
CLI_DEB_VERSION = $(shell \
	v="$(CLI_VERSION_RAW)"; \
	if printf '%s' "$$v" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		printf '%s-1\n' "$${v#v}"; \
	else \
		printf '%s\n' '0.0.0+git'; \
	fi)

.PHONY: build
build: cmd/$(NCLI_TOOL_NAME)

cmd/$(NCLI_TOOL_NAME): vendor FORCE
	go build $(GO_TAG_FLAGS) -ldflags "$(GO_LDFLAGS)" -o $(NCLI_TOOL_NAME) ./cmd/$(NCLI_TOOL_NAME)

vendor: go.mod
	go mod download
	go mod vendor

cmd/$(NCLI_TOOL_NAME)/version.txt:
	git describe --tags --always > cmd/$(NCLI_TOOL_NAME)/version.txt
	truncate -s -1 cmd/$(NCLI_TOOL_NAME)/version.txt

.PHONY: mocks
mocks:
	cd internal/types; go generate

TESTPKGS=github.com/mikeb26/octium/cmd/$(NCLI_TOOL_NAME) github.com/mikeb26/octium/internal github.com/mikeb26/octium/internal/prompts github.com/mikeb26/octium/internal/ui github.com/mikeb26/octium/internal/am github.com/mikeb26/octium/internal/llmclient github.com/mikeb26/octium/internal/threads github.com/mikeb26/octium/internal/scm github.com/mikeb26/octium/internal/scm/git github.com/mikeb26/octium/internal/tools github.com/mikeb26/octium/internal/workspace

# Enable the race detector by default for `make test`. You can disable with:
#   make test RACE=0
RACE ?= 1
TESTFLAGS ?=
ifeq ($(RACE),1)
TESTFLAGS += -race
endif

.PHONY: test
test: mocks
	go test $(GO_TAG_FLAGS) $(TESTFLAGS) $(TESTPKGS)

unit-tests.xml: mocks FORCE
	gotestsum --junitfile unit-tests.xml -- $(GO_TAG_FLAGS) $(TESTFLAGS) $(TESTPKGS)

.PHONY: lint
lint:
	golangci-lint run ./...

PKG_COMMON_SYSUSERS_OUT := pkg/common/sysusers.d/$(CLI_TOOL_NAME)-$(CLI_SANDBOX_USERNAME).conf
PKG_COMMON_TMPFILES_OUT := pkg/common/tmpfiles.d/$(CLI_TOOL_NAME)-$(CLI_SANDBOX_USERNAME).conf
PKG_COMMON_SUDOERS_OUT := pkg/common/sudoers.d/$(CLI_SANDBOX_GROUPNAME)-$(CLI_SANDBOX_USERNAME)-echo
PKG_COMMON_POSTINSTALL_OUT := pkg/common/libexec/$(CLI_TOOL_NAME)-postinstall-common.sh
PKG_COMMON_RUN_AS_OUT := pkg/common/libexec/run-as-$(CLI_SANDBOX_USERNAME)
PKG_COMMON_GITCONFIG_OUT := pkg/common/share/gitconfig

PKG_RENDERED_FILES := \
	$(PKG_COMMON_SYSUSERS_OUT) \
	$(PKG_COMMON_TMPFILES_OUT) \
	$(PKG_COMMON_SUDOERS_OUT) \
	$(PKG_COMMON_POSTINSTALL_OUT) \
	$(PKG_COMMON_RUN_AS_OUT) \
	$(PKG_COMMON_GITCONFIG_OUT) \
	pkg/deb/debian/changelog \
	pkg/deb/debian/octium.install \
	pkg/deb/debian/octium.postinst \
	pkg/deb/debian/rules \
	pkg/deb/debian/README.Debian \
	pkg/README.md \
	pkg/deb/README.md

.PHONY: pkg-generate
pkg-generate: $(PKG_RENDERED_FILES)

.PHONY: deb
deb: cmd/$(NCLI_TOOL_NAME)/version.txt pkg-generate
	cd pkg/deb && ./build.sh -d

$(PKG_COMMON_SYSUSERS_OUT): pkg/common/sysusers.d/octium-aiagent.conf.in
$(PKG_COMMON_TMPFILES_OUT): pkg/common/tmpfiles.d/octium-aiagent.conf.in
$(PKG_COMMON_SUDOERS_OUT): pkg/common/sudoers.d/octium-share-aiagent-echo.in
$(PKG_COMMON_POSTINSTALL_OUT): pkg/common/libexec/octium-postinstall-common.sh.in
$(PKG_COMMON_RUN_AS_OUT): pkg/common/libexec/run-as-aiagent.in
$(PKG_COMMON_GITCONFIG_OUT): pkg/common/share/gitconfig.in

pkg/deb/debian/changelog: pkg/deb/debian/changelog.in cmd/$(NCLI_TOOL_NAME)/version.txt
pkg/deb/debian/octium.install: pkg/deb/debian/octium.install.in
pkg/deb/debian/octium.postinst: pkg/deb/debian/octium.postinst.in
pkg/deb/debian/rules: pkg/deb/debian/rules.in
pkg/deb/debian/README.Debian: pkg/deb/debian/README.Debian.in
pkg/README.md: pkg/README.md.in
pkg/deb/README.md: pkg/deb/README.md.in

$(PKG_RENDERED_FILES):
	@mkdir -p $(dir $@)
	@sed \
		-e 's|@CLI_TOOL_NAME@|$(CLI_TOOL_NAME)|g' \
		-e 's|@CLI_SANDBOX_USERNAME@|$(CLI_SANDBOX_USERNAME)|g' \
		-e 's|@CLI_SANDBOX_GROUPNAME@|$(CLI_SANDBOX_GROUPNAME)|g' \
		-e 's|@CLI_DEB_VERSION@|$(CLI_DEB_VERSION)|g' \
		"$<" > "$@"
	@chmod --reference="$<" "$@"

.PHONY: clean
clean:
	rm -f $(NCLI_TOOL_NAME) unit-tests.xml /tmp/$(CLI_TOOL_NAME)*.deb

.PHONY: pkg-clean
pkg-clean:
	rm -f $(PKG_RENDERED_FILES)

.PHONY: deps
deps:
	rm -rf go.mod go.sum vendor
	go mod init github.com/mikeb26/octium
	go mod edit -replace=github.com/rthornton128/goncurses=github.com/mikeb26/rthornton128-goncurses@bc9261688f2c003b706dacc3a9437181cb864bbe
	GOPROXY=direct go mod tidy
	go mod vendor

FORCE:
