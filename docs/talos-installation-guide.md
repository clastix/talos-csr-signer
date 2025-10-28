# Talos Linux Installation Guide

## Overview

Practical guide for installing Talos Linux on Proxmox VE 8 and bare metal. For general Talos information, architecture details, and other platforms, see the [official Talos documentation](https://www.talos.dev/).

## Prerequisites

- Talos v1.11.3 or higher
- `talosctl` CLI installed
- `kubectl` for cluster access
- For Proxmox: Proxmox VE 8.0+

**Minimum requirements**: 2 CPU cores, 2GB RAM, 10GB disk
**Recommended**: 4 CPU cores, 4GB RAM, 20GB SSD

## Proxmox VE 8 Installation

Detailed procedure for deploying a complete Talos cluster (1 control plane + 2 workers) on Proxmox VE 8.

### Phase 1: Download Talos Image

#### On Proxmox Host

```bash
# SSH to Proxmox
ssh root@your-proxmox-ip

# Check network bridge
brctl show
# Note your bridge name (e.g., vnet1 or vmbr0)

# Check storage
pvesm status
# Note your storage (e.g., local or local-lvm)

# Download Talos image
cd /var/lib/vz/template/iso
curl -L -o talos-v1.11.3-nocloud-amd64.raw.xz \
  "https://factory.talos.dev/image/376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba/v1.11.3/nocloud-amd64.raw.xz"

# Extract (keeps compressed file with -k)
xz -dk talos-v1.11.3-nocloud-amd64.raw.xz

# Verify (~4.2GB extracted)
ls -lh talos-v1.11.3-nocloud-amd64.raw*

# Create resource pool (optional but recommended)
pvesh create /pools -poolid talos -comment "Talos Linux Cluster VMs"
```

### Phase 2: Create Control Plane VM

```bash
# Set variables (adjust for your environment)
VMID=120
STORAGE="local"      # Your storage from Phase 1
BRIDGE="vnet1"       # Your bridge from Phase 1

# Create VM with host CPU type (CRITICAL!)
qm create $VMID \
  --name talos-control-plane \
  --memory 4096 \
  --cores 4 \
  --cpu host \
  --net0 virtio,bridge=$BRIDGE \
  --scsihw virtio-scsi-pci \
  --pool talos

# Import disk
qm importdisk $VMID /var/lib/vz/template/iso/talos-v1.11.3-nocloud-amd64.raw $STORAGE

# Attach and configure (for directory storage)
qm set $VMID --scsi0 local:120/vm-120-disk-0.raw
# For LVM: qm set $VMID --scsi0 local-lvm:vm-120-disk-0

qm set $VMID --boot order=scsi0 --ostype l26
qm set $VMID --efidisk0 ${STORAGE}:1,efitype=4m,pre-enrolled-keys=0
qm set $VMID --vga std

# Resize to 20GB BEFORE first boot (CRITICAL!)
qm resize $VMID scsi0 +16G

# Verify config
qm config $VMID | grep -E "cpu|scsi0|boot"

# Start VM
qm start $VMID
sleep 30
```

**Get Control Plane IP**: Open Proxmox web console (VM 120 → Console) and note the IP address shown on the Talos screen.

### Phase 3: Create Worker Nodes

```bash
# Worker 1 (VM 121)
VMID=121
qm create $VMID --name talos-worker-01 --memory 4096 --cores 4 --cpu host \
  --net0 virtio,bridge=$BRIDGE --scsihw virtio-scsi-pci --pool talos
qm importdisk $VMID /var/lib/vz/template/iso/talos-v1.11.3-nocloud-amd64.raw $STORAGE
qm set $VMID --scsi0 local:121/vm-121-disk-0.raw --boot order=scsi0 --ostype l26
qm set $VMID --efidisk0 ${STORAGE}:1,efitype=4m,pre-enrolled-keys=0 --vga std
qm resize $VMID scsi0 +16G
qm start $VMID

# Worker 2 (VM 122)
VMID=122
qm create $VMID --name talos-worker-02 --memory 4096 --cores 4 --cpu host \
  --net0 virtio,bridge=$BRIDGE --scsihw virtio-scsi-pci --pool talos
qm importdisk $VMID /var/lib/vz/template/iso/talos-v1.11.3-nocloud-amd64.raw $STORAGE
qm set $VMID --scsi0 local:122/vm-122-disk-0.raw --boot order=scsi0 --ostype l26
qm set $VMID --efidisk0 ${STORAGE}:1,efitype=4m,pre-enrolled-keys=0 --vga std
qm resize $VMID scsi0 +16G
qm start $VMID
```

**Get Worker IPs**: Check Proxmox web console for VM 121 and VM 122 IP addresses.

### Phase 4: Configure Talos Cluster

#### On Your Workstation

```bash
# Set control plane IP (from Phase 2)
CONTROL_PLANE_IP="192.168.11.105"  # Your actual IP
CLUSTER_NAME="talos-proxmox"
ENDPOINT="https://${CONTROL_PLANE_IP}:6443"

# Generate configuration
mkdir -p ~/talos-config && cd ~/talos-config
talosctl gen config $CLUSTER_NAME $ENDPOINT

# Apply to control plane
talosctl apply-config --insecure --nodes $CONTROL_PLANE_IP --file controlplane.yaml
sleep 180  # Wait for installation

# Configure talosctl client
cp talosconfig ~/.talos/config
talosctl config endpoint $CONTROL_PLANE_IP
talosctl config node $CONTROL_PLANE_IP

# Verify connectivity
talosctl version

# Bootstrap Kubernetes
talosctl bootstrap
talosctl health --wait-timeout 10m

# Get kubeconfig
talosctl kubeconfig ~/.kube/config
kubectl get nodes
```

### Phase 5: Join Worker Nodes

```bash
# Set worker IPs (from Phase 3)
WORKER1_IP="192.168.11.106"  # Your actual IPs
WORKER2_IP="192.168.11.107"

# Apply worker configuration
talosctl apply-config --insecure --nodes $WORKER1_IP --file worker.yaml
talosctl apply-config --insecure --nodes $WORKER2_IP --file worker.yaml
sleep 180  # Wait for workers to join

# Verify all nodes
kubectl get nodes

# Optional: Label workers
kubectl label node talos-worker-01 node-role.kubernetes.io/worker=
kubectl label node talos-worker-02 node-role.kubernetes.io/worker=
```

### Verify Cluster Health

```bash
# Check cluster health
talosctl health --nodes $CONTROL_PLANE_IP

# Check cluster members
talosctl get members --nodes $CONTROL_PLANE_IP

# Check pod distribution
kubectl get pods -n kube-system -o wide
```

### Common Issues

**VM won't boot**: CPU type not set to `host`
```bash
qm stop <VMID>
qm set <VMID> --cpu host
qm start <VMID>
```

**Ephemeral partition warning**: Disk resized after first boot (recreate VM)

**Workers won't join**: Check network bridge matches control plane

For detailed troubleshooting, see [Talos documentation](https://www.talos.dev/latest/talos-guides/troubleshooting/).

### Quick Reference: Add Worker VM

```bash
# On Proxmox (adjust VMID, STORAGE, BRIDGE)
VMID=123 && STORAGE="local" && BRIDGE="vnet1" && \
qm create $VMID --name talos-worker-03 --memory 4096 --cores 4 --cpu host \
  --net0 virtio,bridge=$BRIDGE --scsihw virtio-scsi-pci --pool talos && \
qm importdisk $VMID /var/lib/vz/template/iso/talos-v1.11.3-nocloud-amd64.raw $STORAGE && \
qm set $VMID --scsi0 local:${VMID}/vm-${VMID}-disk-0.raw --boot order=scsi0 --ostype l26 && \
qm set $VMID --efidisk0 ${STORAGE}:1,efitype=4m,pre-enrolled-keys=0 --vga std && \
qm resize $VMID scsi0 +16G && \
qm start $VMID

# On workstation
WORKER_IP="192.168.11.xxx"  # Get from console
talosctl apply-config --insecure --nodes $WORKER_IP --file worker.yaml
```

## Next Steps

After successful installation:

1. **Join to kubeadm cluster**: See [talos-kubeadm-integration.md](talos-kubeadm-integration.md)
2. **Install CNI**: If not using default Flannel
3. **Configure storage**: Install Longhorn or local-path-provisioner
4. **Set up monitoring**: Prometheus/Grafana for cluster visibility
5. **Backup configuration**: Keep copies of machine configs and talosconfig

## Resources

- **Official Documentation**: https://www.talos.dev/
- **GitHub Releases**: https://github.com/siderolabs/talos/releases
- **Image Factory**: https://factory.talos.dev/
- **Community**: https://github.com/siderolabs/talos/discussions

## Quick Reference Commands

```bash
# Download talosctl
curl -sL https://talos.dev/install | sh

# Apply configuration
talosctl apply-config --insecure --nodes <IP> --file config.yaml

# Check status
talosctl --nodes <IP> version
talosctl --nodes <IP> services
talosctl --nodes <IP> dashboard

# Troubleshoot
talosctl --nodes <IP> logs kubelet
talosctl --nodes <IP> dmesg
talosctl --nodes <IP> get members
```

