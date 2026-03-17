# Infrastructure - Kubernetes Deployment (Helm Charts)

Triển khai Redis, PostgreSQL, Kafka, Keycloak và Vault với kiến trúc High Availability sử dụng Helm Charts.

## Kiến trúc

```
┌─────────────────────────────────────────────────────────────────┐
│                              INFRASTRUCTURE                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │   Redis Stack   │  │ PostgreSQL Stack│  │   Kafka Stack   │  │
│  ├─────────────────┤  ├─────────────────┤  ├─────────────────┤  │
│  │  ┌───────────┐  │  │  ┌───────────┐  │  │  ┌───────────┐  │  │
│  │  │  HAProxy  │  │  │  │  HAProxy  │  │  │  │  HAProxy  │  │  │
│  │  │  :6379    │  │  │  │  :5432    │  │  │  │  :9092    │  │  │
│  │  └─────┬─────┘  │  │  └─────┬─────┘  │  │  └─────┬─────┘  │  │
│  │   ┌────┴────┐   │  │   ┌────┴────┐   │  │   ┌────┴────┐   │  │
│  │   ▼         ▼   │  │   ▼         ▼   │  │   ▼    ▼    ▼   │  │
│  │ Master   Slaves │  │ Master   Slaves │  │ Broker Broker   │  │
│  │  (1)      (2)   │  │  (1)      (2)   │  │  (1)   (2)  (3) │  │
│  │                 │  │                 │  │   KRaft Mode    │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
│                                                                 │
│  ┌─────────────────┐  ┌─────────────────┐                       │
│  │  Keycloak IAM   │  │ HashiCorp Vault │                       │
│  ├─────────────────┤  ├─────────────────┤                       │
│  │  StatefulSet    │  │  Raft Storage   │                       │
│  │  :8080 (HTTP)   │  │  :8200 (API)    │                       │
│  └─────────────────┘  └─────────────────┘                       │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Prerequisites

- Kubernetes cluster (>= 1.26)
- Helm (>= 3.12)
- kubectl configured

## Quick Start

```bash
# Deploy tất cả infrastructure components
make deploy-all

# Hoặc deploy riêng từng component
make deploy-redis
make deploy-postgresql
make deploy-kafka
make deploy-keycloak
make deploy-vault

# Upgrade sau khi thay đổi values
make upgrade-redis
make upgrade-postgresql
make upgrade-kafka
make upgrade-keycloak
make upgrade-vault

# Keycloak DB init/repair
make wait-keycloak-init-db
make reinit-keycloak-db
make repair-keycloak
make logs-keycloak-init-db

# Kiểm tra status
make status

# Xóa tất cả
make delete-all
```

## Helm Charts

Mỗi service được đóng gói thành một Helm chart riêng biệt:

| Chart | Namespace | Description |
|-------|-----------|-------------|
| `helm/redis` | infrastructure | Redis master-slave + HAProxy |
| `helm/postgresql` | infrastructure | PostgreSQL primary-replica + HAProxy + pgvector |
| `helm/kafka` | infrastructure | Kafka KRaft (No Zookeeper) + HAProxy + Kafka UI |
| `helm/keycloak` | keycloak | Keycloak Identity & Access Management |
| `helm/vault` | infrastructure | HashiCorp Vault with Raft storage |

### Customize values

Mỗi chart có file `values.yaml` để configure. Override bằng cách:

```bash
# Sửa trực tiếp values.yaml
vi helm/redis/values.yaml

# Hoặc override khi deploy
helm upgrade redis helm/redis -n infrastructure --set slave.replicas=3

# Hoặc dùng custom values file
helm upgrade redis helm/redis -n infrastructure -f custom-redis-values.yaml
```

## Connection Endpoints

### Single Entry Points (Recommended)

Sử dụng các endpoint này trong ứng dụng - HAProxy tự động route đến master/slaves:

| Service | Host | Port | Description |
|---------|------|------|-------------|
| **Redis (Read/Write)** | `redis-haproxy.infrastructure.svc.cluster.local` | `6379` | Master - write operations |
| **Redis (Read Only)** | `redis-haproxy.infrastructure.svc.cluster.local` | `6380` | Slaves - load balanced reads |
| **PostgreSQL (Read/Write)** | `postgresql-haproxy.infrastructure.svc.cluster.local` | `5432` | Primary - write operations |
| **PostgreSQL (Read Only)** | `postgresql-haproxy.infrastructure.svc.cluster.local` | `5433` | Replicas - load balanced reads |
| **Kafka** | `kafka-haproxy.infrastructure.svc.cluster.local` | `9092` | Brokers - load balanced |
| **Keycloak** | `keycloak.keycloak.svc.cluster.local` | `8080` | IAM service |
| **Vault** | `vault.infrastructure.svc.cluster.local` | `8200` | Secret management |

### Connection Strings

```bash
# Redis
REDIS_URL="redis://:${REDIS_PASSWORD}@redis-haproxy.infrastructure.svc.cluster.local:6379"

# PostgreSQL
DATABASE_URL="postgresql://postgres:${POSTGRES_PASSWORD}@postgresql-haproxy.infrastructure.svc.cluster.local:5432/postgres"

# Kafka (KRaft Mode - No Zookeeper needed!)
KAFKA_BOOTSTRAP_SERVERS="kafka-haproxy.infrastructure.svc.cluster.local:9092"

# Keycloak
KEYCLOAK_URL="http://keycloak.keycloak.svc.cluster.local:8080"

# Vault
VAULT_ADDR="http://vault.infrastructure.svc.cluster.local:8200"
```

## Environment Variables cho Applications

```yaml
# Trong Kubernetes Deployment
env:
  # Redis
  - name: REDIS_HOST
    value: "redis-haproxy.infrastructure.svc.cluster.local"
  - name: REDIS_PORT
    value: "6379"
  - name: REDIS_PASSWORD
    valueFrom:
      secretKeyRef:
        name: redis-secret
        key: redis-password

  # PostgreSQL
  - name: DATABASE_HOST
    value: "postgresql-haproxy.infrastructure.svc.cluster.local"
  - name: DATABASE_PORT
    value: "5432"
  - name: DATABASE_PASSWORD
    valueFrom:
      secretKeyRef:
        name: postgresql-secret
        key: postgres-password

  # Kafka
  - name: KAFKA_BOOTSTRAP_SERVERS
    value: "kafka-haproxy.infrastructure.svc.cluster.local:9092"

  # Vault
  - name: VAULT_ADDR
    value: "http://vault.infrastructure.svc.cluster.local:8200"
```

## Admin UIs & Monitoring

### HAProxy Stats Pages

| Service | URL | NodePort |
|---------|-----|----------|
| Redis HAProxy | `http://<node-ip>:30384/stats` | 30384 |
| PostgreSQL HAProxy | `http://<node-ip>:30485/stats` | 30485 |
| Kafka HAProxy | `http://<node-ip>:30494/stats` | 30494 |

### Web UIs

| Service | URL | NodePort |
|---------|-----|----------|
| Kafka UI | `http://<node-ip>:30808` | 30808 |
| Keycloak Admin | `http://<node-ip>:30880/admin` | 30880 |
| Vault UI | `http://<node-ip>:30820` | 30820 |

## Port Forwarding (Local Development)

```bash
make port-forward-redis          # localhost:6379
make port-forward-postgresql     # localhost:5432
make port-forward-kafka          # localhost:9092
make port-forward-kafka-ui       # localhost:8080
make port-forward-keycloak       # localhost:8080
make port-forward-vault          # localhost:8200
```

## Scaling

```bash
# Scale Redis slaves
helm upgrade redis helm/redis -n infrastructure --set slave.replicas=3

# Scale PostgreSQL slaves
helm upgrade postgresql helm/postgresql -n infrastructure --set slave.replicas=3

# Scale Kafka brokers (min: 3 for KRaft)
helm upgrade kafka helm/kafka -n infrastructure --set kafka.replicas=5
```

## Components Detail

### Redis (Master-Slave)

| Component | Replicas | Description |
|-----------|----------|-------------|
| Master | 1 | Read/Write operations |
| Slaves | 2 | Read-only, replicated |
| HAProxy | 2 | High Availability proxy |

- **Persistence**: AOF + RDB snapshots
- **Storage**: 10Gi per instance
- **Memory**: 256Mi - 1Gi per instance

### PostgreSQL (Primary-Replica)

| Component | Replicas | Description |
|-----------|----------|-------------|
| Primary | 1 | Read/Write operations |
| Replicas | 2 | Streaming replication |
| HAProxy | 2 | High Availability proxy |

- **Replication**: Synchronous streaming
- **Storage**: 20Gi per instance
- **Memory**: 512Mi - 2Gi per instance
- **Extension**: pgvector for vector search

### Kafka (KRaft Mode)

| Component | Replicas | Description |
|-----------|----------|-------------|
| Brokers | 3 | Combined controller + broker |
| HAProxy | 2 | High Availability proxy |
| Kafka UI | 1 | Web administration |

- **Mode**: KRaft (No Zookeeper required!)
- **Replication Factor**: 3
- **Min ISR**: 2
- **Storage**: 20Gi per broker
- **Memory**: 1Gi - 2Gi per broker

### Keycloak

| Component | Replicas | Description |
|-----------|----------|-------------|
| Keycloak | 1 | IAM server |
| Init DB Job | 1 | Database initialization |

- **Database**: PostgreSQL (via HAProxy)
- **Storage**: 5Gi
- **Memory**: 512Mi - 2Gi
- **Features**: token-exchange, admin-fine-grained-authz

### Vault

| Component | Replicas | Description |
|-----------|----------|-------------|
| Vault | 1 | Secret management |

- **Storage Backend**: Integrated Raft
- **Storage**: 10Gi
- **Memory**: 256Mi - 1Gi
- **UI**: Enabled

## Secret Management (Contract-first)

> **QUAN TRONG**: Không commit plaintext secrets trong `values.yaml`.

Tất cả chart đã dùng secret contract chung:
- `secret.mode`: `existingSecret` | `externalSecret`
- `secret.name`: tên Kubernetes Secret workload sẽ đọc
- `secret.keys`: map logical key -> secret key
- `secret.externalSecret.*`: cấu hình ExternalSecret (khi dùng Vault/ESO)

### Canonical keys

- Redis: `redis-password`
- PostgreSQL: `postgres-password`, `replication-password`
- Kafka: `kafka-admin-password`
- Keycloak: `keycloak-admin-password`, `keycloak-db-password`, `keycloak-postgres-password`
- Vault: `vault-root-token`

### Migrate sang Vault (ESO)

1. Tạo `SecretStore`/`ClusterSecretStore` trỏ Vault.
2. Set mỗi chart:
   - `secret.mode=externalSecret`
   - `secret.externalSecret.enabled=true`
   - `secret.externalSecret.secretStoreRef.name=<store-name>`
   - `secret.externalSecret.data` map `remoteRef` -> `secretKey`
3. Deploy/upgrade chart.

### Rollback nhanh

Chuyển về:
- `secret.mode=existingSecret`
- đảm bảo `secret.name` trỏ Secret có đủ canonical keys.

## Logs

```bash
# HAProxy logs
make logs-redis
make logs-postgresql
make logs-kafka

# Application logs
make logs-kafka-ui
make logs-keycloak
make logs-vault

# Specific pod logs
kubectl logs -n infrastructure redis-master-0
kubectl logs -n infrastructure postgresql-master-0
kubectl logs -n infrastructure kafka-0
kubectl logs -n keycloak keycloak-0
kubectl logs -n infrastructure vault-0
```

## Troubleshooting

### Check pod status
```bash
kubectl get pods -n infrastructure -o wide
kubectl get pods -n keycloak -l app.kubernetes.io/name=keycloak -o wide
```

### Check Helm releases
```bash
helm list -n infrastructure
helm list -n keycloak
```

### Check PVC status
```bash
kubectl get pvc -n infrastructure
```

### Check services
```bash
kubectl get svc -n infrastructure
kubectl get svc -n keycloak
```

### Describe problematic pod
```bash
kubectl describe pod -n infrastructure <pod-name>
```

### Rollback a release
```bash
helm rollback redis -n infrastructure
helm rollback postgresql -n infrastructure
```

### Check HAProxy backends
```bash
curl http://<node-ip>:30384/stats  # Redis
curl http://<node-ip>:30485/stats  # PostgreSQL
curl http://<node-ip>:30494/stats  # Kafka
```

## File Structure

```
infrastructure/
├── Makefile                          # Helm deployment commands
├── README.md                         # This file
└── helm/
    ├── redis/                        # Redis Helm Chart
    │   ├── Chart.yaml
    │   ├── values.yaml
    │   └── templates/
    │       ├── _helpers.tpl
    │       ├── secret.yaml
    │       ├── configmap-master.yaml
    │       ├── configmap-slave.yaml
    │       ├── statefulset-master.yaml
    │       ├── service-master.yaml
    │       ├── statefulset-slave.yaml
    │       ├── service-slave.yaml
    │       ├── haproxy-configmap.yaml
    │       ├── haproxy-deployment.yaml
    │       ├── haproxy-service.yaml
    │       └── haproxy-service-nodeport.yaml
    ├── postgresql/                   # PostgreSQL Helm Chart
    │   ├── Chart.yaml
    │   ├── values.yaml
    │   └── templates/
    │       ├── _helpers.tpl
    │       ├── secret.yaml
    │       ├── configmap-master.yaml
    │       ├── configmap-slave.yaml
    │       ├── statefulset-master.yaml
    │       ├── service-master.yaml
    │       ├── statefulset-slave.yaml
    │       ├── service-slave.yaml
    │       ├── haproxy-configmap.yaml
    │       ├── haproxy-deployment.yaml
    │       ├── haproxy-service.yaml
    │       └── haproxy-service-nodeport.yaml
    ├── kafka/                        # Kafka Helm Chart
    │   ├── Chart.yaml
    │   ├── values.yaml
    │   └── templates/
    │       ├── _helpers.tpl
    │       ├── serviceaccount.yaml
    │       ├── rbac.yaml
    │       ├── secret.yaml
    │       ├── configmap.yaml
    │       ├── statefulset.yaml
    │       ├── service-headless.yaml
    │       ├── haproxy-configmap.yaml
    │       ├── haproxy-deployment.yaml
    │       ├── haproxy-service.yaml
    │       ├── haproxy-service-nodeport.yaml
    │       ├── kafka-ui-deployment.yaml
    │       ├── kafka-ui-service.yaml
    │       └── kafka-ui-service-nodeport.yaml
    ├── keycloak/                     # Keycloak Helm Chart
    │   ├── Chart.yaml
    │   ├── values.yaml
    │   └── templates/
    │       ├── _helpers.tpl
    │       ├── secret.yaml
    │       ├── configmap.yaml
    │       ├── job-init-db.yaml
    │       ├── statefulset.yaml
    │       ├── service-headless.yaml
    │       ├── service.yaml
    │       └── service-nodeport.yaml
    └── vault/                        # Vault Helm Chart
        ├── Chart.yaml
        ├── values.yaml
        ├── README.md
        └── templates/
            ├── _helpers.tpl
            ├── secret.yaml
            ├── configmap.yaml
            ├── serviceaccount.yaml
            ├── clusterrolebinding.yaml
            ├── role.yaml
            ├── rolebinding.yaml
            ├── statefulset.yaml
            ├── service-internal.yaml
            ├── service-active.yaml
            ├── service-standby.yaml
            ├── service-ui.yaml
            └── service-nodeport.yaml
```
