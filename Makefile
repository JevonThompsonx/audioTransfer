# Makefile for audioTransfer (Go implementation)
#
# Common targets:
#   make build   - compile the audiotransfer binary into ./bin
#   make test    - run all Go tests
#   make lint    - gofmt + go vet checks (CI-equivalent)
#   make tidy    - run go mod tidy
#   make vuln    - run govulncheck (advisory vulnerability scan)
#   make clean   - remove build artifacts

GO          ?= go
BINARY      := audiotransfer
BIN_DIR     := bin
CMD_PKG     := ./cmd/audiotransfer
GOFLAGS     ?=

.PHONY: all build test lint tidy vuln clean

all: build

build:
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD_PKG)

test:
	$(GO) test ./...

lint:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-compliant:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

vuln:
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	$(shell $(GO) env GOPATH)/bin/govulncheck ./...

clean:
	rm -rf $(BIN_DIR)
