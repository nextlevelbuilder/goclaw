# HashiCorp Vault Helm Chart

## Overview

Vault with integrated Raft storage backend for secret management.

## Deployment

```bash
# Deploy via Makefile
make deploy-vault

# Or deploy directly with Helm
helm install vault helm/vault -n infrastructure --create-namespace

# Upgrade after changing values
helm upgrade vault helm/vault -n infrastructure
```

## Initialize Vault (First Time Only)

```bash
# Initialize with 5 key shares, 3 key threshold
kubectl exec -n infrastructure vault-0 -- vault operator init \
  -key-shares=5 \
  -key-threshold=3 \
  -format=json > vault-init.json

# IMPORTANT: Save vault-init.json securely!
# It contains unseal keys and root token.
```

## Unseal Vault

After initialization or pod restart, Vault needs to be unsealed:

```bash
# Unseal with 3 different keys (from vault-init.json)
kubectl exec -n infrastructure vault-0 -- vault operator unseal <key1>
kubectl exec -n infrastructure vault-0 -- vault operator unseal <key2>
kubectl exec -n infrastructure vault-0 -- vault operator unseal <key3>

# Check status
kubectl exec -n infrastructure vault-0 -- vault status
```

## Login

```bash
kubectl exec -n infrastructure vault-0 -- vault login <root-token>
```

## Enable Secrets Engines

```bash
# Enable KV secrets engine
kubectl exec -n infrastructure vault-0 -- vault secrets enable -path=secret kv-v2

# Enable Kubernetes auth
kubectl exec -n infrastructure vault-0 -- vault auth enable kubernetes

# Configure Kubernetes auth
kubectl exec -n infrastructure vault-0 -- vault write auth/kubernetes/config \
  kubernetes_host="https://$KUBERNETES_PORT_443_TCP_ADDR:443"
```

## Create a Policy

```bash
kubectl exec -n infrastructure vault-0 -- vault policy write app-policy - <<EOF
path "secret/data/app/*" {
  capabilities = ["read", "list"]
}
EOF
```

## Create Kubernetes Auth Role

```bash
kubectl exec -n infrastructure vault-0 -- vault write auth/kubernetes/role/aiugo \
  bound_service_account_names=default \
  bound_service_account_namespaces=aiugo \
  policies=app-policy \
  ttl=24h
```

## Store & Read Secrets

```bash
# Store a secret
kubectl exec -n infrastructure vault-0 -- vault kv put secret/app/database \
  username="admin" \
  password="secret-password"

# Read a secret
kubectl exec -n infrastructure vault-0 -- vault kv get secret/app/database
```

## Configuration

Key values in `values.yaml`:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `replicas` | `1` | Number of Vault instances |
| `image.tag` | `1.15` | Vault image version |
| `resources.requests.memory` | `256Mi` | Memory request |
| `resources.limits.memory` | `1Gi` | Memory limit |
| `storage.size` | `10Gi` | PVC size for Raft data |
| `vault.logLevel` | `info` | Log verbosity |
| `vault.ui` | `true` | Enable Vault UI |
| `vault.config.tlsDisable` | `true` | Disable TLS (enable in production) |
| `nodePort.enabled` | `true` | Expose via NodePort |
| `nodePort.port` | `30820` | NodePort number |

## Access Vault UI

```bash
# Via NodePort
open http://<node-ip>:30820

# Via port-forward
kubectl port-forward -n infrastructure svc/vault-ui 8200:8200
open http://localhost:8200
```

## Connection Details

| Service | Address | Purpose |
|---------|---------|---------|
| `vault.infrastructure.svc.cluster.local:8200` | Active node only | Write operations |
| `vault-standby.infrastructure.svc.cluster.local:8200` | All nodes | Read operations |
| `vault-ui.infrastructure.svc.cluster.local:8200` | UI access | Web interface |
| `vault-internal.infrastructure.svc.cluster.local:8200` | Internal | StatefulSet DNS |

## Auto-Unseal (Production)

For production, consider using auto-unseal with:
- AWS KMS
- Azure Key Vault
- GCP Cloud KMS
- HashiCorp Vault Transit

## Backup

```bash
# Create Raft snapshot
kubectl exec -n infrastructure vault-0 -- vault operator raft snapshot save /tmp/raft.snap

# Copy snapshot locally
kubectl cp infrastructure/vault-0:/tmp/raft.snap ./vault-backup.snap
```

## Troubleshooting

```bash
# Check logs
kubectl logs -n infrastructure vault-0

# Check Raft peers
kubectl exec -n infrastructure vault-0 -- vault operator raft list-peers

# Force leader election
kubectl exec -n infrastructure vault-0 -- vault operator step-down

# Rollback Helm release
helm rollback vault -n infrastructure
```
