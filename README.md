# Talos CSR Signer Service

A lightweight gRPC service that implements the Talos `trustd` Security Service protocol, enabling `talosctl` access for Talos worker nodes joining kubeadm-based or Kamaji control planes.

## The Problem

When Talos workers join kubeadm/Kamaji control planes:
- ✅ **Kubernetes functionality works** - kubelet, pods, services, networking
- ❌ **Talos API fails to start** - no `talosctl` access (logs, upgrades, console)

**Root Cause:** Talos workers need a `trustd` service to sign Talos API certificates. Kubeadm/Kamaji control planes don't provide this service.

## The Solution

This service acts as `trustd`, implementing the Talos Security Service gRPC protocol to sign certificate requests from workers.

**Result:** Full `talosctl` functionality while using kubeadm/Kamaji control planes.

## Features

- ✅ **Lightweight** - ~20MB container, distroless base image
- ✅ **Standard Protocol** - Implements Talos SecurityService gRPC spec
- ✅ **Secure** - Token authentication, TLS encryption, read-only filesystem
- ✅ **Production-Ready** - High availability, resource limits, health checks
- ✅ **Easy Deployment** - Simple Makefile commands, pre-built manifests

## Architecture

```
┌────────────────────────────────────────────────────┐
│ Control Plane IP: 10.10.10.101                     │
│                                                    │
│  Port 6443  → kube-apiserver (Kubernetes PKI)      │
│  Port 8132  → konnectivity server                  │
│  Port 50001 → talos-csr-signer (Talos Machine PKI) │
│                                                    │
└─────────────┬────────────────┬─────────────────────┘
              │                │
      ┌───────▼────────────────▼──────┐
      │     Talos Worker              │
      │                               │
      │ kubelet     → Port 6443       │
      │ konn. agent → Port 8132       │
      │ apid        → Port 50001      │
      └───────────────────────────────┘
```

**How it works:**
1. CSR signer shares LoadBalancer IP with control plane (different port)
2. Talos worker discovers CSR signer via Talos discovery protocol
3. Worker sends certificate request to CSR signer (port 50001)
4. CSR signer validates token and signs certificate with Machine CA
5. Worker receives certificate, starts apid service
6. `talosctl` can now manage the worker

**For detailed architecture explanation:** See [docs/talos-kubeadm-integration.md](docs/talos-kubeadm-integration.md)

**For complete step-by-step instructions:** See [docs/deployment-guide.md](docs/deployment-guide.md)

## Prerequisites

- Kubernetes cluster with kubeadm or Kamaji control plane
- MetalLB configured with IP sharing support
- Docker for building images
- kubectl with cluster access
- Container registry access (docker.io/bsctl by default)

## Deployment

### Using Makefile

```bash
# Complete installation workflow
make install          # Build + push + deploy

# Individual steps
make build            # Build Go binary
make docker-build     # Build Docker image
make docker-push      # Push to registry
make deploy           # Deploy to Kubernetes

# Management
make status           # Show deployment status
make logs             # View logs
make restart          # Restart pods
make undeploy         # Remove deployment
```

### Manual Deployment

```bash
# Apply Kubernetes manifests
kubectl apply -f deploy/01-secret.yaml      # Create secret first
kubectl apply -f deploy/02-deployment.yaml  # Deploy CSR signer
kubectl apply -f deploy/03-service.yaml     # Expose via LoadBalancer
```

## Configuration

### Environment Variables

Configure in `deploy/02-deployment.yaml`:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `50001` | gRPC server port |
| `CA_CERT_PATH` | `/etc/talos-ca/ca.crt` | Machine CA certificate path |
| `CA_KEY_PATH` | `/etc/talos-ca/ca.key` | Machine CA private key path |
| `TALOS_TOKEN` | *(required)* | Machine token for authentication |
| `SERVER_IPS` | *(optional)* | LoadBalancer IPs for TLS certificate SANs |

### Resource Requirements

```yaml
resources:
  requests:
    cpu: 100m
    memory: 64Mi
  limits:
    cpu: 200m
    memory: 128Mi
```

**Typical usage:** ~50MB RAM, <50m CPU per replica

### High Availability

Increase replicas for production:

```yaml
# deploy/02-deployment.yaml
spec:
  replicas: 2  # Or more
```

All replicas share the same LoadBalancer IP via MetalLB.

## Security

### CA Private Key Protection
- Stored in Kubernetes Secret with restricted permissions
- Mounted read-only in pods
- Consider enabling Kubernetes secret encryption at rest
- Rotate CA periodically (requires updating all workers)

### Token Authentication
- Same token used by all workers (shared secret model)
- Passed via gRPC metadata (not HTTP headers)
- Rotate when adding/removing workers or on suspected compromise

### Network Security
- Only authenticated requests accepted (token validation)
- Consider NetworkPolicy to restrict CSR signer access
- TLS encryption for all gRPC communication

### Container Security
- Runs as non-root user (UID 65532)
- Read-only root filesystem
- All capabilities dropped
- Distroless base image (`gcr.io/distroless/static-debian12`)

## Development

### Build Locally

```bash
# Install dependencies
make deps

# Generate protobuf code
make proto

# Build binary
make build

# Run locally (requires CA files)
CA_CERT_PATH=./ca.crt \
CA_KEY_PATH=./ca.key \
TALOS_TOKEN=your-token \
./bin/talos-csr-signer
```

### Test with Docker

```bash
# Build image
make docker-build

# Run container locally
docker run --rm \
  -v $(pwd)/ca.crt:/etc/talos-ca/ca.crt:ro \
  -v $(pwd)/ca.key:/etc/talos-ca/ca.key:ro \
  -e TALOS_TOKEN=test-token \
  -p 50001:50001 \
  docker.io/bsctl/talos-csr-signer:latest
```

### Run Tests

```bash
make test              # Run unit tests
make lint              # Run golangci-lint
make verify-deployment # Verify deployment health
```

## Makefile Commands

```bash
# Building
make build              # Build Go binary locally
make docker-build       # Build Docker image
make docker-push        # Build and push to registry

# Deployment
make deploy             # Build, push, and deploy to Kubernetes
make deploy-local       # Deploy without pushing (for local testing)
make undeploy           # Remove deployment from Kubernetes
make restart            # Restart pods (e.g., after updating secret)

# Monitoring
make status             # Show deployment status
make logs               # Show recent logs
make logs-follow        # Follow logs in real-time
make describe           # Describe deployment and pods

# Testing
make verify-deployment  # Verify deployment is healthy
make test              # Run unit tests
make lint              # Run linter

# Complete workflows
make install           # Build + push + deploy
make reinstall         # Undeploy + install
make release           # Clean + build + push
```

## Troubleshooting

Common issues and solutions:

- **Pod not starting:** Check secret exists and contains valid CA materials
- **LoadBalancer IP pending:** Verify MetalLB is installed and IP sharing annotation matches
- **Worker apid not starting:** Ensure `discovery.enabled: true` in worker config
- **Certificate signing fails:** Verify token in worker config matches secret

**For detailed troubleshooting:** See [docs/deployment-guide.md#troubleshooting](docs/deployment-guide.md#troubleshooting)

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Submit a pull request

## License

This project is licensed under the Apache License 2.0 - see LICENSE file for details.

## References

- **Talos Documentation:** https://www.talos.dev
- **Kubernetes TLS Bootstrapping:** https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/
- **Kamaji Documentation:** https://kamaji.clastix.io/

