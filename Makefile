GO ?= go
GOFMT ?= gofmt
GOLANGCI_LINT ?= golangci-lint
GOOS ?= $(shell $(GO) env GOOS)
BIN ?= bin

ifeq ($(GOOS),windows)
EXE := .exe
else
EXE :=
endif

EXAMPLES := chat tool notesmcp a2aserver
BINARIES := $(addprefix $(BIN)/,$(addsuffix $(EXE),$(EXAMPLES)))
GO_SOURCES := $(shell find . -name '*.go' -not -path './bin/*')

.PHONY: all examples chat a2aserver fmt fmt-check vet test lint golangci clean

all: examples

examples: $(BINARIES)

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

# Strict lint via golangci-lint (requires a version compatible with the
# project's Go toolchain).
golangci:
	$(GOLANGCI_LINT) run ./...

clean:
	rm -rf $(BIN)
