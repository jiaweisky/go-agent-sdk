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

EXAMPLES := chat tool
BINARIES := $(addprefix $(BIN)/,$(addsuffix $(EXE),$(EXAMPLES)))

.PHONY: all examples fmt fmt-check vet lint golangci clean

all: examples

examples: $(BINARIES)

$(BIN)/%$(EXE):
	@mkdir -p $(BIN)
	$(GO) build -o $@ ./examples/$*

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

# Always-available lint: formatting check plus go vet.
lint: fmt-check vet

# Strict lint via golangci-lint (requires a version compatible with the
# project's Go toolchain).
golangci:
	$(GOLANGCI_LINT) run ./...

clean:
	rm -rf $(BIN)
