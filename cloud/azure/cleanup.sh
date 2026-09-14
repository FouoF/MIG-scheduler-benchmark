#!/usr/bin/env bash
set -euo pipefail

group=${AZURE_RESOURCE_GROUP:?set AZURE_RESOURCE_GROUP to the dedicated MIGBench resource group}
az group show --name "$group" --query '{name:name,location:location}' -o table
az group delete --name "$group" --yes --no-wait
printf 'Deletion requested for Azure resource group %s.\n' "$group"
