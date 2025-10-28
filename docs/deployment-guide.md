# Talos CSR Signer - Deployment Guide

Complete step-by-step guide for deploying the Talos CSR Signer service to enable `talosctl` access for Talos workers joining kubeadm/Kamaji control planes.

## Overview

This guide walks through:
1. Generating Talos configuration and extracting credentials
2. Creating Kubernetes secrets  
3. Configuring and deploying the CSR signer service
4. Configuring and deploying Talos workers
5. Verifying the complete setup

**Time required:** 20-30 minutes  
**Skill level:** Intermediate Kubernetes/Talos knowledge

## Prerequisites

Before starting, ensure you have:

1. **Kubernetes cluster** with kubeadm or Kamaji control plane running
2. **MetalLB** installed and configured with IP address pool
3. **Docker** installed for building images
4. **kubectl** configured with cluster admin access
5. **talosctl** CLI tool installed
6. **Container registry** credentials (docker.io/bsctl by default)
7. **Control plane LoadBalancer IP** (e.g., 10.10.10.101)

## Step 1: Generate Talos Configuration

Generate a fresh Talos configuration to obtain the Machine CA and token:

```bash
# Create temporary directory
mkdir -p ~/talos-config
cd ~/talos-config

# Generate Talos config (use your control plane endpoint)
talosctl gen config sample-cluster https://10.10.10.101:6443

# This creates:
#   controlplane.yaml - Contains Machine CA and token
#   worker.yaml       - Worker config template  
#   talosconfig       - Admin credentials
```

**What this does:**
- Generates a new Talos Machine CA (separate from Kubernetes CA)
- Creates machine token for authentication
- Creates configuration templates

## Step 2: Extract Machine CA and Token

Extract the required credentials from the generated configuration:

```bash
# Extract Machine CA certificate (base64-encoded)
grep -A 20 "machine:" controlplane.yaml | \
  grep -A 1 "ca:" | \
  grep "crt:" | \
  awk '{print $2}' > machine-ca.crt.b64

# Extract Machine CA private key (base64-encoded)
grep -A 20 "machine:" controlplane.yaml | \
  grep -A 2 "ca:" | \
  grep "key:" | \
  awk '{print $2}' > machine-ca.key.b64

# Extract machine token
grep "token:" controlplane.yaml | \
  head -1 | \
  awk '{print $2}' > machine-token.txt

# Verify extraction
echo "CA Cert (first 50 chars): $(head -c 50 machine-ca.crt.b64)"
echo "CA Key (first 50 chars): $(head -c 50 machine-ca.key.b64)"
echo "Token: $(cat machine-token.txt)"
```

**Expected output:**
```
CA Cert (first 50 chars): LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJQZE...
CA Key (first 50 chars): LS0tLS1CRUdJTiBFRDI1NTE5IFBSSVZBVEUKS0VZLS0tL...
Token: ask2bc.ur2wne77uxy26po9
```

## Step 3: Create Kubernetes Secret

Create a Kubernetes secret with the Machine CA materials:

```bash
# Decode base64-encoded PEM files
base64 -d machine-ca.crt.b64 > /tmp/machine-ca.crt
base64 -d machine-ca.key.b64 > /tmp/machine-ca.key

# Create secret (kubectl will base64-encode the files automatically)
kubectl create secret generic talos-machine-ca -n default \
  --from-file=ca.crt=/tmp/machine-ca.crt \
  --from-file=ca.key=/tmp/machine-ca.key \
  --from-literal=token=$(cat machine-token.txt) \
  --dry-run=client -o yaml | kubectl apply -f -

# Verify secret was created
kubectl get secret talos-machine-ca -n default

# Clean up temporary files
rm -f /tmp/machine-ca.crt /tmp/machine-ca.key
```

**Verification:**
```bash
# Check secret contents (should show 3 keys)
kubectl get secret talos-machine-ca -n default -o jsonpath='{.data}' | jq 'keys'
# Expected: ["ca.crt","ca.key","token"]
```

## Step 4: Configure Deployment

Update the deployment manifest with your LoadBalancer IP:

```bash
cd /path/to/talos-csr-signer

# Edit deploy/02-deployment.yaml
vi deploy/02-deployment.yaml
```

Update the `SERVER_IPS` environment variable (line 41):
```yaml
- name: SERVER_IPS
  value: "10.10.10.101"  # Replace with your control plane IP
```

**Why this matters:** The CSR signer generates a TLS certificate that includes this IP in the Subject Alternative Names (SANs).

## Step 5: Configure LoadBalancer Service

Update the service manifest with your control plane IP:

```bash
# Edit deploy/03-service.yaml
vi deploy/03-service.yaml
```

Update the LoadBalancer IP (line 15):
```yaml
spec:
  type: LoadBalancer
  loadBalancerIP: 10.10.10.101  # Must match control plane IP
```

**Critical:** The `metallb.io/allow-shared-ip: "shared-ip"` annotation must match the annotation on your control plane service.

## Step 6: Ensure Control Plane Service Has IP Sharing

Your control plane service (kube-apiserver) must also have the MetalLB IP sharing annotation:

```bash
# Check control plane service annotations
kubectl get svc <control-plane-service> -n <namespace> -o yaml | grep allow-shared-ip

# If missing, add the annotation:
kubectl annotate svc <control-plane-service> -n <namespace> \
  metallb.io/allow-shared-ip="shared-ip" --overwrite
```

**For Kamaji:** The control plane service is usually named after your tenant (e.g., `sample-cluster`).

**For kubeadm:** The service may be in the kube-system namespace and named `kubernetes`.

## Step 7: Build and Deploy

Build the Docker image and deploy to Kubernetes:

```bash
cd /path/to/talos-csr-signer

# Build and push Docker image
make docker-build
make docker-push

# Deploy to Kubernetes
make deploy
```

**What this does:**
1. Builds Docker image with tag `docker.io/bsctl/talos-csr-signer:latest`
2. Pushes image to registry
3. Verifies secret is configured
4. Deploys secret, deployment, and service to Kubernetes
5. Waits for deployment to be ready

**Expected output:**
```
✓ Docker image built
✓ Docker image pushed
✓ Secret appears to be configured
Deploying to Kubernetes namespace: default
...
deployment.apps/talos-csr-signer condition met
✓ Deployed to Kubernetes

Deployment Status
Pods:
NAME                               READY   STATUS    RESTARTS   AGE
talos-csr-signer-xxxxxxxxx-xxxxx   1/1     Running   0          30s

Services:
NAME               TYPE           CLUSTER-IP      EXTERNAL-IP    PORT(S)
talos-csr-signer   LoadBalancer   10.96.xxx.xxx   10.10.10.101   50001:xxxxx/TCP
```

## Step 8: Verify Deployment

Check that the CSR signer is running and has the correct LoadBalancer IP:

```bash
# Check pods
kubectl get pods -l app=talos-csr-signer -n default

# Check service
kubectl get svc talos-csr-signer -n default

# Check logs
kubectl logs -l app=talos-csr-signer -n default
```

**Expected logs:**
```
2025/10/28 12:00:00 Loaded CA certificate and private key
2025/10/28 12:00:00 Valid token prefix: ask2bc...
2025/10/28 12:00:00 Generated TLS certificate for gRPC server with IPs: [10.10.10.101 127.0.0.1]
2025/10/28 12:00:00 Talos CSR Signer listening on port 50001 with TLS enabled
```

**Verify LoadBalancer IP allocation:**
```bash
EXTERNAL_IP=$(kubectl get svc talos-csr-signer -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "CSR Signer LoadBalancer IP: $EXTERNAL_IP"
# Should output: 10.10.10.101
```

## Step 9: Configure Talos Worker

Create a Talos worker configuration that points to both the Kubernetes API and CSR signer:

```yaml
version: v1alpha1
machine:
  type: worker

  # Machine token from Step 2
  token: ask2bc.ur2wne77uxy26po9

  # Machine CA certificate from Step 2 (base64-encoded)
  ca:
    crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t...
    key: ""  # Empty - workers don't need CA private key

  network:
    hostname: talos-worker-01
    interfaces:
      - interface: eth0
        dhcp: true

  kubelet:
    image: ghcr.io/siderolabs/kubelet:v1.29.0
    clusterDNS:
      - 10.96.0.10  # Adjust to your cluster's DNS service IP
    extraArgs:
      node-labels: "node-role.kubernetes.io/worker="

  install:
    disk: /dev/sda
    image: ghcr.io/siderolabs/installer:v1.8.3

cluster:
  # Kubernetes cluster configuration
  controlPlane:
    endpoint: https://10.10.10.101:6443

  # Kubernetes CA certificate (NOT Talos Machine CA)
  ca:
    crt: LS0tLS1CRUdJTi...  # Get from: cat /etc/kubernetes/pki/ca.crt | base64 -w0

  # Kubernetes bootstrap token
  token: abcdef.0123456789abcdef  # Get from: kubeadm token create

  network:
    dnsDomain: cluster.local
    podSubnets:
      - 10.244.0.0/16  # Adjust to your cluster's pod subnet
    serviceSubnets:
      - 10.96.0.0/12   # Adjust to your cluster's service subnet
    cni:
      name: none  # kubeadm/Kamaji manages CNI

  # CRITICAL: Enable discovery so workers can find CSR signer
  discovery:
    enabled: true
    registries:
      kubernetes:
        disabled: true
      service:
        disabled: true
```

**Key configuration points:**
- `machine.token` and `machine.ca.crt` - From generated Talos config (Step 2)
- `cluster.controlPlane.endpoint` - Control plane IP:6443
- `cluster.ca.crt` - **Kubernetes CA** (not Talos CA), get from `/etc/kubernetes/pki/ca.crt`
- `cluster.token` - **Kubernetes bootstrap token**, get from `kubeadm token create`
- `cluster.discovery.enabled: true` - **Required** for worker to find CSR signer

## Step 10: Deploy Talos Worker

Apply the configuration to your Talos worker node:

```bash
# Get worker node IP (from DHCP or console)
WORKER_IP=192.168.11.100

# Apply configuration
talosctl apply-config \
  --nodes ${WORKER_IP} \
  --file talos-worker.yaml \
  --insecure

# Monitor bootstrap process
talosctl --nodes ${WORKER_IP} logs kubelet --follow
```

**Expected kubelet logs:**
```
[INFO] Starting kubelet...
[INFO] Using bootstrap token for authentication
[INFO] Successfully registered node talos-worker-01
```

## Step 11: Verify Worker Joined Cluster

Check that the worker joined the Kubernetes cluster:

```bash
# Check nodes
kubectl get nodes

# Expected output:
# NAME              STATUS   ROLES    AGE   VERSION
# control-plane     Ready    control-plane   10d   v1.29.0
# talos-worker-01   Ready    <none>   30s   v1.29.0

# Check CSR signer logs for certificate signing
kubectl logs -l app=talos-csr-signer -n default --tail=20
```

**Expected CSR signer logs:**
```
2025/10/28 12:05:00 === New Certificate Request Received ===
2025/10/28 12:05:00 Metadata extracted successfully
2025/10/28 12:05:00 Token found in metadata
2025/10/28 12:05:00 Token validated successfully
2025/10/28 12:05:00 CSR parsed successfully
2025/10/28 12:05:00 CSR signature verified
2025/10/28 12:05:00 ✓ Certificate signed successfully for: apid
2025/10/28 12:05:00 === Certificate Request Completed Successfully ===
```

## Step 12: Verify Talos API Access

Test that `talosctl` can access the worker:

```bash
# Check Talos services
talosctl --nodes ${WORKER_IP} services

# Expected output should show:
# STATE   HEALTH   ID       IMAGE
# Running Healthy  apid     ...    ← This should be Running

# Test other talosctl commands
talosctl --nodes ${WORKER_IP} version
talosctl --nodes ${WORKER_IP} dashboard
```

---

## Troubleshooting

### Issue 1: CSR Signer Pod Not Starting

**Symptoms:**
```bash
$ kubectl get pods -l app=talos-csr-signer
NAME                               READY   STATUS    RESTARTS   AGE
talos-csr-signer-xxxxxxxxx-xxxxx   0/1     Error     3          2m
```

**Check logs:**
```bash
kubectl logs -l app=talos-csr-signer -n default
```

**Common causes:**
- **Missing secret:** `Failed to read CA certificate: open /etc/talos-ca/ca.crt: no such file`
  - Solution: Verify secret exists: `kubectl get secret talos-machine-ca -n default`
- **Invalid CA format:** `Failed to decode PEM private key`
  - Solution: Regenerate secret with correct base64-decoded PEM files
- **Invalid token:** `TALOS_TOKEN environment variable is required`
  - Solution: Verify secret has `token` key: `kubectl get secret talos-machine-ca -o jsonpath='{.data.token}'`

### Issue 2: LoadBalancer IP Not Assigned

**Symptoms:**
```bash
$ kubectl get svc talos-csr-signer -n default
NAME               TYPE           EXTERNAL-IP   PORT(S)
talos-csr-signer   LoadBalancer   <pending>     50001:xxxxx/TCP
```

**Causes:**
1. **MetalLB not installed/configured**
   ```bash
   # Check MetalLB pods
   kubectl get pods -n metallb-system
   ```

2. **IP sharing not configured**
   ```bash
   # Check both services have matching annotation
   kubectl get svc talos-csr-signer -n default -o yaml | grep allow-shared-ip
   kubectl get svc <control-plane-svc> -n <namespace> -o yaml | grep allow-shared-ip

   # Values must match exactly
   ```

3. **IP address already in use**
   ```bash
   # Check IP pool configuration
   kubectl get ipaddresspool -n metallb-system -o yaml
   ```

### Issue 3: Worker apid Service Not Starting

**Symptoms:**
```bash
$ talosctl --nodes ${WORKER_IP} services
...
apid     Stopped  ...
```

**Check apid logs:**
```bash
talosctl --nodes ${WORKER_IP} logs apid
```

**Common causes:**
1. **Discovery disabled**
   - Error: `failed to discover endpoints`
   - Solution: Set `discovery.enabled: true` in worker config

2. **CSR signer not reachable**
   - Error: `connection refused` or `timeout`
   - Solution: Verify network connectivity:
     ```bash
     # From worker node
     telnet 10.10.10.101 50001
     ```

3. **Token mismatch**
   - Error: `invalid token` in CSR signer logs
   - Solution: Ensure `machine.token` in worker config matches token in secret

4. **CA certificate mismatch**
   - Error: `certificate verification failed`
   - Solution: Ensure `machine.ca.crt` in worker config matches CA in secret

### Issue 4: MetalLB IP Conflict

**Symptoms:**
- CSR signer gets different IP than control plane
- Both services show same IP but only one is reachable

**Solution:**
```bash
# Verify IP sharing annotation matches exactly
kubectl get svc talos-csr-signer -n default -o jsonpath='{.metadata.annotations.metallb\.io/allow-shared-ip}'
kubectl get svc <control-plane> -n <namespace> -o jsonpath='{.metadata.annotations.metallb\.io/allow-shared-ip}'

# Should both output: shared-ip

# If different, update to match:
kubectl annotate svc talos-csr-signer -n default \
  metallb.io/allow-shared-ip="shared-ip" --overwrite
```

### Issue 5: Certificate Signing Fails

**Symptoms:**
- Worker apid service fails to start
- CSR signer logs show: `invalid CSR signature` or `failed to create certificate`

**Check CSR signer logs:**
```bash
kubectl logs -l app=talos-csr-signer -n default --tail=50
```

**Common causes:**
1. **CA private key corrupted**
   ```bash
   # Regenerate secret from original files
   make undeploy
   # Go back to Step 3
   ```

2. **Token authentication failed**
   - Check CSR signer logs for: `ERROR: Invalid token received`
   - Verify worker is using correct token

---

## Configuration Reference

### Environment Variables

The CSR signer supports the following environment variables (configured in `deploy/02-deployment.yaml`):

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `50001` | gRPC server port |
| `CA_CERT_PATH` | `/etc/talos-ca/ca.crt` | Path to Machine CA certificate |
| `CA_KEY_PATH` | `/etc/talos-ca/ca.key` | Path to Machine CA private key |
| `TALOS_TOKEN` | *(required)* | Machine token for authentication |
| `SERVER_IPS` | *(optional)* | Comma-separated LoadBalancer IPs for TLS certificate |

### Resource Requirements

Default resource limits (configured in `deploy/02-deployment.yaml`):

```yaml
resources:
  requests:
    cpu: 100m
    memory: 64Mi
  limits:
    cpu: 200m
    memory: 128Mi
```

Typical usage: ~50MB RAM, <50m CPU per replica.

### Makefile Commands

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

# Complete workflows
make install           # Build + push + deploy
make reinstall         # Undeploy + install
```

---

## Next Steps

After successful deployment:

1. **Add more workers:** Repeat Steps 9-12 for additional workers
2. **Enable High Availability:** Increase CSR signer replicas in `deploy/02-deployment.yaml`
3. **Monitor operations:** Set up monitoring for CSR signer pods and worker nodes
4. **Review security:** Implement Kubernetes secret encryption at rest, NetworkPolicy restrictions

## References

- **Architecture Guide:** [../talos-kubeadm-integration.md](../talos-kubeadm-integration.md)
- **Project README:** [../README.md](../README.md)
- **Talos Documentation:** https://www.talos.dev
- **MetalLB IP Sharing:** https://metallb.universe.tf/usage/#ip-address-sharing
- **Kubernetes TLS Bootstrapping:** https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/
