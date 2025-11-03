# Sidecar Deployment Guide

Deployment guide for running Talos CSR Signer as a sidecar in Kamaji TenantControlPlane with Talos workers.

## Architecture

CSR Signer runs as a sidecar container in Kamaji TenantControlPlane, sharing the same LoadBalancer service on port 50001. Kamaji manages the control plane lifecycle.

## Prerequisites

- Kubernetes cluster with Kamaji installed
- kubectl with cluster admin access
- talosctl CLI
- yq CLI
- Talos worker nodes (bare metal, VMs, or cloud instances)

## Step 1: Configure Environment

```bash
cp deploy/.env.example deploy/.env
vi deploy/.env
source deploy/.env
```

Example configuration:

```bash
export CLUSTER_NAME="my-cluster"
export NAMESPACE="default"
export CONTROL_PLANE_IP="10.10.10.100"
export KUBERNETES_VERSION="v1.33.0"
export TALOS_VERSION="v1.8.3"
export WORKER_IPS="10.10.10.201 10.10.10.202 10.10.10.203"
```

## Step 2: Generate Talos Secrets

```bash
talosctl gen secrets -o secrets.yaml
```

## Step 3: Prepare Credentials

Extract Talos credentials and create Kubernetes secret:

```bash
TALOS_CA_CRT=$(yq -r '.certs.os.crt' secrets.yaml)
TALOS_CA_KEY=$(yq -r '.certs.os.key' secrets.yaml)
TALOS_TOKEN=$(yq -r '.trustdinfo.token' secrets.yaml)
TALOS_CLUSTER_ID=$(yq -r '.cluster.id' secrets.yaml)
TALOS_CLUSTER_SECRET=$(yq -r '.cluster.secret' secrets.yaml)

kubectl create secret generic ${CLUSTER_NAME}-talos-ca -n $NAMESPACE \
  --from-literal=ca.crt="$(echo "$TALOS_CA_CRT" | base64 -d)" \
  --from-literal=ca.key="$(echo "$TALOS_CA_KEY" | base64 -d)" \
  --from-literal=token="$TALOS_TOKEN"
```

## Step 4: Generate the TLS Certificate with cert-manager

The gRPC Server is server by a TLS Certificate generate by cert-manager, a dependency already required by Kamaji.

To get the certificate required by the server, deploy the following files:

- `deploy/00-secret.yaml`
- `deploy/01-issuer.yaml`
- `deploy/01-certificate.yaml`

> The said files still require some customisations regarding the Cluster name, and exposed IPs (`$WORKER_IPS`):
> this information vary according to your needs.

## Step 5: Deploy Control Plane

Deploy TenantControlPlane with the CSR Signer sidecar:

```bash
kubectl apply -f - <<EOF
apiVersion: kamaji.clastix.io/v1alpha1
kind: TenantControlPlane
metadata:
  name: $CLUSTER_NAME
  namespace: $NAMESPACE
spec:
  dataStore: default
  controlPlane:
    deployment:
      replicas: 1
      additionalContainers:
        - name: talos-csr-signer
          image: ghcr.io/clastix/talos-csr-signer:latest
          ports:
            - name: grpc
              containerPort: 50001
              protocol: TCP
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
            secretName: ${CLUSTER_NAME}-talos-ca
        - name: tls-cert
          secret:
            secretName: ${CLUSTER_NAME}-talos-tls-cert
    service:
      serviceType: LoadBalancer
      additionalMetadata:
        annotations:
          metallb.io/loadBalancerIPs: $CONTROL_PLANE_IP
      additionalPorts:
        - name: talos-csr-signer
          port: 50001
          targetPort: 50001
          protocol: TCP
  networkProfile:
    address: $CONTROL_PLANE_IP
    port: 6443
  kubernetes:
    version: $KUBERNETES_VERSION
    kubelet:
      cgroupfs: systemd
  addons:
    coreDNS: {}
    kubeProxy: {}
    konnectivity: {}
EOF

kubectl wait --for=condition=Ready tcp $CLUSTER_NAME -n $NAMESPACE --timeout=300s
kubectl logs -n $NAMESPACE -l kamaji.clastix.io/name=$CLUSTER_NAME -c talos-csr-signer --tail=20
```

## Step 5: Configure Workers

Generate the `talosconfig` for worker management:

```bash
talosctl gen config $CLUSTER_NAME https://$CONTROL_PLANE_IP:6443 \
  --with-secrets secrets.yaml \
  --output-types talosconfig \
  --output talosconfig

talosctl --talosconfig=talosconfig config endpoint $WORKER_IPS
```

Extract Kubernetes credentials and create worker configuration:

```bash
kubectl get secret ${CLUSTER_NAME}-admin-kubeconfig -n $NAMESPACE \
  -o jsonpath='{.data.admin\.conf}' | base64 -d > ${NAMESPACE}-${CLUSTER_NAME}.kubeconfig

K8S_CA=$(kubectl --kubeconfig=${NAMESPACE}-${CLUSTER_NAME}.kubeconfig config view --raw \
  -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')

JOIN_COMMAND=$(kubeadm --kubeconfig=${NAMESPACE}-${CLUSTER_NAME}.kubeconfig token create --print-join-command 2>/dev/null)
K8S_BOOTSTRAP_TOKEN=$(echo "$JOIN_COMMAND" | grep -oP 'token \K[^ ]+')

cat > worker.yaml <<EOF
version: v1alpha1
persist: true
machine:
  type: worker
  token: ${TALOS_TOKEN}
  ca:
    crt: ${TALOS_CA_CRT}
    key: ""
  kubelet:
    image: ghcr.io/siderolabs/kubelet:${KUBERNETES_VERSION}
    extraArgs:
      rotate-certificates: "true"
  install:
    disk: /dev/sda
    image: ghcr.io/siderolabs/installer:${TALOS_VERSION}
  features:
    rbac: true
    kubePrism:
      enabled: false

cluster:
  id: ${TALOS_CLUSTER_ID}
  secret: ${TALOS_CLUSTER_SECRET}
  controlPlane:
    endpoint: https://${CONTROL_PLANE_IP}:6443
  clusterName: ${CLUSTER_NAME}
  network:
    dnsDomain: cluster.local
    podSubnets:
      - 10.244.0.0/16
    serviceSubnets:
      - 10.96.0.0/12
  token: ${K8S_BOOTSTRAP_TOKEN}
  ca:
    crt: ${K8S_CA}
    key: ""
  discovery:
    enabled: true
    registries:
      kubernetes:
        disabled: true
      service:
        disabled: true
EOF
```

## Step 6: Join Workers

Deploy workers and monitor joining:

```bash
for WORKER_IP in $WORKER_IPS; do
  talosctl apply-config --insecure --nodes $WORKER_IP --file worker.yaml
done

kubectl logs -n $NAMESPACE -l kamaji.clastix.io/name=$CLUSTER_NAME -c talos-csr-signer -f
```

Verify workers are joining:

```bash
kubectl --kubeconfig=${NAMESPACE}-${CLUSTER_NAME}.kubeconfig get nodes -o wide
```

## Step 7: Deploy CNI

```bash
kubectl --kubeconfig=${NAMESPACE}-${CLUSTER_NAME}.kubeconfig apply -f \
  https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml

kubectl --kubeconfig=${NAMESPACE}-${CLUSTER_NAME}.kubeconfig wait --for=condition=ready pod \
  -l app=flannel -n kube-flannel --timeout=120s
```

## Step 8: Verify Cluster

Check Kubernetes nodes:

```bash
kubectl --kubeconfig=${NAMESPACE}-${CLUSTER_NAME}.kubeconfig get nodes -o wide
kubectl --kubeconfig=${NAMESPACE}-${CLUSTER_NAME}.kubeconfig get pods -A
```

Check CSR signing activity:

```bash
kubectl logs -n $NAMESPACE -l kamaji.clastix.io/name=$CLUSTER_NAME -c talos-csr-signer --tail=50
```

Verify Talos worker nodes:

```bash
FIRST_WORKER=$(echo $WORKER_IPS | awk '{print $1}')

talosctl --talosconfig=talosconfig -e $FIRST_WORKER -n $FIRST_WORKER version
talosctl --talosconfig=talosconfig -e $FIRST_WORKER -n $FIRST_WORKER get members
talosctl --talosconfig=talosconfig -e $FIRST_WORKER -n $FIRST_WORKER service kubelet status
talosctl --talosconfig=talosconfig -e $FIRST_WORKER -n $FIRST_WORKER dmesg | grep -i talos
```

Check all workers:

```bash
for WORKER_IP in $WORKER_IPS; do
  echo "=== Worker: $WORKER_IP ==="
  talosctl --talosconfig=talosconfig -e $WORKER_IP -n $WORKER_IP get machineconfig -o yaml | grep -A 2 kubelet
done
```

Note: Use `-e <ip> -n <ip>` for direct connection. Workers cannot forward Talos API requests in Kamaji deployments.

---

## Troubleshooting

### Workers Not Joining

```bash
kubectl logs -n $NAMESPACE -l kamaji.clastix.io/name=$CLUSTER_NAME -c talos-csr-signer
```

Common causes:
- Token mismatch: Verify token in secret matches worker.yaml
- Discovery disabled: Check `cluster.discovery.enabled: true` in worker.yaml
- Port 50001 not accessible: Verify service has port 50001 exposed

### CSR Signer Not Starting

```bash
kubectl describe pod -n $NAMESPACE -l kamaji.clastix.io/name=$CLUSTER_NAME
```

Common causes:
- Secret not found: Verify secret exists in correct namespace
- Invalid CA format: Ensure CA cert/key properly base64 decoded
- Missing token: Check TALOS_TOKEN environment variable

### Token Validation Failing

```bash
kubectl get secret ${CLUSTER_NAME}-talos-ca -n $NAMESPACE -o jsonpath='{.data.token}' | base64 -d
yq -r '.machine.token' worker.yaml
```

Tokens must match exactly.

---

## Configuration Reference

### Environment Variables

| Variable        | Default | Description                       |
|-----------------|---------|-----------------------------------|
| `PORT`          | `50001` | gRPC server port                  |
| `CA_CERT_PATH`  | `/etc/talos-ca/ca.crt` | Talos Machine CA certificate path |
| `CA_KEY_PATH`   | `/etc/talos-ca/ca.key` | Talos Machine CA private key path |
| `TLS_CERT_PATH` | `/etc/talos-ca/ca.crt` | CSR gRPC server certificate path  |
| `TLS_KEY_PATH`  | `/etc/talos-ca/ca.key` | CSR gRPC server private key path |
| `TALOS_TOKEN`   | required | Machine token for authentication  |

---

## References

- [Kamaji Documentation](https://kamaji.clastix.io)
- [Talos Documentation](https://www.talos.dev)
- [Kubernetes TLS Bootstrapping](https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/)
