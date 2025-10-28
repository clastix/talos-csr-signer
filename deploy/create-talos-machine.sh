#!/bin/bash
set -e

# Talos Worker VM Creation Script
# This script creates a Talos worker VM on Proxmox

# Configuration
VMID=121
VM_NAME="talos-worker-01"
STORAGE="local"
BRIDGE="vnet1"
IMAGE="/var/lib/vz/template/iso/talos-v1.11.3-nocloud-amd64.raw"
MEMORY=4096
CORES=4
DISK_SIZE="+16G"

echo "========================================="
echo "Talos Worker VM Creation Script"
echo "========================================="
echo "VMID: $VMID"
echo "Name: $VM_NAME"
echo "Storage: $STORAGE"
echo "Bridge: $BRIDGE"
echo "Memory: ${MEMORY}MB"
echo "Cores: $CORES"
echo "========================================="

# Check if VM exists and stop/destroy it
if qm status $VMID &>/dev/null; then
    echo "VM $VMID exists. Stopping and destroying..."
    qm stop $VMID 2>/dev/null || true
    sleep 2
    qm destroy $VMID
    echo "VM $VMID destroyed."
else
    echo "VM $VMID does not exist. Proceeding with creation..."
fi

echo ""
echo "Creating VM $VMID..."
qm create $VMID \
    --name "$VM_NAME" \
    --memory $MEMORY \
    --cores $CORES \
    --cpu host \
    --net0 virtio,bridge=$BRIDGE \
    --scsihw virtio-scsi-pci \
    --pool talos

echo "Importing disk..."
qm importdisk $VMID "$IMAGE" $STORAGE

echo "Configuring disk and boot..."
qm set $VMID \
    --scsi0 ${STORAGE}:${VMID}/vm-${VMID}-disk-0.raw \
    --boot order=scsi0 \
    --ostype l26

echo "Configuring EFI disk..."
qm set $VMID --efidisk0 ${STORAGE}:1,efitype=4m,pre-enrolled-keys=0

echo "Configuring serial console (required for qm terminal)..."
qm set $VMID --serial0 socket --vga std

echo "Resizing disk..."
qm resize $VMID scsi0 $DISK_SIZE

echo ""
echo "Starting VM $VMID..."
qm start $VMID

echo ""
echo "========================================="
echo "VM Creation Complete!"
echo "========================================="
echo ""
echo "Next steps:"
echo "1. Wait 30 seconds for VM to boot"
echo "2. On kamaji-cmp host, run:"
echo "   cd /home/bsctl/in-progress"
echo "   talosctl apply-config --nodes 192.168.11.100 --file talos-worker-milano.yaml --insecure"
echo ""
echo "3. Monitor console with: qm terminal $VMID"
echo "   (Press Ctrl+O to exit console)"
echo ""
echo "4. Check CSR signer logs for certificate requests:"
echo "   kubectl logs -n default -l app=talos-csr-signer --tail=50"
echo ""
echo "========================================="
