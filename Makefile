# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GO=go
GO_MAJOR_VERSION = $(shell $(GO) version | cut -c 14- | cut -d' ' -f1 | cut -d'.' -f1)
GO_MINOR_VERSION = $(shell $(GO) version | cut -c 14- | cut -d' ' -f1 | cut -d'.' -f2)

GO_VERSION = $(GO_MAJOR_VERSION).$(GO_MINOR_VERSION)

PACKAGE_NAME = $(shell awk '/^module / {print $$2}' go.mod)

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# CGO is required only on macOS (IOKit via hidapi). Linux and Windows builds
# are pure Go and produce fully static binaries.
GOOS ?= $(shell go env GOOS)
ifeq ($(GOOS),darwin)
CGO_ENABLED ?= 1
else
CGO_ENABLED ?= 0
endif
export CGO_ENABLED

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: build
build: fmt vet ## Build the openvlm CLI binary.
	GOCACHE=$(HOME)/.gocache go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/openvlm .

.PHONY: cgobuild
cgobuild: fmt vet ## Build the openvlm CLI binary with CGO_ENABLED=1.
	CGO_ENABLED=1 GOCACHE=$(HOME)/.gocache go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/openvlm .

.PHONY: run
run: fmt vet ## Run the CLI from your host (forwards args via ARGS=...).
	go run . $(ARGS)

.PHONY: test
test: fmt vet ## Run tests.
	go test ./... -coverprofile=coverage.out -covermode=atomic

.PHONY: test-race
test-race: fmt vet ## Run tests with race detector. Always uses CGO_ENABLED=1.
	CGO_ENABLED=1 go test -race -timeout 120s ./... -coverprofile=coverage.out -covermode=atomic

.PHONY: lint-go
lint-go: ## Install golangci-lint if not present, then run it.
	$(GOBIN)/golangci-lint run --fix --timeout 5m

.PHONY: lint
lint: lint-go ## Run linters.

.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf bin/

