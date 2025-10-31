# Talos CSR Signer - Makefile
# Build, test, and container image automation
#
# NOTE: This Makefile is for development and building container images.
#       For deployment instructions, see:
#       - docs/sidecar-deployment.md (Kamaji)
#       - docs/standalone-deployment.md (kubeadm)

# Configuration
IMAGE_REGISTRY ?= docker.io
IMAGE_REPO ?= bsctl/talos-csr-signer
IMAGE_TAG ?= latest
IMAGE_NAME = $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(IMAGE_TAG)

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GOCLEAN = $(GOCMD) clean
GOTEST = $(GOCMD) test
GOGET = $(GOCMD) get
GOMOD = $(GOCMD) mod

# Binary name
BINARY_NAME = talos-csr-signer
BINARY_PATH = bin/$(BINARY_NAME)

# Protobuf
PROTO_DIR = proto
PROTO_FILES = $(PROTO_DIR)/security.proto
PROTO_GEN = $(PROTO_DIR)/security.pb.go $(PROTO_DIR)/security_grpc.pb.go

# Colors for output
COLOR_RESET = \033[0m
COLOR_BOLD = \033[1m
COLOR_GREEN = \033[32m
COLOR_YELLOW = \033[33m
COLOR_BLUE = \033[34m

.PHONY: all proto build docker-build docker-push docker-run clean help test lint deps

# Default target - show help
.DEFAULT_GOAL := help

all: proto build docker-build ## Build all components (proto + binary + docker image)

##@ General

help: ## Display this help message
	@echo "$(COLOR_BOLD)Talos CSR Signer - Available Targets$(COLOR_RESET)"
	@echo ""
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make $(COLOR_BLUE)<target>$(COLOR_RESET)\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  $(COLOR_BLUE)%-20s$(COLOR_RESET) %s\n", $$1, $$2 } /^##@/ { printf "\n$(COLOR_BOLD)%s$(COLOR_RESET)\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

proto: ## Generate protobuf code
	@echo "$(COLOR_GREEN)Generating protobuf code...$(COLOR_RESET)"
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)
	@echo "$(COLOR_GREEN)✓ Protobuf code generated$(COLOR_RESET)"

deps: ## Download Go module dependencies
	@echo "$(COLOR_GREEN)Downloading dependencies...$(COLOR_RESET)"
	@$(GOMOD) download
	@$(GOMOD) tidy
	@echo "$(COLOR_GREEN)✓ Dependencies downloaded$(COLOR_RESET)"

build: proto ## Build the binary locally
	@echo "$(COLOR_GREEN)Building binary...$(COLOR_RESET)"
	@mkdir -p bin
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="-w -s" -o $(BINARY_PATH) ./cmd
	@echo "$(COLOR_GREEN)✓ Binary built: $(BINARY_PATH)$(COLOR_RESET)"

test: ## Run unit tests
	@echo "$(COLOR_GREEN)Running tests...$(COLOR_RESET)"
	@$(GOTEST) -v -race -coverprofile=coverage.out ./...
	@echo "$(COLOR_GREEN)✓ Tests passed$(COLOR_RESET)"

lint: ## Run golangci-lint (requires golangci-lint installed)
	@echo "$(COLOR_GREEN)Running linter...$(COLOR_RESET)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
		echo "$(COLOR_GREEN)✓ Lint passed$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)⚠ golangci-lint not installed, skipping$(COLOR_RESET)"; \
	fi

clean: ## Clean generated files and binaries
	@echo "$(COLOR_GREEN)Cleaning...$(COLOR_RESET)"
	@rm -f $(PROTO_GEN)
	@rm -rf bin/
	@rm -f coverage.out
	@$(GOCLEAN)
	@echo "$(COLOR_GREEN)✓ Cleaned$(COLOR_RESET)"

##@ Docker

docker-build: ## Build Docker image
	@echo "$(COLOR_GREEN)Building Docker image: $(IMAGE_NAME)$(COLOR_RESET)"
	@docker build -t $(IMAGE_NAME) .
	@echo "$(COLOR_GREEN)✓ Docker image built$(COLOR_RESET)"

docker-push: docker-build ## Build and push Docker image to registry
	@echo "$(COLOR_GREEN)Pushing Docker image: $(IMAGE_NAME)$(COLOR_RESET)"
	@docker push $(IMAGE_NAME)
	@echo "$(COLOR_GREEN)✓ Docker image pushed$(COLOR_RESET)"

docker-run: docker-build ## Run Docker container locally (for testing)
	@echo "$(COLOR_GREEN)Running Docker container...$(COLOR_RESET)"
	@docker run --rm -it \
		-p 50001:50001 \
		-e CA_CERT_PATH=/tmp/ca.crt \
		-e CA_KEY_PATH=/tmp/ca.key \
		-e TALOS_TOKEN=test-token \
		$(IMAGE_NAME)

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
