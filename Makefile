export GO111MODULE=on
export GOFLAGS=-mod=vendor

# Build/test tags. Defaults to enabling github.com/negrel/assert assertions.
# Override with e.g.:
#   make TAGS=
#   make TAGS="assert,other"
TAGS ?= assert
GO_TAG_FLAGS :=
ifneq ($(strip $(TAGS)),)
GO_TAG_FLAGS := -tags $(TAGS)
endif

.PHONY: build
build: cmd/gptcli

cmd/gptcli: vendor FORCE
	go build $(GO_TAG_FLAGS) -o gptcli cmd/gptcli/*.go

vendor: go.mod
	go mod download
	go mod vendor

cmd/gptcli/version.txt:
	git describe --tags > cmd/gptcli/version.txt
	truncate -s -1 cmd/gptcli/version.txt

.PHONY: mocks
mocks:
	cd internal/types; go generate

TESTPKGS=github.com/mikeb26/gptcli/cmd/gptcli github.com/mikeb26/gptcli/internal github.com/mikeb26/gptcli/internal/prompts github.com/mikeb26/gptcli/internal/ui github.com/mikeb26/gptcli/internal/am github.com/mikeb26/gptcli/internal/llmclient github.com/mikeb26/gptcli/internal/threads github.com/mikeb26/gptcli/internal/scm github.com/mikeb26/gptcli/internal/scm/git github.com/mikeb26/gptcli/internal/tools github.com/mikeb26/gptcli/internal/workspace

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

.PHONY: clean
clean:
	rm -f gptcli unit-tests.xml

.PHONY: deps
deps:
	rm -rf go.mod go.sum vendor
	go mod init github.com/mikeb26/gptcli
	go mod edit -replace=github.com/rthornton128/goncurses=github.com/mikeb26/rthornton128-goncurses@bc9261688f2c003b706dacc3a9437181cb864bbe
	GOPROXY=direct go mod tidy
	go mod vendor

FORCE:
