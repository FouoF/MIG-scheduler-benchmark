#!/usr/bin/env bash
set -euo pipefail

# Creates the CPU-only host used by the control-plane benchmark. The resource
# group is deliberately isolated so cleanup can remove every billable object.
group=${AZURE_RESOURCE_GROUP:-migbench-$(date +%Y%m%d)}
location=${AZURE_LOCATION:-eastasia}
vm=${AZURE_VM_NAME:-migbench-cp}
size=${AZURE_VM_SIZE:-Standard_D8as_v5}

az group create --name "$group" --location "$location" \
  --tags purpose=migbench owner=codex >/dev/null
az vm create --resource-group "$group" --name "$vm" --location "$location" \
  --image Ubuntu2404 --size "$size" --os-disk-size-gb 128 \
  --storage-sku Premium_LRS --admin-username azureuser \
  --generate-ssh-keys --public-ip-sku Standard >/dev/null

ip=$(az vm show -d --resource-group "$group" --name "$vm" --query publicIps -o tsv)
printf 'resource_group=%s\nvm=%s\nip=%s\n' "$group" "$vm" "$ip"
