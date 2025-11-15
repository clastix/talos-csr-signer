# Talos CSR Signer

A standalone gRPC service that implements the Talos Security Service protocol, enabling Talos worker nodes to obtain certificates and function with non-Talos control planes.

> Warning: This project is still experimental and in active development. Features and instructions may change.

_tl;dr;_ ▶️  watch the [live action demo](https://youtu.be/nSGo_72LnmY) of the project.

## Overview

Talos CSR Signer bridges the gap between traditional Kubernetes control planes (kubeadm, Kamaji) and Talos Linux worker nodes. It provides the certificate signing functionality that Talos workers expect from a native Talos control plane.

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
|     (subject, IPs)              |
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

Workers locate the CSR Signer using the same discovery mechanism as Talos's native `trustd`:

1. **Control Plane Endpoint**: Workers are configured with the control plane IP (e.g., `https://10.10.10.100:6443`)
2. **Port Translation**: Workers contact port 50001 on the same IP for certificate signing
3. **Automatic Failover**: In HA deployments, LoadBalancer routes requests to healthy CSR Signer pods

### Security Model

The CSR Signer uses the same authentication model as Talos's native `trustd`:

- **Shared Secret**: Single machine token for all workers in the cluster
- **Token Authentication**: Requests validated via gRPC metadata
- **TLS Encryption**: All communication encrypted in transit
- **CA Private Key**: Stored in Kubernetes Secret, mounted read-only

This is an intentional design inherited from Talos Linux.

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
          image: ghcr.io/clastix/talos-csr-signer:latest
          ports:
            - containerPort: 50001
          env:
            - name: TALOS_TOKEN
              valueFrom:
                secretKeyRef:
                  name: cluster-talos-ca
                  key: token
          volumeMounts:
            - name: talos-ca
              mountPath: /etc/talos-ca
              readOnly: true
            - name: tls-cert
              mountPath: /etc/talos-server-crt
              readOnly: true
      additionalVolumes:
        - name: talos-ca
          secret:
            secretName: cluster-talos-ca
        - name: tls-cert
          secret:
            secretName: cluster-talos-tls-cert
    service:
      additionalPorts:
        - name: talos-csr-signer
          port: 50001
          targetPort: 50001
```

Use when:

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
        image: ghcr.io/clastix/talos-csr-signer:latest
        ports:
        - containerPort: 50001
          hostPort: 50001
        env:
        - name: TALOS_TOKEN
          valueFrom:
            secretKeyRef:
              name: cluster-talos-ca
              key: token
        volumeMounts:
        - name: talos-ca
          mountPath: /etc/talos-ca
          readOnly: true
        - name: tls-cert
          mountPath: /etc/talos-server-crt
          readOnly: true
      volumes:
      - name: talos-ca
        secret:
          secretName: cluster-talos-ca
      - name: tls-cert
        secret:
          secretName: cluster-talos-tls-cert
```

Use when:

- Existing kubeadm control plane with VIP (keepalived, kube-vip)
- Want to add Talos workers to existing clusters

See [docs/standalone-deployment.md](docs/standalone-deployment.md) for complete guide.

## Use Cases

### When to Use CSR Signer

**Multi-Tenant Kubernetes:**
- Kamaji provides virtualized control planes
- Each tenant gets isolated Talos workers machines
- Separate Machine PKI per tenant

**Cost Optimization:**
- Control plane: Managed Kubernetes (convenience, support)
- Workers: Self-managed Talos (cost-effective, secure)

### When NOT to Use CSR Signer

If you're deploying a pure Talos Linux, use the native `trustd` service that comes with Talos control planes.

## Configuration

The service is configured through environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `50001` | gRPC server port |
| `CA_CERT_PATH` | `/etc/talos-ca/tls.crt` | Talos Machine CA certificate path |
| `CA_KEY_PATH` | `/etc/talos-ca/tls.key` | Talos Machine CA private key path |
| `TLS_CERT_PATH` | `/etc/talos-server-crt/tls.crt` | CSR gRPC server certificate path |
| `TLS_KEY_PATH` | `/etc/talos-server-crt/tls.key` | CSR gRPC server private key path |
| `TALOS_TOKEN` | *(required)* | Machine token for authentication |

### Prerequisites

- **cert-manager**: Required to generate TLS certificates for the gRPC server
- **Talos secrets**: Generated using `talosctl gen secrets`

The Talos Machine CA certificate, private key, and token are stored in a Kubernetes Secret. The gRPC server TLS certificate is generated by cert-manager using the Talos Machine CA as the issuer.

Talos uses ED25519 keys with non-RFC-7468-compliant PEM labels (`BEGIN ED25519 PRIVATE KEY`). The `cert-manager` requires RFC 7468 format (`BEGIN PRIVATE KEY`). The deployment guides include a simple `sed` workaround to fix the PEM label without modifying the actual key bytes.

## Development

Build and test workflow:

```bash
# Install dependencies and generate protobuf code
make deps && make proto

# Build binary
make build

# Run tests
make test
make lint

# Build container image with ko (default)
make docker-build

# Build container image with Docker
docker build -t docker.io/bsctl/talos-csr-signer:latest .
docker push docker.io/bsctl/talos-csr-signer:latest
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

# Container Images
make oci-build    # Build OCI image with ko
make oci-run      # Run container locally (testing)

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

This project is licensed under the **Apache License 2.0**. See the [LICENSE](LICENSE) file for full details.

### Protocol Buffer Definition - Mozilla Public License 2.0

The protocol buffer definition (`proto/security.proto`) is derived from the [Talos Linux project](https://github.com/siderolabs/talos) and is licensed under the **Mozilla Public License 2.0**. See the [LICENSE-MPL-2.0](https://www.mozilla.org/en-US/MPL/2.0/) file for full details.

This file implements the Talos gRPC protocol specification to ensure compatibility with Talos worker nodes.

### Attribution

This project implements the Talos gRPC protocol as defined by:

- **Talos Linux:** https://github.com/siderolabs/talos
- **Protocol Definition Source:** https://github.com/siderolabs/talos/blob/main/api/security/security.proto
- **Copyright:** Sidero Labs, Inc.

We gratefully acknowledge the Talos Linux project and Sidero Labs for creating and maintaining the protocol specification that makes this integration possible.

> A special mention to Andrei Kvapil from [Ænix](https://aenix.io/), the creators of [Cozystack](https://cozystack.io/):
> with its [PoC](https://github.com/cozystack/standalone-trustd/blob/main/POC.md) the ideas has been validated and finalized here.

## Documentation

### Deployment Guides

Implementation details and step-by-step instructions:

- **[Sidecar Deployment (Kamaji)](docs/sidecar-deployment.md)** - Deploy as Kamaji TenantControlPlane sidecar
- **[Standalone Deployment (kubeadm)](docs/standalone-deployment.md)** - Deploy on kubeadm control planes with VIP

### External Resources

- **Talos Linux:** https://www.talos.dev
- **Kamaji:** https://kamaji.clastix.io
- **Kubernetes TLS Bootstrapping:** https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/
