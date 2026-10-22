GO ?= go
GOFMT ?= gofmt
# golangci-lint is pinned and run through `go run` so it is compiled with the
# project's Go toolchain (a prebuilt binary built with an older Go refuses a
# newer target). Override with GOLANGCI_LINT=golangci-lint to use a local binary.
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOOS ?= $(shell $(GO) env GOOS)
BIN ?= bin
DIST ?= dist
# Build metadata. An exact git tag (HEAD tagged) is a release version; anything
# else is a dev build carrying the short commit and a -dirty marker. Outside git
# everything degrades to "dev".
# Makefile.nmake cannot expand $(shell) and mirrors this block with
# scripts/versionstamp.ps1; keep the two in sync.
# Tag version: take the exact tag on HEAD only when it is made of
# [0-9A-Za-z._+-] (an optional leading "v" before a digit is stripped:
# v1.2.3 -> 1.2.3, vendor-drop stays). The filter runs inside the shell
# pipeline, so an unsafe tag never reaches make expansion: a tag like
# v1.2.3$(cmd) would otherwise be command-substituted when a recipe expands
# VERSION. Anything else is treated as untagged (dev build).
TAG_VERSION := $(shell git describe --tags --exact-match 2>/dev/null | grep -E '^v?[0-9A-Za-z._+-]+$$' | sed 's/^v\([0-9]\)/\1/')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
# "-dirty" when the working tree differs from HEAD. Unlike `git describe
# --dirty`, untracked files count too, because `go build` compiles them.
GIT_DIRTY := $(shell git status --porcelain 2>/dev/null | grep -q . && echo -dirty)

ifneq ($(TAG_VERSION),)
# A tagged-but-dirty HEAD is not a release build.
BUILD_VERSION := $(TAG_VERSION)$(GIT_DIRTY)
BUILD_KIND := $(if $(GIT_DIRTY),dev,release)
else ifneq ($(GIT_COMMIT),)
BUILD_VERSION := dev-$(GIT_COMMIT)$(GIT_DIRTY)
BUILD_KIND := dev
else
BUILD_VERSION := dev
BUILD_KIND := dev
endif

# Version stamped into binaries and release archive names; override to stamp a
# custom one (e.g. `make release VERSION=1.2.3` still requires a tagged HEAD).
# VERSION is interpolated into shell recipes, so only pass trusted values; the
# filters above cover repo-derived tags, not caller-supplied overrides.
VERSION ?= $(BUILD_VERSION)

# Link-time stamp for the commands that expose a version (cmd/agentd main.version).
VERSION_FLAG := -X main.version=$(VERSION)

ifeq ($(GOOS),windows)
EXE := .exe
else
EXE :=
endif

EXAMPLES := chat tool notesmcp weathermcp a2a-embedded agentd-client
BINARIES := $(addprefix $(BIN)/,$(addsuffix $(EXE),$(EXAMPLES)))
CMDS := agentd agentd-supervisor
CMD_BINARIES := $(addprefix $(BIN)/,$(addsuffix $(EXE),$(CMDS)))
GO_SOURCES := $(shell find . -name '*.go' -not -path './bin/*')

.PHONY: all examples cmds chat a2a-embedded agentd fmt fmt-check vet test lint golangci check-boundary release clean version FORCE

all: examples cmds

examples: $(BINARIES)

cmds: $(CMD_BINARIES)

# Build the multi-transport server command. Dev builds are stamped
# dev-<commit>[-dirty]; releases are stamped with the exact git tag. FORCE makes
# `go build` run on every make, so a new commit or a dirty tree is re-stamped
# instead of leaving a stale binary (unchanged flags still hit the build cache).
$(BIN)/agentd$(EXE): FORCE $(GO_SOURCES) go.mod go.sum
	@mkdir -p $(BIN)
	$(GO) build -ldflags "$(VERSION_FLAG)" -o $@ ./cmd/agentd

# Always out of date: used to re-evaluate the stamped version.
FORCE:

# Build the host supervisor command: it speaks the supervisor_command JSON
# protocol and satisfies it via a running agentd's control API.
$(BIN)/agentd-supervisor$(EXE): $(GO_SOURCES) go.mod go.sum
	@mkdir -p $(BIN)
	$(GO) build -o $@ ./cmd/agentd-supervisor

# Run agentd. Uses the git-ignored local config; override with
# CONFIG=path (see cmd/agentd/agentd.example.yaml). Serves until Ctrl+C.
CONFIG ?= cmd/agentd/agentd.yaml

agentd: $(BIN)/agentd$(EXE)
	$(BIN)/agentd$(EXE) --config $(CONFIG)

$(BIN)/%$(EXE): $(GO_SOURCES) go.mod go.sum
	@mkdir -p $(BIN)
	$(GO) build -o $@ ./examples/$*

# Run the chat example with examples/chat as its working directory, so its .env,
# .config and .data stay under examples/chat instead of the launch directory.
chat: $(BIN)/chat$(EXE)
	$(BIN)/chat$(EXE) -C examples/chat

# Run the embedded-A2A example: it walks through multi-turn context and an
# input-required round trip, then keeps serving until Ctrl+C.
a2a-embedded: $(BIN)/a2a-embedded$(EXE)
	$(BIN)/a2a-embedded$(EXE)

# Format all Go sources in place.
fmt:
	$(GOFMT) -w .

# Fail if any Go source is not gofmt-clean.
fmt-check:
	@files="$$($(GOFMT) -l .)"; \
	if [ -n "$$files" ]; then \
		echo "unformatted files:"; echo "$$files"; exit 1; \
	fi

vet:
	$(GO) vet ./...

# Run all Go tests.
test:
	$(GO) test ./...

# Always-available lint: formatting check plus go vet.
lint: fmt-check vet

# Verify the internal/ boundary: no exported pkg/... symbol may reference an
# internal/ type (AST check), and the external probe module (public imports
# only) must build.
check-boundary:
	$(GO) run testdata/boundarycheck.go
	cp go.sum testdata/external/go.sum
	cd testdata/external && $(GO) build -o /dev/null ./...
	@echo "internal boundary OK"

# Strict lint via golangci-lint. Rules are pinned by .golangci.yml and the tool
# version is pinned above, so `make golangci` is reproducible.
golangci:
	$(GOLANGCI_LINT) run ./...

# Build the commands for the current platform and package a release archive
# into $(DIST)/: a tar.gz plus a sha256 checksum file, named
# go-agent-sdk-$(VERSION). Fails unless HEAD is exactly tagged and clean, so a
# release archive can never carry a dev version stamp.
release:
	@test -n "$(TAG_VERSION)" || { \
		echo "release: HEAD must be tagged with a version tag (e.g. v1.2.3)" >&2; exit 1; }
	@test -z "$(GIT_DIRTY)" || { \
		echo "release: working tree is dirty, commit or stash first:" >&2; \
		git status --short >&2; exit 1; }
	@case "$(VERSION)" in \
		'') echo "release: VERSION is empty" >&2; exit 1;; \
		*[!0-9A-Za-z._+-]*) \
			echo "release: VERSION may only contain [0-9A-Za-z._+-]" >&2; exit 1;; \
	esac
	rm -rf "$(DIST)"
	mkdir -p "$(DIST)/go-agent-sdk-$(VERSION)"
	for cmd in $(CMDS); do \
		$(GO) build -trimpath -ldflags "-s -w $(VERSION_FLAG)" \
			-o "$(DIST)/go-agent-sdk-$(VERSION)/$$cmd$(EXE)" ./cmd/$$cmd || exit 1; \
	done
	tar -czf "$(DIST)/go-agent-sdk-$(VERSION).tar.gz" -C "$(DIST)" "go-agent-sdk-$(VERSION)"
	rm -rf "$(DIST)/go-agent-sdk-$(VERSION)"
	cd "$(DIST)" && sha256sum * > sha256sums.txt
	@echo "release archive in $(DIST)/ ($(VERSION))"

clean:
	rm -rf $(BIN) $(DIST)

# Print the version and kind that the next build would stamp, e.g.
# "dev-abc1234-dirty (dev)" or "1.2.3 (release)".
version:
	@echo "$(VERSION) ($(BUILD_KIND))"
