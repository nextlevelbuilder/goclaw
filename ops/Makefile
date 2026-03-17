# Infrastructure & Apps Makefile
# Deploy Redis, PostgreSQL, Kafka, Keycloak, Vault, NilClaw, Portal using Helm Charts

NAMESPACE := infrastructure
KEYCLOAK_NAMESPACE := keycloak
APPS_NAMESPACE := nilclaw
HELM_DIR := helm/infrastructure
APPS_DIR := helm/apps
KEYCLOAK_INIT_DB_JOB := keycloak-init-db

.PHONY: help deploy-all deploy-redis deploy-postgresql deploy-kafka deploy-keycloak deploy-vault \
        upgrade-all upgrade-redis upgrade-postgresql upgrade-kafka upgrade-keycloak upgrade-vault \
        delete-all delete-redis delete-postgresql delete-kafka delete-keycloak delete-vault \
        deploy-apps deploy-nilclaw deploy-portal upgrade-apps upgrade-nilclaw upgrade-portal \
        delete-apps delete-nilclaw delete-portal logs-nilclaw logs-portal \
        status logs wait-keycloak-init-db reinit-keycloak-db repair-keycloak logs-keycloak-init-db

help:
	@echo "Infrastructure Helm Deployment Commands:"
	@echo ""
	@echo "  Deploy (install):"
	@echo "    make deploy-all        - Deploy all infrastructure components"
	@echo "    make deploy-redis      - Deploy Redis with master-slave + HAProxy"
	@echo "    make deploy-postgresql - Deploy PostgreSQL with master-slave + HAProxy"
	@echo "    make deploy-kafka      - Deploy Kafka KRaft + HAProxy + Kafka UI"
	@echo "    make deploy-keycloak   - Deploy Keycloak IAM (namespace: keycloak)"
	@echo "    make deploy-vault      - Deploy HashiCorp Vault with Raft storage"
	@echo ""
	@echo "  Upgrade (apply changes):"
	@echo "    make upgrade-all        - Upgrade all components"
	@echo "    make upgrade-redis      - Upgrade Redis"
	@echo "    make upgrade-postgresql - Upgrade PostgreSQL"
	@echo "    make upgrade-kafka      - Upgrade Kafka"
	@echo "    make upgrade-keycloak   - Upgrade Keycloak"
	@echo "    make upgrade-vault      - Upgrade Vault"
	@echo ""
	@echo "  Delete (uninstall):"
	@echo "    make delete-all        - Delete all infrastructure components"
	@echo "    make delete-redis      - Delete Redis"
	@echo "    make delete-postgresql - Delete PostgreSQL"
	@echo "    make delete-kafka      - Delete Kafka"
	@echo "    make delete-keycloak   - Delete Keycloak"
	@echo "    make delete-vault      - Delete Vault"
	@echo ""
	@echo "  Operations:"
	@echo "    make status            - Show status of all components"
	@echo "    make logs-redis        - Show Redis HAProxy logs"
	@echo "    make logs-postgresql   - Show PostgreSQL HAProxy logs"
	@echo "    make logs-kafka        - Show Kafka HAProxy logs"
	@echo "    make logs-keycloak     - Show Keycloak logs"
	@echo "    make logs-keycloak-init-db - Show Keycloak init DB logs"
	@echo "    make logs-vault        - Show Vault logs"
	@echo "    make wait-keycloak-init-db - Wait for Keycloak init DB job"
	@echo "    make reinit-keycloak-db    - Recreate Keycloak init DB job"
	@echo "    make repair-keycloak       - Reinit DB + restart Keycloak"
	@echo ""
	@echo "  Port Forwarding:"
	@echo "    make port-forward-redis      - localhost:6379"
	@echo "    make port-forward-postgresql  - localhost:5432"
	@echo "    make port-forward-kafka       - localhost:9092"
	@echo "    make port-forward-kafka-ui    - localhost:8080"
	@echo "    make port-forward-keycloak    - localhost:8080"
	@echo "    make port-forward-vault       - localhost:8200"
	@echo ""
	@echo "Connection Endpoints (after deployment):"
	@echo "  Redis:      redis-haproxy.$(NAMESPACE).svc.cluster.local:6379"
	@echo "  PostgreSQL: postgresql-haproxy.$(NAMESPACE).svc.cluster.local:5432"
	@echo "  Kafka:      kafka-haproxy.$(NAMESPACE).svc.cluster.local:9092"
	@echo "  Keycloak:   keycloak.$(KEYCLOAK_NAMESPACE).svc.cluster.local:8080"
	@echo "  Vault:      vault.$(NAMESPACE).svc.cluster.local:8200"

# ============================================================
# Deploy (helm install)
# ============================================================

deploy-all: deploy-redis deploy-postgresql deploy-kafka deploy-keycloak deploy-vault
	@echo ""
	@echo "============================================"
	@echo "All infrastructure components deployed!"
	@echo "============================================"
	@echo ""
	@echo "Connection Endpoints:"
	@echo "  Redis:      redis-haproxy.$(NAMESPACE).svc.cluster.local:6379"
	@echo "  Redis Read: redis-haproxy.$(NAMESPACE).svc.cluster.local:6380"
	@echo ""
	@echo "  PostgreSQL: postgresql-haproxy.$(NAMESPACE).svc.cluster.local:5432"
	@echo "  PostgreSQL Read: postgresql-haproxy.$(NAMESPACE).svc.cluster.local:5433"
	@echo ""
	@echo "  Kafka (KRaft): kafka-haproxy.$(NAMESPACE).svc.cluster.local:9092"
	@echo ""
	@echo "  Keycloak: keycloak.$(KEYCLOAK_NAMESPACE).svc.cluster.local:8080"
	@echo ""
	@echo "  Vault: vault.$(NAMESPACE).svc.cluster.local:8200"
	@echo ""
	@echo "Admin UIs (NodePort):"
	@echo "  Redis HAProxy Stats:      http://<node-ip>:30384/stats"
	@echo "  PostgreSQL HAProxy Stats: http://<node-ip>:30485/stats"
	@echo "  Kafka HAProxy Stats:      http://<node-ip>:30494/stats"
	@echo "  Kafka UI:                 http://<node-ip>:30808"
	@echo "  Keycloak Admin:           http://<node-ip>:30880/admin"
	@echo "  Vault UI:                 http://<node-ip>:30820"

deploy-redis:
	@echo "Deploying Redis master-slave with HAProxy..."
	@helm upgrade --install redis $(HELM_DIR)/redis -n $(NAMESPACE) --create-namespace
	@echo "Redis deployed successfully!"
	@echo "Connection: redis-haproxy.$(NAMESPACE).svc.cluster.local:6379"

deploy-postgresql:
	@echo "Deploying PostgreSQL master-slave with HAProxy..."
	@helm upgrade --install postgresql $(HELM_DIR)/postgresql -n $(NAMESPACE) --create-namespace
	@echo "PostgreSQL deployed successfully!"
	@echo "Connection: postgresql-haproxy.$(NAMESPACE).svc.cluster.local:5432"

deploy-kafka:
	@echo "Deploying Kafka cluster with KRaft mode (No Zookeeper)..."
	@helm upgrade --install kafka $(HELM_DIR)/kafka -n $(NAMESPACE) --create-namespace
	@echo "Kafka (KRaft) deployed successfully!"
	@echo "Connection: kafka-haproxy.$(NAMESPACE).svc.cluster.local:9092"

deploy-keycloak:
	@echo "Deploying Keycloak IAM..."
	@helm upgrade --install keycloak $(HELM_DIR)/keycloak -n $(KEYCLOAK_NAMESPACE) --create-namespace
	@$(MAKE) wait-keycloak-init-db
	@echo "Keycloak deployed successfully!"
	@echo "Connection: keycloak.$(KEYCLOAK_NAMESPACE).svc.cluster.local:8080"
	@echo "Admin UI:   http://<node-ip>:30880/admin"

deploy-vault:
	@echo "Deploying HashiCorp Vault..."
	@helm upgrade --install vault $(HELM_DIR)/vault -n $(NAMESPACE) --create-namespace
	@echo "Vault deployed successfully!"
	@echo "Connection: vault.$(NAMESPACE).svc.cluster.local:8200"
	@echo "UI:         http://<node-ip>:30820"

# ============================================================
# Upgrade (helm upgrade)
# ============================================================

upgrade-all: upgrade-redis upgrade-postgresql upgrade-kafka upgrade-keycloak upgrade-vault
	@echo "All infrastructure components upgraded!"

upgrade-redis:
	@echo "Upgrading Redis..."
	@helm upgrade redis $(HELM_DIR)/redis -n $(NAMESPACE)
	@echo "Redis upgraded!"

upgrade-postgresql:
	@echo "Upgrading PostgreSQL..."
	@helm upgrade postgresql $(HELM_DIR)/postgresql -n $(NAMESPACE)
	@echo "PostgreSQL upgraded!"

upgrade-kafka:
	@echo "Upgrading Kafka..."
	@helm upgrade kafka $(HELM_DIR)/kafka -n $(NAMESPACE)
	@echo "Kafka upgraded!"

upgrade-keycloak:
	@echo "Upgrading Keycloak..."
	@helm upgrade keycloak $(HELM_DIR)/keycloak -n $(KEYCLOAK_NAMESPACE)
	@$(MAKE) wait-keycloak-init-db
	@echo "Keycloak upgraded!"

upgrade-vault:
	@echo "Upgrading Vault..."
	@helm upgrade vault $(HELM_DIR)/vault -n $(NAMESPACE)
	@echo "Vault upgraded!"

# ============================================================
# Keycloak DB Init / Repair
# ============================================================

wait-keycloak-init-db:
	@echo "Waiting for Keycloak init DB job ($(KEYCLOAK_INIT_DB_JOB))..."
	@kubectl wait --for=condition=complete -n $(KEYCLOAK_NAMESPACE) job/$(KEYCLOAK_INIT_DB_JOB) --timeout=180s

reinit-keycloak-db:
	@echo "Recreating Keycloak init DB job..."
	@kubectl delete job $(KEYCLOAK_INIT_DB_JOB) -n $(KEYCLOAK_NAMESPACE) --ignore-not-found
	@helm upgrade keycloak $(HELM_DIR)/keycloak -n $(KEYCLOAK_NAMESPACE)
	@$(MAKE) wait-keycloak-init-db

repair-keycloak: reinit-keycloak-db
	@echo "Restarting Keycloak StatefulSet..."
	@kubectl rollout restart statefulset/keycloak -n $(KEYCLOAK_NAMESPACE)
	@kubectl rollout status statefulset/keycloak -n $(KEYCLOAK_NAMESPACE) --timeout=180s
	@echo "Keycloak repair flow completed!"

logs-keycloak-init-db:
	@kubectl logs -n $(KEYCLOAK_NAMESPACE) job/$(KEYCLOAK_INIT_DB_JOB) --tail=200

# ============================================================
# Delete (helm uninstall)
# ============================================================

delete-all: delete-vault delete-keycloak delete-kafka delete-postgresql delete-redis
	@echo "All infrastructure components deleted!"

delete-redis:
	@echo "Deleting Redis..."
	@helm uninstall redis -n $(NAMESPACE) 2>/dev/null || echo "Redis release not found"
	@echo "Redis deleted!"

delete-postgresql:
	@echo "Deleting PostgreSQL..."
	@helm uninstall postgresql -n $(NAMESPACE) 2>/dev/null || echo "PostgreSQL release not found"
	@echo "PostgreSQL deleted!"

delete-kafka:
	@echo "Deleting Kafka..."
	@helm uninstall kafka -n $(NAMESPACE) 2>/dev/null || echo "Kafka release not found"
	@echo "Kafka deleted!"

delete-keycloak:
	@echo "Deleting Keycloak..."
	@helm uninstall keycloak -n $(KEYCLOAK_NAMESPACE) 2>/dev/null || echo "Keycloak release not found"
	@echo "Keycloak deleted!"

delete-vault:
	@echo "Deleting Vault..."
	@helm uninstall vault -n $(NAMESPACE) 2>/dev/null || echo "Vault release not found"
	@echo "Vault deleted!"

# ============================================================
# Status
# ============================================================

status:
	@echo "============================================"
	@echo "Helm Releases"
	@echo "============================================"
	@echo ""
	@echo "--- Infrastructure namespace ---"
	@helm list -n $(NAMESPACE) 2>/dev/null || echo "No releases found"
	@echo ""
	@echo "--- Keycloak namespace ($(KEYCLOAK_NAMESPACE)) ---"
	@helm list -n $(KEYCLOAK_NAMESPACE) 2>/dev/null || echo "No releases found"
	@echo ""
	@echo "============================================"
	@echo "Kubernetes Resources"
	@echo "============================================"
	@echo ""
	@echo "--- Pods ---"
	@kubectl get pods -n $(NAMESPACE) -o wide 2>/dev/null || echo "No pods found"
	@echo ""
	@echo "--- Keycloak Pods ---"
	@kubectl get pods -n $(KEYCLOAK_NAMESPACE) -l app.kubernetes.io/name=keycloak -o wide 2>/dev/null || echo "No Keycloak pods found"
	@echo ""
	@echo "--- Services ---"
	@kubectl get services -n $(NAMESPACE) 2>/dev/null || echo "No services found"
	@echo ""
	@echo "--- Keycloak Services ---"
	@kubectl get services -n $(KEYCLOAK_NAMESPACE) -l app.kubernetes.io/name=keycloak 2>/dev/null || echo "No Keycloak services found"
	@echo ""
	@echo "--- StatefulSets ---"
	@kubectl get statefulsets -n $(NAMESPACE) 2>/dev/null || echo "No statefulsets found"
	@echo ""
	@echo "--- Deployments ---"
	@kubectl get deployments -n $(NAMESPACE) 2>/dev/null || echo "No deployments found"
	@echo ""
	@echo "--- PersistentVolumeClaims ---"
	@kubectl get pvc -n $(NAMESPACE) 2>/dev/null || echo "No PVCs found"

# ============================================================
# Logs
# ============================================================

logs-redis:
	@kubectl logs -n $(NAMESPACE) -l app=redis-haproxy --tail=100 -f

logs-postgresql:
	@kubectl logs -n $(NAMESPACE) -l app=postgresql-haproxy --tail=100 -f

logs-kafka:
	@kubectl logs -n $(NAMESPACE) -l app=kafka-haproxy --tail=100 -f

logs-kafka-ui:
	@kubectl logs -n $(NAMESPACE) -l app=kafka-ui --tail=100 -f

logs-keycloak:
	@kubectl logs -n $(KEYCLOAK_NAMESPACE) -l app.kubernetes.io/name=keycloak --tail=100 -f

logs-vault:
	@kubectl logs -n $(NAMESPACE) -l app=vault --tail=100 -f

# ============================================================
# Port Forwarding
# ============================================================

port-forward-redis:
	@echo "Forwarding Redis to localhost:6379..."
	@kubectl port-forward -n $(NAMESPACE) svc/redis-haproxy 6379:6379

port-forward-postgresql:
	@echo "Forwarding PostgreSQL to localhost:5432..."
	@kubectl port-forward -n $(NAMESPACE) svc/postgresql-haproxy 5432:5432

port-forward-kafka:
	@echo "Forwarding Kafka to localhost:9092..."
	@kubectl port-forward -n $(NAMESPACE) svc/kafka-haproxy 9092:9092

port-forward-kafka-ui:
	@echo "Forwarding Kafka UI to localhost:8080..."
	@kubectl port-forward -n $(NAMESPACE) svc/kafka-ui 8080:8080

port-forward-keycloak:
	@echo "Forwarding Keycloak to localhost:8080..."
	@kubectl port-forward -n $(KEYCLOAK_NAMESPACE) svc/keycloak 8080:8080

port-forward-vault:
	@echo "Forwarding Vault to localhost:8200..."
	@kubectl port-forward -n $(NAMESPACE) svc/vault-ui 8200:8200

# ============================================================
# Scaling (via Helm upgrade)
# ============================================================

scale-redis-slaves:
	@read -p "Enter number of Redis slaves: " count; \
	helm upgrade redis $(HELM_DIR)/redis -n $(NAMESPACE) --set slave.replicas=$$count

scale-postgresql-slaves:
	@read -p "Enter number of PostgreSQL slaves: " count; \
	helm upgrade postgresql $(HELM_DIR)/postgresql -n $(NAMESPACE) --set slave.replicas=$$count

scale-kafka-brokers:
	@read -p "Enter number of Kafka brokers (min 3): " count; \
	helm upgrade kafka $(HELM_DIR)/kafka -n $(NAMESPACE) --set kafka.replicas=$$count

# ============================================================
# Helm utilities
# ============================================================

template-redis:
	@helm template redis $(HELM_DIR)/redis -n $(NAMESPACE)

template-postgresql:
	@helm template postgresql $(HELM_DIR)/postgresql -n $(NAMESPACE)

template-kafka:
	@helm template kafka $(HELM_DIR)/kafka -n $(NAMESPACE)

template-keycloak:
	@helm template keycloak $(HELM_DIR)/keycloak -n $(KEYCLOAK_NAMESPACE)

template-vault:
	@helm template vault $(HELM_DIR)/vault -n $(NAMESPACE)

lint:
	@echo "Linting all charts..."
	@helm lint $(HELM_DIR)/redis
	@helm lint $(HELM_DIR)/postgresql
	@helm lint $(HELM_DIR)/kafka
	@helm lint $(HELM_DIR)/keycloak
	@helm lint $(HELM_DIR)/vault
	@helm lint $(APPS_DIR)/nilclaw
	@helm lint $(APPS_DIR)/portal
	@echo "All charts passed linting!"

# ============================================================
# Apps: Deploy (helm install)
# ============================================================

deploy-apps: deploy-nilclaw deploy-portal
	@echo ""
	@echo "============================================"
	@echo "All application components deployed!"
	@echo "============================================"
	@echo ""
	@echo "  NilClaw Gateway: nilclaw.$(APPS_NAMESPACE).svc.cluster.local:18790"
	@echo "  Portal UI:       portal.$(APPS_NAMESPACE).svc.cluster.local:80"

deploy-nilclaw:
	@echo "Deploying NilClaw AI Gateway..."
	@helm upgrade --install nilclaw $(APPS_DIR)/nilclaw -n $(APPS_NAMESPACE) --create-namespace
	@echo "NilClaw deployed successfully!"
	@echo "Connection: nilclaw.$(APPS_NAMESPACE).svc.cluster.local:18790"

deploy-portal:
	@echo "Deploying Portal Web UI..."
	@helm upgrade --install portal $(APPS_DIR)/portal -n $(APPS_NAMESPACE) --create-namespace
	@echo "Portal deployed successfully!"
	@echo "Connection: portal.$(APPS_NAMESPACE).svc.cluster.local:80"

# ============================================================
# Apps: Upgrade (helm upgrade)
# ============================================================

upgrade-apps: upgrade-nilclaw upgrade-portal
	@echo "All application components upgraded!"

upgrade-nilclaw:
	@echo "Upgrading NilClaw..."
	@helm upgrade nilclaw $(APPS_DIR)/nilclaw -n $(APPS_NAMESPACE)
	@echo "NilClaw upgraded!"

upgrade-portal:
	@echo "Upgrading Portal..."
	@helm upgrade portal $(APPS_DIR)/portal -n $(APPS_NAMESPACE)
	@echo "Portal upgraded!"

# ============================================================
# Apps: Delete (helm uninstall)
# ============================================================

delete-apps: delete-portal delete-nilclaw
	@echo "All application components deleted!"

delete-nilclaw:
	@echo "Deleting NilClaw..."
	@helm uninstall nilclaw -n $(APPS_NAMESPACE) 2>/dev/null || echo "NilClaw release not found"
	@echo "NilClaw deleted!"

delete-portal:
	@echo "Deleting Portal..."
	@helm uninstall portal -n $(APPS_NAMESPACE) 2>/dev/null || echo "Portal release not found"
	@echo "Portal deleted!"

# ============================================================
# Apps: Logs
# ============================================================

logs-nilclaw:
	@kubectl logs -n $(APPS_NAMESPACE) -l app.kubernetes.io/name=nilclaw --tail=100 -f

logs-portal:
	@kubectl logs -n $(APPS_NAMESPACE) -l app.kubernetes.io/name=portal --tail=100 -f

# ============================================================
# Apps: Port Forwarding
# ============================================================

port-forward-nilclaw:
	@echo "Forwarding NilClaw to localhost:18790..."
	@kubectl port-forward -n $(APPS_NAMESPACE) svc/nilclaw 18790:18790

port-forward-portal:
	@echo "Forwarding Portal to localhost:8080..."
	@kubectl port-forward -n $(APPS_NAMESPACE) svc/portal 8080:80

# ============================================================
# Apps: Template (dry-run)
# ============================================================

template-nilclaw:
	@helm template nilclaw $(APPS_DIR)/nilclaw -n $(APPS_NAMESPACE)

template-portal:
	@helm template portal $(APPS_DIR)/portal -n $(APPS_NAMESPACE)
