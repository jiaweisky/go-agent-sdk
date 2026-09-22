GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
BIN ?= bin

ifeq ($(GOOS),windows)
EXE := .exe
else
EXE :=
endif

EXAMPLES := chat tool
BINARIES := $(addprefix $(BIN)/,$(addsuffix $(EXE),$(EXAMPLES)))

.PHONY: all examples clean

all: examples

examples: $(BINARIES)

$(BIN)/%$(EXE):
	@mkdir -p $(BIN)
	$(GO) build -o $@ ./examples/$*

clean:
	rm -rf $(BIN)
