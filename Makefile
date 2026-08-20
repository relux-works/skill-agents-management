.PHONY: all build test vet install clean

ROOT_DIR := $(shell pwd)
CLI_DIR  := $(ROOT_DIR)/tools/agents-management
BIN_DIR  := $(HOME)/.local/bin

# Output path of `make build`. Overridable so a caller (a test, a release job)
# can build into a scratch directory without touching the checkout.
BIN ?= $(CLI_DIR)/agents-management

# Version metadata from git. Overridable so a caller can build with known
# values and assert they reached the binary.
VERSION    ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

CLI_PKG := github.com/relux-works/skill-agents-management/tools/agents-management/cmd

LDFLAGS := -X $(CLI_PKG).Version=$(VERSION) \
           -X $(CLI_PKG).Commit=$(COMMIT) \
           -X $(CLI_PKG).BuildDate=$(BUILD_DATE)

# -mod=mod is explicit everywhere: Go auto-enables -mod=vendor the moment a
# vendor/modules.txt exists, and a stale vendor tree then fails every build.
# The extraction source dropped vendoring for exactly that reason
# (skill-project-management TASK-260819-3vr8j3).
GOFLAGS_MOD := -mod=mod

all: build

build:
	@go build $(GOFLAGS_MOD) -ldflags '$(LDFLAGS)' -o $(BIN) ./tools/agents-management

test:
	@go test $(GOFLAGS_MOD) ./... -count=1

vet:
	@go vet $(GOFLAGS_MOD) ./...

install: build
	@mkdir -p $(BIN_DIR)
	@rm -f $(BIN_DIR)/agents-management
	@cp $(BIN) $(BIN_DIR)/agents-management
	@chmod +x $(BIN_DIR)/agents-management
	@echo "Installed agents-management -> $(BIN_DIR)/agents-management"

clean:
	@rm -f $(CLI_DIR)/agents-management
	@echo "Cleaned."
