# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Purpose

Repository này chứa Helm charts để triển khai stack hạ tầng trên Kubernetes:
- Redis (master/slave + HAProxy)
- PostgreSQL (primary/replica + HAProxy, image pgvector)
- Kafka KRaft (không dùng Zookeeper) + HAProxy + Kafka UI
- Keycloak (IAM)
- Vault (integrated Raft storage)

Điểm điều phối chính là `Makefile`; toàn bộ lifecycle deploy/upgrade/delete vận hành qua các target `make`.

## Prerequisites

- Kubernetes cluster >= 1.26
- Helm >= 3.12
- `kubectl` đã trỏ đúng cluster/context

## Common Commands

### Khám phá nhanh
```bash
make help
```

### Validate cấu hình (thay cho build/test truyền thống)
```bash
# Lint tất cả chart
make lint

# Lint 1 chart (single-chart check)
helm lint helm/redis

# Render manifest để review trước khi apply
make template-redis
make template-postgresql
make template-kafka
make template-keycloak
make template-vault
```

### Deploy / Upgrade
```bash
# Deploy toàn bộ stack
make deploy-all

# Deploy từng component
make deploy-redis
make deploy-postgresql
make deploy-kafka
make deploy-keycloak
make deploy-vault

# Upgrade sau khi đổi values/template
make upgrade-all
make upgrade-redis
make upgrade-postgresql
make upgrade-kafka
make upgrade-keycloak
make upgrade-vault
```

### Vận hành & quan sát
```bash
# Tổng quan releases + resources
make status

# Logs
make logs-redis
make logs-postgresql
make logs-kafka
make logs-kafka-ui
make logs-keycloak
make logs-vault

# Keycloak DB init/repair flow
make wait-keycloak-init-db
make reinit-keycloak-db
make repair-keycloak
make logs-keycloak-init-db
```

### Local access bằng port-forward
```bash
make port-forward-redis
make port-forward-postgresql
make port-forward-kafka
make port-forward-kafka-ui
make port-forward-keycloak
make port-forward-vault
```

### Gỡ stack
```bash
make delete-all
# hoặc delete riêng từng component
```

## Architecture (Big Picture)

### 1) Orchestration layer
- `Makefile` là entrypoint duy nhất để vận hành charts.
- Có 2 namespace logic:
  - `infrastructure`: redis, postgresql, kafka, vault
  - `keycloak`: keycloak
- Target `deploy-keycloak` và `upgrade-keycloak` có bước chờ Job init DB hoàn tất trước khi coi là thành công.

### 2) Pattern chung của data services
- Redis/PostgreSQL/Kafka đều dùng mô hình **single entrypoint qua HAProxy**.
- App nên kết nối qua HAProxy service thay vì pod/service nội bộ riêng lẻ.
- Redis và PostgreSQL hỗ trợ tách luồng read/write bằng cổng riêng trên cùng HAProxy service.

### 3) Redis chart
- Topology: 1 master + N slave (StatefulSet tách master/slave).
- HAProxy dùng TCP health check để xác định master/slave và route tương ứng.
- Persistence bật AOF + snapshot (RDB).

### 4) PostgreSQL chart
- Topology: 1 primary + N replica, replication streaming.
- Sử dụng image `pgvector/pgvector` (Postgres + extension vector).
- HAProxy tách cổng primary(read/write) và readonly(read scaling).

### 5) Kafka chart (KRaft)
- Không có Zookeeper; broker kiêm controller.
- `initContainer` tạo/lấy `CLUSTER_ID` qua ConfigMap dùng chung để đảm bảo các broker dùng cùng cluster ID.
- Cơ chế này cần RBAC để đọc/ghi ConfigMap trong namespace release.

### 6) Keycloak chart
- Keycloak chạy StatefulSet, cấu hình DB trỏ về `postgresql-haproxy.infrastructure.svc.cluster.local`.
- Job `*-init-db` (Postgres client) đảm nhiệm tạo user/database/grant quyền trước khi Keycloak dùng DB.
- Đây là dependency xuyên namespace quan trọng nhất trong repo.

### 7) Vault chart
- Vault chạy StatefulSet với storage backend `raft` (integrated storage).
- File `vault.hcl` trong ConfigMap dùng placeholder `HOSTNAME`; container startup `sed` để render theo pod name trước khi chạy `vault server`.
- `updateStrategy: OnDelete` nên việc rollout cần chủ động hơn khi thay đổi.

## Important Files

- `Makefile`: orchestration commands cho toàn bộ repo
- `README.md`: kiến trúc tổng quan, endpoint, vận hành
- `helm/*/values.yaml`: cấu hình chính cho từng service
- `helm/keycloak/templates/job-init-db.yaml`: logic bootstrap DB của Keycloak
- `helm/kafka/templates/statefulset.yaml`: KRaft cluster-id bootstrap + runtime env
- `helm/vault/templates/configmap.yaml` + `helm/vault/templates/statefulset.yaml`: Vault raft config/render runtime

## Notes for Future Claude Instances

- Không có `.cursor/rules`, `.cursorrules`, hoặc `.github/copilot-instructions.md` trong repo tại thời điểm phân tích.
- Repository này không có test suite ứng dụng; “validation” thực tế là `helm lint`, `helm template`, và kiểm tra runtime qua `make status` + logs.
- Nhiều credentials mặc định đang nằm trong `values.yaml`; cần thay đổi trước môi trường production.
