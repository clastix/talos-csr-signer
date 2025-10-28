# Talos CSR Signer - Makefile
# Comprehensive build, test, and deployment automation

# Configuration
IMAGE_REGISTRY ?= docker.io
IMAGE_REPO ?= bsctl/talos-csr-signer
IMAGE_TAG ?= latest
IMAGE_NAME = $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(IMAGE_TAG)

NAMESPACE ?= default
KUBECONFIG ?= $(HOME)/.kube/config

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

.PHONY: all proto build docker-build docker-push deploy undeploy verify-secret clean help test lint deps

# Default target - show help
.DEFAULT_GOAL := help

all: proto build docker-build

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

##@ Kubernetes Deployment

verify-secret: ## Verify the secret configuration
	@echo "$(COLOR_GREEN)Verifying secret configuration...$(COLOR_RESET)"
	@if ! grep -q "LS0tLS1CRUdJTi" deploy/01-secret.yaml; then \
		echo "$(COLOR_YELLOW)⚠ Warning: deploy/01-secret.yaml may still contain placeholder values$(COLOR_RESET)"; \
		echo "$(COLOR_YELLOW)  Please update ca.crt, ca.key, and token before deploying$(COLOR_RESET)"; \
		exit 1; \
	fi
	@echo "$(COLOR_GREEN)✓ Secret appears to be configured$(COLOR_RESET)"

deploy: docker-push verify-secret ## Build, push image, and deploy to Kubernetes
	@echo "$(COLOR_GREEN)Deploying to Kubernetes namespace: $(NAMESPACE)$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	@kubectl --kubeconfig=$(KUBECONFIG) apply -f deploy/01-secret.yaml
	@kubectl --kubeconfig=$(KUBECONFIG) apply -f deploy/02-deployment.yaml
	@kubectl --kubeconfig=$(KUBECONFIG) apply -f deploy/03-service.yaml
	@echo "$(COLOR_GREEN)✓ Deployed to Kubernetes$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BOLD)Waiting for deployment to be ready...$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) rollout status deployment/talos-csr-signer --timeout=120s
	@echo ""
	@$(MAKE) status

deploy-local: verify-secret ## Deploy to Kubernetes without pushing (uses local image)
	@echo "$(COLOR_YELLOW)⚠ Deploying with local image (not pushed to registry)$(COLOR_RESET)"
	@echo "$(COLOR_YELLOW)  Make sure your cluster can access locally built images$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	@kubectl --kubeconfig=$(KUBECONFIG) apply -f deploy/01-secret.yaml
	@kubectl --kubeconfig=$(KUBECONFIG) apply -f deploy/02-deployment.yaml
	@kubectl --kubeconfig=$(KUBECONFIG) apply -f deploy/03-service.yaml
	@echo "$(COLOR_GREEN)✓ Deployed to Kubernetes$(COLOR_RESET)"

deploy-loadbalancer: ## Deploy LoadBalancer service (shares IP with Milano)
	@echo "$(COLOR_GREEN)Deploying LoadBalancer service...$(COLOR_RESET)"
	@echo "$(COLOR_YELLOW)⚠ This requires MetalLB IP sharing to be enabled$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BOLD)Step 1: Patch Milano service to enable IP sharing$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) annotate svc milano -n default \
		metallb.universe.tf/allow-shared-ip=milano-shared --overwrite
	@echo "$(COLOR_GREEN)✓ Milano service annotated$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BOLD)Step 2: Deploy CSR Signer LoadBalancer$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) apply -f deploy/04-loadbalancer.yaml
	@echo "$(COLOR_GREEN)✓ LoadBalancer service deployed$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BOLD)Checking LoadBalancer IP allocation...$(COLOR_RESET)"
	@sleep 5
	@LB_IP=$$(kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) get svc talos-csr-signer-lb -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "pending"); \
	echo "LoadBalancer IP: $$LB_IP"; \
	if [ "$$LB_IP" = "10.10.10.101" ]; then \
		echo "$(COLOR_GREEN)✓ IP sharing successful! CSR Signer available at 10.10.10.101:50001$(COLOR_RESET)"; \
	elif [ "$$LB_IP" = "pending" ]; then \
		echo "$(COLOR_YELLOW)⚠ IP allocation pending, check with: kubectl -n $(NAMESPACE) get svc talos-csr-signer-lb$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)⚠ Got different IP: $$LB_IP (expected 10.10.10.101)$(COLOR_RESET)"; \
	fi

undeploy-loadbalancer: ## Remove LoadBalancer service
	@echo "$(COLOR_GREEN)Removing LoadBalancer service...$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) delete -f deploy/04-loadbalancer.yaml --ignore-not-found=true
	@echo "$(COLOR_GREEN)✓ LoadBalancer service removed$(COLOR_RESET)"

undeploy: ## Remove deployment from Kubernetes
	@echo "$(COLOR_GREEN)Removing deployment from namespace: $(NAMESPACE)$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) delete -f deploy/04-loadbalancer.yaml --ignore-not-found=true
	@kubectl --kubeconfig=$(KUBECONFIG) delete -f deploy/03-service.yaml --ignore-not-found=true
	@kubectl --kubeconfig=$(KUBECONFIG) delete -f deploy/02-deployment.yaml --ignore-not-found=true
	@kubectl --kubeconfig=$(KUBECONFIG) delete -f deploy/01-secret.yaml --ignore-not-found=true
	@echo "$(COLOR_GREEN)✓ Deployment removed$(COLOR_RESET)"

restart: ## Restart the deployment (to pick up new secret values)
	@echo "$(COLOR_GREEN)Restarting deployment...$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) rollout restart deployment/talos-csr-signer
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) rollout status deployment/talos-csr-signer --timeout=120s
	@echo "$(COLOR_GREEN)✓ Deployment restarted$(COLOR_RESET)"

##@ Monitoring & Debugging

status: ## Show deployment status
	@echo "$(COLOR_BOLD)Deployment Status$(COLOR_RESET)"
	@echo "$(COLOR_BLUE)Pods:$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) get pods -l app=talos-csr-signer
	@echo ""
	@echo "$(COLOR_BLUE)Services:$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) get svc -l app=talos-csr-signer
	@echo ""
	@NODE_PORT=$$(kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) get svc talos-csr-signer-external -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || echo "N/A"); \
	NODE_IP=$$(kubectl --kubeconfig=$(KUBECONFIG) get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || echo "N/A"); \
	echo "$(COLOR_BLUE)External Access:$(COLOR_RESET)"; \
	echo "  NodePort: $$NODE_IP:$$NODE_PORT"; \
	echo "  Configure workers to forward: <control-plane-ip>:50001 -> $$NODE_IP:$$NODE_PORT"

logs: ## Show logs from CSR signer pods
	@echo "$(COLOR_GREEN)Fetching logs...$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) logs -l app=talos-csr-signer --tail=50

logs-follow: ## Follow logs from CSR signer pods
	@echo "$(COLOR_GREEN)Following logs (Ctrl+C to stop)...$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) logs -l app=talos-csr-signer -f

describe: ## Describe deployment and pods
	@echo "$(COLOR_BOLD)Deployment:$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) describe deployment talos-csr-signer
	@echo ""
	@echo "$(COLOR_BOLD)Pods:$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) describe pods -l app=talos-csr-signer

shell: ## Open shell in running pod
	@POD=$$(kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) get pod -l app=talos-csr-signer -o jsonpath='{.items[0].metadata.name}'); \
	echo "$(COLOR_GREEN)Opening shell in pod: $$POD$(COLOR_RESET)"; \
	kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) exec -it $$POD -- /bin/sh

##@ Testing & Validation

test-connection: ## Test connection to CSR signer (requires nc/netcat)
	@NODE_PORT=$$(kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) get svc talos-csr-signer-external -o jsonpath='{.spec.ports[0].nodePort}'); \
	NODE_IP=$$(kubectl --kubeconfig=$(KUBECONFIG) get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'); \
	echo "$(COLOR_GREEN)Testing connection to $$NODE_IP:$$NODE_PORT...$(COLOR_RESET)"; \
	if command -v nc >/dev/null 2>&1; then \
		nc -zv $$NODE_IP $$NODE_PORT; \
	else \
		echo "$(COLOR_YELLOW)⚠ netcat (nc) not installed, cannot test connection$(COLOR_RESET)"; \
	fi

verify-deployment: ## Verify deployment is healthy
	@echo "$(COLOR_GREEN)Verifying deployment health...$(COLOR_RESET)"
	@kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) wait --for=condition=available --timeout=60s deployment/talos-csr-signer
	@READY=$$(kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) get deployment talos-csr-signer -o jsonpath='{.status.readyReplicas}'); \
	DESIRED=$$(kubectl --kubeconfig=$(KUBECONFIG) -n $(NAMESPACE) get deployment talos-csr-signer -o jsonpath='{.spec.replicas}'); \
	if [ "$$READY" = "$$DESIRED" ]; then \
		echo "$(COLOR_GREEN)✓ Deployment is healthy: $$READY/$$DESIRED replicas ready$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)⚠ Warning: Only $$READY/$$DESIRED replicas ready$(COLOR_RESET)"; \
		exit 1; \
	fi

##@ Complete Workflows

release: clean proto build docker-build docker-push ## Complete release workflow: clean, build, and push
	@echo "$(COLOR_GREEN)✓ Release complete: $(IMAGE_NAME)$(COLOR_RESET)"

install: release deploy ## Build, push, and deploy in one command
	@echo "$(COLOR_GREEN)✓ Installation complete$(COLOR_RESET)"

install-with-lb: release deploy deploy-loadbalancer ## Complete install with LoadBalancer (recommended for Kamaji)
	@echo "$(COLOR_GREEN)✓ Installation with LoadBalancer complete$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BOLD)Workers can now connect to:$(COLOR_RESET)"
	@echo "  Kubernetes API: 10.10.10.101:6443"
	@echo "  Talos CSR Signer: 10.10.10.101:50001"

reinstall: undeploy install ## Undeploy and reinstall
	@echo "$(COLOR_GREEN)✓ Reinstallation complete$(COLOR_RESET)"

##@ Information

version: ## Show version information
	@echo "$(COLOR_BOLD)Talos CSR Signer$(COLOR_RESET)"
	@echo "Image: $(IMAGE_NAME)"
	@echo "Namespace: $(NAMESPACE)"
	@echo "Kubeconfig: $(KUBECONFIG)"

env: ## Show environment variables
	@echo "$(COLOR_BOLD)Environment:$(COLOR_RESET)"
	@echo "IMAGE_REGISTRY = $(IMAGE_REGISTRY)"
	@echo "IMAGE_REPO     = $(IMAGE_REPO)"
	@echo "IMAGE_TAG      = $(IMAGE_TAG)"
	@echo "IMAGE_NAME     = $(IMAGE_NAME)"
	@echo "NAMESPACE      = $(NAMESPACE)"
	@echo "KUBECONFIG     = $(KUBECONFIG)"
