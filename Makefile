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
# Version stamp used for release archive names; falls back to "dev" outside git.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

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

.PHONY: all examples cmds chat a2a-embedded agentd fmt fmt-check vet test lint golangci check-boundary release clean

all: examples cmds

examples: $(BINARIES)

cmds: $(CMD_BINARIES)

# Build the multi-transport server command.
$(BIN)/agentd$(EXE): $(GO_SOURCES) go.mod go.sum
	@mkdir -p $(BIN)
	$(GO) build -o $@ ./cmd/agentd

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
# go-agent-sdk-$(VERSION).
release:
	rm -rf $(DIST)
	mkdir -p $(DIST)/go-agent-sdk-$(VERSION)
	for cmd in $(CMDS); do \
		$(GO) build -trimpath -ldflags "-s -w" \
			-o $(DIST)/go-agent-sdk-$(VERSION)/$$cmd$(EXE) ./cmd/$$cmd; \
	done
	tar -czf $(DIST)/go-agent-sdk-$(VERSION).tar.gz -C $(DIST) go-agent-sdk-$(VERSION)
	rm -rf $(DIST)/go-agent-sdk-$(VERSION)
	cd $(DIST) && sha256sum * > sha256sums.txt
	@echo "release archive in $(DIST)/ ($(VERSION))"

clean:
	rm -rf $(BIN) $(DIST)
