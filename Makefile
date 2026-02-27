# Imnive Design // Inco Build Protocol

.PHONY: gen build test run clean install

INCO_BIN      := bin/inco

# Require inco: go install github.com/incogno-design/inco/cmd/inco@latest
INCO := $(shell command -v inco 2>/dev/null)

# --- Generate overlay from contract directives ---
gen:
ifndef INCO
	$(error "inco not found in PATH. Install with: go install github.com/incogno-design/inco/cmd/inco@latest")
endif
	@inco gen .

# --- Build with overlay (self-hosted) ---
build:
ifndef INCO
	$(error "inco not found in PATH. Install with: go install github.com/incogno-design/inco/cmd/inco@latest")
endif
	@inco gen .
	@inco build -o $(INCO_BIN) ./cmd/inco
	@echo "inco: self-hosted binary ready at $(INCO_BIN)"

# --- Test with overlay ---
test:
ifndef INCO
	$(error "inco not found in PATH. Install with: go install github.com/incogno-design/inco/cmd/inco@latest")
endif
	@inco test ./...

# --- Run with overlay ---
run:
ifndef INCO
	$(error "inco not found in PATH. Install with: go install github.com/incogno-design/inco/cmd/inco@latest")
endif
	@inco run .

# --- Clean cache and binaries ---
clean:
	@rm -rf .inco_cache bin/

# --- Install: build self-hosted binary to GOPATH/bin ---
install:
ifndef INCO
	$(error "inco not found in PATH. Install with: go install github.com/incogno-design/inco/cmd/inco@latest")
endif
	@inco gen .
	@go build -overlay .inco_cache/overlay.json -o $(GOPATH)/bin/inco ./cmd/inco 2>/dev/null || \
		go install ./cmd/inco
	@echo "inco: installed to GOPATH/bin"
