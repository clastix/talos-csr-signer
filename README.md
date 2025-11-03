# Talos CSR Signer

A standalone gRPC service that implements the Talos Security Service protocol, enabling Talos worker nodes to obtain certificates and function with non-Talos control planes.

## Overview

Talos CSR Signer bridges the gap between traditional Kubernetes control planes (kubeadm, Kamaji, managed Kubernetes) and Talos Linux worker nodes. It provides the certificate signing functionality that Talos workers expect from a native Talos control plane.

### The Problem

Talos Linux worker nodes require two separate PKI systems to function:

1. **Kubernetes PKI** - For kubelet to join the Kubernetes cluster (port 6443)
2. **Talos Machine PKI** - For the Talos API (apid) to enable node management (port 50000/50001)

In hybrid deployments where the control plane is not running Talos Linux:
- ✅ Kubernetes functionality works - kubelet joins successfully using standard bootstrap tokens
- ❌ Talos API remains unavailable - workers cannot obtain Talos certificates
- ❌ No `talosctl` access - cannot view logs, upgrade nodes, or access console

Talos workers expect a `trustd` service (part of Talos Linux) to sign Talos API certificates. Traditional control planes don't provide this service.

### The Solution

This service implements the same gRPC protocol as Talos's native `trustd`, acting as a certificate authority for the Talos Machine PKI. It runs as a standard Kubernetes workload alongside your control plane, providing certificate signing services to Talos workers.

**Result:** Talos Linux functionality on worker nodes while maintaining your existing control plane infrastructure.

## How It Works

### Dual PKI Architecture

Talos worker nodes operate with two independent PKI systems:

```
┌────────────────────────────────────────────────────┐
│ Control Plane: 10.10.10.101                        │
│                                                    │
│  Port 6443  → kube-apiserver                       │
│               Issues: Kubernetes certificates      │
│               Used by: kubelet                     │
│                                                    │
│  Port 50001 → talos-csr-signer                     │
│               Issues: Talos Machine certificates   │
│               Used by: apid (Talos API)            │
│                                                    │
└─────────────┬────────────────┬─────────────────────┘
              │                │
              │                │
      ┌───────▼────────────────▼──────┐
      │     Talos Worker              │
      │                               │
      │ kubelet → 6443 (K8s certs)    │
      │ apid    → 50001 (Talos certs) │
      └───────────────────────────────┘
```

### Certificate Signing Flow

When a Talos worker starts, it requests certificates for its API service (apid):

```
Talos Worker                 Talos CSR Signer
|                                 |
|  1. Generate CSR                |
|     (subject, IPs, DNS)         |
|                                 |
|  2. gRPC: Certificate()         |
|     + metadata: token           |
|────────────────────────────────>|
|                                 |
|              3. Validate token  |
|              4. Sign with CA    |
|                                 |
|  5. CertificateResponse         |
|     ca: <CA cert>               |
|     crt: <signed cert>          |
|<────────────────────────────────|
|                                 |
|  6. Start apid with cert        |
|                                 |
```

### Discovery and Connection

Workers locate the CSR Signer using the same discovery mechanism as native Talos clusters:

1. **Control Plane Endpoint**: Workers are configured with the control plane IP (e.g., `https://10.10.10.101:6443`)
2. **Port Translation**: Workers contact port 50001 on the same IP for certificate signing
3. **Automatic Failover**: In HA deployments, LoadBalancer routes requests to healthy CSR Signer pods

### Security Model

The CSR Signer uses the same authentication model as Talos's native `trustd`:

- **Shared Secret**: Single machine token for all workers in the cluster
- **Token Authentication**: Requests validated via gRPC metadata
- **TLS Encryption**: All communication encrypted in transit
- **CA Private Key**: Stored in Kubernetes Secret, mounted read-only

This is not a limitation but an intentional design inherited from Talos Linux.

## Deployment Models

### Sidecar Deployment (Kamaji)

Run CSR Signer as a sidecar container in Kamaji TenantControlPlane, sharing the same LoadBalancer IP on port 50001:

```yaml
apiVersion: kamaji.clastix.io/v1alpha1
kind: TenantControlPlane
spec:
  controlPlane:
    deployment:
      additionalContainers:
        - name: talos-csr-signer
          image: docker.io/bsctl/talos-csr-signer:latest
          ports:
            - containerPort: 50001
```

**Use when:**

- Running Kamaji for multi-tenant Kubernetes
- Each tenant needs isolated Talos worker support
- Control planes are dynamically provisioned

See [docs/sidecar-deployment.md](docs/sidecar-deployment.md) for complete guide.

### Standalone Deployment (kubeadm)

Run CSR Signer as a DaemonSet on control plane nodes, exposed via HostPort 50001:

```yaml
apiVersion: apps/v1
kind: DaemonSet
spec:
  template:
    spec:
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
      containers:
      - name: talos-csr-signer
        ports:
        - containerPort: 50001
          hostPort: 50001
```

**Use when:**

- Existing kubeadm control plane with VIP (keepalived, kube-vip)
- Want to add Talos workers to existing clusters
- Incremental migration to Talos

See [docs/standalone-deployment.md](docs/standalone-deployment.md) for complete guide.

## Use Cases

### When to Use CSR Signer

**Multi-Tenant Kubernetes:**
- Kamaji provides virtualized control planes
- Each tenant gets isolated Talos worker support
- Separate Machine PKI per tenant

**Cost Optimization:**
- Control plane: Managed Kubernetes (convenience, support)
- Workers: Self-managed Talos (cost-effective, secure)

### When NOT to Use CSR Signer

If you're deploying a pure Talos Linux, use the native `trustd` service that comes with Talos.


## Key Features

- **Protocol Compatible**: Wire-compatible with Talos `trustd` using SecurityService gRPC spec
- **Lightweight**: ~20MB distroless container image
- **High Availability**: Supports multiple replicas behind LoadBalancer
- **Secure**: Non-root, read-only filesystem, minimal capabilities
- **Kubernetes Native**: Deploy with kubectl, monitor with standard tools
- **Simple Operations**: Standard Kubernetes deployment patterns

## Configuration

The service is configured through environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `50001` | gRPC server port |
| `CA_CERT_PATH` | `/etc/talos-ca/ca.crt` | Talos Machine CA certificate path |
| `CA_KEY_PATH` | `/etc/talos-ca/ca.key` | Talos Machine CA private key path |
| `TALOS_TOKEN` | *(required)* | Machine token for authentication |
| `SERVER_IPS` | *(optional)* | Comma-separated IPs for TLS certificate SANs |

CA certificate, private key, and token are mounted from a Kubernetes Secret.

## Development

The `Makefile` provides targets for building and testing.

Build and test workflow:

```bash
# Install dependencies and generate protobuf code
make deps && make proto

# Build binary
make build

# Run tests
make test
make lint

# Build and push Docker image (for custom registries)
make docker-build
make docker-push IMAGE_REGISTRY=your-registry.com IMAGE_REPO=your-repo
```

Available Makefile targets:

```bash
make help         # Show all available targets

# Development
make proto        # Generate protobuf code
make deps         # Download Go module dependencies
make build        # Build binary locally
make test         # Run unit tests
make lint         # Run golangci-lint

# Docker
make docker-build # Build Docker image
make docker-push  # Build and push to registry
make docker-run   # Run container locally (testing)

# Utilities
make clean        # Clean generated files
make version      # Show version information
make env          # Show environment variables
```

For deployment instructions, see the deployment guides in [docs/](docs/).

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Submit a pull request

## License

### Apache License 2.0

The majority of this project is licensed under the **Apache License 2.0**. See the [LICENSE](LICENSE) file for full details.

### Protocol Buffer Definition - Mozilla Public License 2.0

The protocol buffer definition (`proto/security.proto`) is derived from the [Talos Linux project](https://github.com/siderolabs/talos) and is licensed under the **Mozilla Public License 2.0**. See the [LICENSE-MPL-2.0](https://www.mozilla.org/en-US/MPL/2.0/) file for full details.

This file implements the Talos gRPC protocol specification to ensure compatibility with Talos worker nodes.

### Attribution

This project implements the Talos gRPC protocol as defined by:

- **Talos Linux:** https://github.com/siderolabs/talos
- **Protocol Definition Source:** https://github.com/siderolabs/talos/blob/main/api/security/security.proto
- **Copyright:** Sidero Labs, Inc.

We gratefully acknowledge the Talos Linux project and Sidero Labs for creating and maintaining the protocol specification that makes this integration possible.

## Documentation

### Deployment Guides

Implementation details and step-by-step instructions:

- **[Sidecar Deployment (Kamaji)](docs/sidecar-deployment.md)** - Deploy as Kamaji TenantControlPlane sidecar
- **[Standalone Deployment (kubeadm)](docs/standalone-deployment.md)** - Deploy on kubeadm control planes with VIP

### External Resources

- **Talos Linux:** https://www.talos.dev
- **Kamaji:** https://kamaji.clastix.io
- **Kubernetes TLS Bootstrapping:** https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/
