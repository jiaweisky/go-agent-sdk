GO ?= go
GOFMT ?= gofmt
# golangci-lint is pinned and run through `go run` so it is compiled with the
# project's Go toolchain (a prebuilt binary built with an older Go refuses a
# newer target). Override with GOLANGCI_LINT=golangci-lint to use a local binary.
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOOS ?= $(shell $(GO) env GOOS)
BIN ?= bin

ifeq ($(GOOS),windows)
EXE := .exe
else
EXE :=
endif

EXAMPLES := chat tool notesmcp a2aserver httpclient
BINARIES := $(addprefix $(BIN)/,$(addsuffix $(EXE),$(EXAMPLES)))
CMDS := a2aserve agentserve
CMD_BINARIES := $(addprefix $(BIN)/,$(addsuffix $(EXE),$(CMDS)))
GO_SOURCES := $(shell find . -name '*.go' -not -path './bin/*')

.PHONY: all examples cmds chat a2aserver a2aserve agentserve fmt fmt-check vet test lint golangci check-boundary clean

all: examples cmds

examples: $(BINARIES)

cmds: $(CMD_BINARIES)

# Build the A2A server command.
$(BIN)/a2aserve$(EXE): $(GO_SOURCES) go.mod go.sum
	@mkdir -p $(BIN)
	$(GO) build -o $@ ./cmd/a2aserve

# Run the A2A server. Uses the git-ignored local config; override with
# CONFIG=path (see cmd/a2aserve/a2aserve.example.yaml). Serves until Ctrl+C.
CONFIG ?= cmd/a2aserve/a2aserve.yaml

a2aserve: $(BIN)/a2aserve$(EXE)
	$(BIN)/a2aserve$(EXE) --config $(CONFIG)

# Build the HTTP API server command.
$(BIN)/agentserve$(EXE): $(GO_SOURCES) go.mod go.sum
	@mkdir -p $(BIN)
	$(GO) build -o $@ ./cmd/agentserve

# Run the HTTP API server (see cmd/agentserve/agentserve.example.yaml).
AGENTSERVE_CONFIG ?= cmd/agentserve/agentserve.yaml

agentserve: $(BIN)/agentserve$(EXE)
	$(BIN)/agentserve$(EXE) --config $(AGENTSERVE_CONFIG)

$(BIN)/%$(EXE): $(GO_SOURCES) go.mod go.sum
	@mkdir -p $(BIN)
	$(GO) build -o $@ ./examples/$*

# Run the chat example with examples/chat as its working directory, so its .env,
# .config and .data stay under examples/chat instead of the launch directory.
chat: $(BIN)/chat$(EXE)
	$(BIN)/chat$(EXE) -C examples/chat

# Run the A2A server example: it walks through multi-turn context and an
# input-required round trip, then keeps serving until Ctrl+C.
a2aserver: $(BIN)/a2aserver$(EXE)
	$(BIN)/a2aserver$(EXE)

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

clean:
	rm -rf $(BIN)
