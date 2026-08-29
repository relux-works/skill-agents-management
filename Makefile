.PHONY: all build test vet regress contract-mutants install clean

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

# The landing gate's regression net: one fast, cross-cutting check per class of
# failure this repository has already paid for, each with the negative that
# shows it bites. It is deliberately a SEPARATE target from `test` rather than
# a subset of it — `test` is the deep per-package acceptance and takes as long
# as that deserves, while this one sits in front of every landing and has to
# stay cheap enough that nobody is tempted to skip it. See internal/regress.
#
# env -u TASK_BOARD_DIR: an inherited board directory reaches the test process
# and is not this module's to read (the extraction source's BUG-260823-1tkumz).
regress:
	@env -u TASK_BOARD_DIR go test $(GOFLAGS_MOD) ./internal/regress/... -count=1

contract-mutants:
	@python3 .scripts/verify-inference-engine-contract.py

install: build
	@mkdir -p $(BIN_DIR)
	@rm -f $(BIN_DIR)/agents-management
	@cp $(BIN) $(BIN_DIR)/agents-management
	@chmod +x $(BIN_DIR)/agents-management
	@echo "Installed agents-management -> $(BIN_DIR)/agents-management"

clean:
	@rm -f $(CLI_DIR)/agents-management
	@echo "Cleaned."
