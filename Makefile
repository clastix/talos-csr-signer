# Kamaji addon for Talos worker nodes - Makefile
# Build, test, and container image automation
#
# NOTE: This Makefile is for development and building container images.
#       For deployment instructions, see:
#       - docs/sidecar-deployment.md (Kamaji)
#       - docs/standalone-deployment.md (kubeadm)

GIT_HEAD_COMMIT ?= $$(git rev-parse --short HEAD)
VERSION ?= $(or $(shell git describe --abbrev=0 --tags 2>/dev/null),$(GIT_HEAD_COMMIT))

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
KO ?= $(LOCALBIN)/ko
PROTOC ?= $(LOCALBIN)/protoc
PROTOC_GO ?= $(LOCALBIN)/protoc-gen-go-grpc

# OCI variables
OCI_REGISTRY ?= ghcr.io
OCI_REPO ?= clastix/talos-csr-signer
OCI_TAG ?= latest
OCI_NAME = $(OCI_REGISTRY)/$(OCI_REPO):$(OCI_TAG)

# Binary name
BINARY_NAME = talos-csr-signer
BINARY_PATH = bin/$(BINARY_NAME)

# Protobuf
PROTO_DIR = pkg/proto
PROTO_FILES = $(PROTO_DIR)/security.proto
PROTO_GEN = $(PROTO_DIR)/security.pb.go $(PROTO_DIR)/security_grpc.pb.go
PROTOC_VERSION := 28.2
PROTOC_GO_VERSION := 1.5.1

# Default target - show help
.DEFAULT_GOAL := help

all: build

##@ Binary

.PHONY: ko
ko: $(KO) ## Download ko locally if necessary.
$(KO): $(LOCALBIN)
	test -s $(LOCALBIN)/ko || GOBIN=$(LOCALBIN) CGO_ENABLED=0 go install -ldflags="-s -w" github.com/google/ko@v0.18.0

.PHONY: protoc_go
protoc_go: $(PROTOC_GO) ## Download protoc-gen-go-grpc locally if necessary.
$(PROTOC_GO): $(LOCALBIN)
	test -s $(LOCALBIN)/protoc-gen-go-grpc || GOBIN=$(LOCALBIN) CGO_ENABLED=0 go install -ldflags="-s -w" google.golang.org/grpc/cmd/protoc-gen-go-grpc@v$(PROTOC_GO_VERSION)

.PHONY: protoc
protoc: $(PROTOC) ## Download protoc locally if necessary.
$(PROTOC): $(LOCALBIN)
	test -s $(PROTOC) || (rm -f $(PROTOC) && \
	curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v$(PROTOC_VERSION)/protoc-$(PROTOC_VERSION)-linux-x86_64.zip && \
	unzip -j protoc-*.zip bin/protoc -d bin && \
	rm protoc-*.zip)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	test -s $(LOCALBIN)/golangci-lint || GOBIN=$(LOCALBIN) CGO_ENABLED=0 go install -ldflags="-s -w" github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.0.2

##@ General

help: ## Display this help message
	@echo "Kamaji Talos Addon - Available Targets"
	@echo ""
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make $(COLOR_BLUE)<target>\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  $(COLOR_BLUE)%-20s %s\n", $$1, $$2 } /^##@/ { printf "\n%s\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

proto: protoc protoc_go ## Generate protobuf code
	PATH=$$PATH:$(LOCALBIN) $(PROTOC) --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)

deps: ## Download Go module dependencies
	go mod download
	go mod tidy

build: pkg/proto ## Build the binary locally
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o $(BINARY_PATH) .

test: ## Run unit tests
	go test -v -race -coverprofile=coverage.out ./...

lint: golangci-lint ## Run golangci-lint (requires golangci-lint installed)
	$(GOLANGCI_LINT) run -c=.golangci.yaml ./...

clean: ## Clean generated files and binaries
	@rm -f $(PROTO_GEN)
	@rm -rf bin/
	@rm -f coverage.out
	@go clean

##@ OCI

KO_LOCAL ?= true
KO_PUSH ?= false

oci-build: $(KO)  ## Build OCI artefact
	KOCACHE=/tmp/ko-cache KO_DOCKER_REPO=${OCI_REGISTRY}/${OCI_REPO} \
	$(KO) build . --bare --tags=$(VERSION) --local=$(KO_LOCAL) --push=$(KO_PUSH)

oci-run: oci-build ## Run OCI container locally (for testing)
	@docker run --rm -it \
		-p 50001:50001 \
		-e CA_CERT_PATH=/tmp/ca.crt \
		-e CA_KEY_PATH=/tmp/ca.key \
		-e TALOS_TOKEN=test-token \
		$(OCI_NAME)

##@ Information

version: ## Show version information
	@echo "$(COLOR_BOLD)Talos CSR Signer$(COLOR_RESET)"
	@echo "Image: $(IMAGE_NAME)"

env: ## Show environment variables
	@echo "$(COLOR_BOLD)Environment:$(COLOR_RESET)"
	@echo "IMAGE_REGISTRY = $(IMAGE_REGISTRY)"
	@echo "IMAGE_REPO     = $(IMAGE_REPO)"
	@echo "IMAGE_TAG      = $(IMAGE_TAG)"
	@echo "IMAGE_NAME     = $(IMAGE_NAME)"
