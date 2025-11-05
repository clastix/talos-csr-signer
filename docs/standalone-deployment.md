# Standalone Deployment Guide

Deployment guide for running Talos CSR Signer on kubeadm control planes with Talos workers.

## Architecture

CSR Signer runs as a DaemonSet on control plane nodes, exposed via HostPort 50001 on the control plane VIP. Compatible with keepalived, kube-vip, and external load balancers.

## Prerequisites

- Kubernetes control plane installed with kubeadm
- Control plane VIP via keepalived, kube-vip, or external LB
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
export CONTROL_PLANE_IP="10.10.10.250"
export KUBERNETES_VERSION="v1.33.0"
export TALOS_VERSION="v1.8.3"
export WORKER_IPS="192.168.11.102 192.168.11.103"
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

## Step 4: Deploy CSR Signer

Deploy CSR Signer DaemonSet:

```bash
cat > talos-csr-signer-daemonset.yaml <<EOF
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: talos-csr-signer
  namespace: ${NAMESPACE}
  labels:
    app: talos-csr-signer
spec:
  selector:
    matchLabels:
      app: talos-csr-signer
  template:
    metadata:
      labels:
        app: talos-csr-signer
    spec:
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
      tolerations:
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
      - key: node-role.kubernetes.io/master
        operator: Exists
        effect: NoSchedule
      containers:
      - name: talos-csr-signer
        image: ghcr.io/clastix/talos-csr-signer:latest
        imagePullPolicy: Always
        ports:
        - name: grpc
          containerPort: 50001
          protocol: TCP
          hostPort: 50001
        env:
          - name: TALOS_TOKEN
            valueFrom:
              secretKeyRef:
                name: ${CLUSTER_NAME}-talos-ca
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
          secretName: ${CLUSTER_NAME}-talos-ca
      - name: tls-cert
        secret:
          secretName: ${CLUSTER_NAME}-talos-tls-cert
EOF

kubectl apply -f talos-csr-signer-daemonset.yaml
kubectl wait --for=condition=ready pod -l app=talos-csr-signer -n ${NAMESPACE} --timeout=120s
kubectl logs -l app=talos-csr-signer -n ${NAMESPACE} --tail=20
```

## Step 5: Configure Workers

Generate talosconfig for worker management:

```bash
talosctl gen config $CLUSTER_NAME https://$CONTROL_PLANE_IP:6443 \
  --with-secrets secrets.yaml \
  --output-types talosconfig \
  --output talosconfig

talosctl --talosconfig=talosconfig config endpoint $WORKER_IPS
```

Extract Kubernetes credentials and create worker configuration:

```bash
K8S_CA=$(kubectl get configmap -n kube-public cluster-info -o jsonpath='{.data.kubeconfig}' | grep certificate-authority-data | awk '{print $2}')
K8S_BOOTSTRAP_TOKEN=$(kubeadm token create --ttl 24h)

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

kubectl logs -l app=talos-csr-signer -n ${NAMESPACE} -f
```

Verify workers are joining:

```bash
kubectl get nodes -o wide
```

## Step 7: Deploy CNI

```bash
kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml
kubectl wait --for=condition=ready pod -l app=flannel -n kube-flannel --timeout=120s
```

## Step 8: Verify Cluster

Check Kubernetes nodes:

```bash
kubectl get nodes -o wide
kubectl get pods -A
```

Check CSR signing activity:

```bash
kubectl logs -l app=talos-csr-signer -n ${NAMESPACE} --tail=50
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

Note: Use `-e <ip> -n <ip>` for direct connection. Workers cannot forward Talos API requests in standalone deployments.

---

## Troubleshooting

### CSR Signer Not Starting

```bash
kubectl logs -l app=talos-csr-signer -n ${NAMESPACE}
```

Common causes:
- Missing secret: `kubectl get secret ${CLUSTER_NAME}-talos-ca -n ${NAMESPACE}`
- Invalid CA format: Regenerate secret (Step 4)
- HostPort conflict: Check port 50001 usage on control plane nodes

### Workers Cannot Reach CSR Signer

```bash
kubectl get pods -l app=talos-csr-signer -n ${NAMESPACE} -o wide
nc -zv $CONTROL_PLANE_IP 50001
```

Check:
- Firewall rules on control plane nodes (port 50001)
- VIP routing (keepalived, kube-vip)
- Cert Manager certificate IPs match VIP

### Worker apid Not Starting

```bash
talosctl --talosconfig=talosconfig -e <worker-ip> -n <worker-ip> logs apid
```

Common causes:
- Discovery disabled: Verify `discovery.enabled: true` in worker.yaml
- Token mismatch: Verify `machine.token` matches secret
- Wrong Kubernetes token: Verify `cluster.token` is Kubernetes bootstrap token

### Workers Show NotReady

Install CNI (Step 9) or verify existing CNI:

```bash
kubectl get pods -n kube-flannel
kubectl get pods -n kube-system -l k8s-app=calico-node
```

### DaemonSet Not Scheduling

```bash
kubectl get nodes --show-labels | grep control-plane
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.taints}{"\n"}{end}'
```

Verify node labels include `node-role.kubernetes.io/control-plane=`

---

## Configuration Reference

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `50001` | gRPC server port |
| `CA_CERT_PATH` | `/etc/talos-ca/ca.crt` | Talos Machine CA certificate path |
| `CA_KEY_PATH` | `/etc/talos-ca/ca.key` | Talos Machine CA private key path |
| `TALOS_TOKEN` | required | Machine token for authentication |

---

## References

- [Talos Documentation](https://www.talos.dev)
- [Kubernetes TLS Bootstrapping](https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/)
