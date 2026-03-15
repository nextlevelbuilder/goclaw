# Kubernetes MCP Server Deployment Guide

## Overview

Deploy MCP servers to Kubernetes using Helm charts and kubectl. After successful deployment, register the server in GoClaw via `register_mcp_server` using the NodePort/IP exposed by the K8s service.

---

## Prerequisites

Before deploying, verify:

1. **kubeconfig** — Valid kubeconfig file accessible at `~/.kube/config` or via `KUBECONFIG` env var
2. **kubectl** — Installed and connected to the target cluster
3. **helm** — Helm 3+ installed
4. **Container image** — MCP server built and pushed to a container registry

### Validate Kubeconfig

**CRITICAL: Always verify kubeconfig before any kubectl/helm operation.**

```bash
# Check current context
kubectl config current-context

# Verify cluster connectivity
kubectl cluster-info

# List available contexts (if multi-cluster)
kubectl config get-contexts

# Switch context if needed
kubectl config use-context <context-name>

# Verify namespace access
kubectl get namespaces
```

If kubeconfig is not at the default path:

```bash
# Set explicitly
export KUBECONFIG=/path/to/kubeconfig

# Or pass per-command
kubectl --kubeconfig=/path/to/kubeconfig cluster-info
```

---

## Step 1: Containerize the MCP Server

### Dockerfile (TypeScript)

```dockerfile
FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:22-alpine
WORKDIR /app
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./

EXPOSE 3000
ENV MCP_TRANSPORT=streamable-http
ENV MCP_PORT=3000

CMD ["node", "dist/index.js"]
```

### Dockerfile (Python)

```dockerfile
FROM python:3.12-slim AS builder
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir --target=/deps -r requirements.txt

FROM python:3.12-slim
WORKDIR /app
COPY --from=builder /deps /usr/local/lib/python3.12/site-packages
COPY . .

EXPOSE 3000
ENV MCP_TRANSPORT=streamable-http
ENV MCP_PORT=3000

CMD ["python", "-m", "server"]
```

### Build and Push

```bash
# Build image
docker build -t <registry>/<name>-mcp-server:latest .

# Push to registry
docker push <registry>/<name>-mcp-server:latest
```

---

## Step 2: Helm Chart Structure

Create a production-grade Helm chart:

```
mcp-server-chart/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── _helpers.tpl
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   ├── hpa.yaml
│   └── NOTES.txt
```

### Chart.yaml

```yaml
apiVersion: v2
name: mcp-server
description: MCP (Model Context Protocol) server Helm chart
type: application
version: 0.1.0
appVersion: "1.0.0"
```

### values.yaml

```yaml
# -- Server name (used in resource names)
nameOverride: ""
fullnameOverride: ""

image:
  repository: ""           # REQUIRED: e.g., ghcr.io/org/my-mcp-server
  tag: "latest"
  pullPolicy: IfNotPresent

# -- Number of replicas
replicaCount: 2

# -- MCP server configuration
mcp:
  # Transport: streamable-http (recommended for K8s)
  transport: streamable-http
  port: 3000
  # -- Health check endpoint path
  healthPath: /health

# -- Environment variables (non-sensitive)
env: {}
  # API_BASE_URL: "https://api.example.com"
  # LOG_LEVEL: "info"

# -- Sensitive environment variables (stored in Secret)
secretEnv: {}
  # API_KEY: "your-api-key"
  # AUTH_TOKEN: "your-token"

service:
  type: NodePort
  port: 3000
  # -- NodePort number (30000-32767). Leave empty for auto-assign.
  nodePort: ""

resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi

# -- Horizontal Pod Autoscaler
autoscaling:
  enabled: false
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilization: 70

# -- Liveness and readiness probes
probes:
  liveness:
    enabled: true
    initialDelaySeconds: 10
    periodSeconds: 30
    timeoutSeconds: 5
    failureThreshold: 3
  readiness:
    enabled: true
    initialDelaySeconds: 5
    periodSeconds: 10
    timeoutSeconds: 3
    failureThreshold: 3

# -- Node selector
nodeSelector: {}

# -- Tolerations
tolerations: []

# -- Affinity rules
affinity: {}
```

### templates/_helpers.tpl

```yaml
{{- define "mcp-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "mcp-server.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "mcp-server.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "mcp-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "mcp-server.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mcp-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
```

### templates/secret.yaml

```yaml
{{- if .Values.secretEnv }}
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "mcp-server.fullname" . }}
  labels:
    {{- include "mcp-server.labels" . | nindent 4 }}
type: Opaque
stringData:
  {{- range $key, $value := .Values.secretEnv }}
  {{ $key }}: {{ $value | quote }}
  {{- end }}
{{- end }}
```

### templates/configmap.yaml

```yaml
{{- if .Values.env }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "mcp-server.fullname" . }}
  labels:
    {{- include "mcp-server.labels" . | nindent 4 }}
data:
  {{- range $key, $value := .Values.env }}
  {{ $key }}: {{ $value | quote }}
  {{- end }}
{{- end }}
```

### templates/deployment.yaml

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "mcp-server.fullname" . }}
  labels:
    {{- include "mcp-server.labels" . | nindent 4 }}
spec:
  {{- if not .Values.autoscaling.enabled }}
  replicas: {{ .Values.replicaCount }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "mcp-server.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "mcp-server.selectorLabels" . | nindent 8 }}
    spec:
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: mcp-server
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: http
              containerPort: {{ .Values.mcp.port }}
              protocol: TCP
          env:
            - name: MCP_TRANSPORT
              value: {{ .Values.mcp.transport | quote }}
            - name: MCP_PORT
              value: {{ .Values.mcp.port | quote }}
          {{- if .Values.env }}
          envFrom:
            - configMapRef:
                name: {{ include "mcp-server.fullname" . }}
          {{- end }}
          {{- if .Values.secretEnv }}
            - secretRef:
                name: {{ include "mcp-server.fullname" . }}
          {{- end }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          {{- if .Values.probes.liveness.enabled }}
          livenessProbe:
            httpGet:
              path: {{ .Values.mcp.healthPath }}
              port: http
            initialDelaySeconds: {{ .Values.probes.liveness.initialDelaySeconds }}
            periodSeconds: {{ .Values.probes.liveness.periodSeconds }}
            timeoutSeconds: {{ .Values.probes.liveness.timeoutSeconds }}
            failureThreshold: {{ .Values.probes.liveness.failureThreshold }}
          {{- end }}
          {{- if .Values.probes.readiness.enabled }}
          readinessProbe:
            httpGet:
              path: {{ .Values.mcp.healthPath }}
              port: http
            initialDelaySeconds: {{ .Values.probes.readiness.initialDelaySeconds }}
            periodSeconds: {{ .Values.probes.readiness.periodSeconds }}
            timeoutSeconds: {{ .Values.probes.readiness.timeoutSeconds }}
            failureThreshold: {{ .Values.probes.readiness.failureThreshold }}
          {{- end }}
```

### templates/service.yaml

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "mcp-server.fullname" . }}
  labels:
    {{- include "mcp-server.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: http
      protocol: TCP
      name: http
      {{- if and (eq .Values.service.type "NodePort") .Values.service.nodePort }}
      nodePort: {{ .Values.service.nodePort }}
      {{- end }}
  selector:
    {{- include "mcp-server.selectorLabels" . | nindent 4 }}
```

### templates/hpa.yaml

```yaml
{{- if .Values.autoscaling.enabled }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "mcp-server.fullname" . }}
  labels:
    {{- include "mcp-server.labels" . | nindent 4 }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ include "mcp-server.fullname" . }}
  minReplicas: {{ .Values.autoscaling.minReplicas }}
  maxReplicas: {{ .Values.autoscaling.maxReplicas }}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .Values.autoscaling.targetCPUUtilization }}
{{- end }}
```

### templates/NOTES.txt

```
MCP Server "{{ include "mcp-server.fullname" . }}" deployed successfully.

{{- if eq .Values.service.type "NodePort" }}
Get the NodePort:
  export NODE_PORT=$(kubectl get svc {{ include "mcp-server.fullname" . }} -n {{ .Release.Namespace }} -o jsonpath='{.spec.ports[0].nodePort}')
  export NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
  echo "MCP Server URL: http://$NODE_IP:$NODE_PORT"

Register in GoClaw:
  register_mcp_server({
    "name": "{{ include "mcp-server.fullname" . }}",
    "transport": "streamable-http",
    "url": "http://$NODE_IP:$NODE_PORT",
    "display_name": "{{ include "mcp-server.fullname" . }}"
  })
{{- end }}
```

---

## Step 3: Deploy with Helm

### Create namespace

```bash
kubectl create namespace mcp-servers
```

### Install the chart

```bash
helm install <release-name> ./mcp-server-chart \
  --namespace mcp-servers \
  --set image.repository=<registry>/<name>-mcp-server \
  --set image.tag=latest \
  --set secretEnv.API_KEY="your-api-key"
```

### With custom values file

Create `my-server-values.yaml`:

```yaml
image:
  repository: ghcr.io/myorg/github-mcp-server
  tag: "1.2.0"

replicaCount: 3

mcp:
  transport: streamable-http
  port: 3000

env:
  LOG_LEVEL: "info"

secretEnv:
  GITHUB_TOKEN: "ghp_xxxxxxxxxxxx"

service:
  type: NodePort
  nodePort: 30080

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 8

resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: "1"
    memory: 1Gi
```

```bash
helm install github-mcp ./mcp-server-chart \
  --namespace mcp-servers \
  -f my-server-values.yaml
```

### Verify deployment

```bash
# Check pods
kubectl get pods -n mcp-servers -l app.kubernetes.io/instance=<release-name>

# Check service
kubectl get svc -n mcp-servers <release-name>-mcp-server

# Check logs
kubectl logs -n mcp-servers -l app.kubernetes.io/instance=<release-name> --tail=50

# Wait for rollout
kubectl rollout status deployment/<release-name>-mcp-server -n mcp-servers
```

---

## Step 4: Get NodePort and IP Address

After successful deployment, retrieve the connection details:

```bash
# Get NodePort
export NODE_PORT=$(kubectl get svc <release-name>-mcp-server \
  -n mcp-servers \
  -o jsonpath='{.spec.ports[0].nodePort}')

# Get Node IP (internal)
export NODE_IP=$(kubectl get nodes \
  -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# Or get external IP (cloud providers)
export NODE_IP=$(kubectl get nodes \
  -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}')

# Verify connectivity
curl -s http://$NODE_IP:$NODE_PORT/health
```

For LoadBalancer service type (cloud):

```bash
export LB_IP=$(kubectl get svc <release-name>-mcp-server \
  -n mcp-servers \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export LB_PORT=$(kubectl get svc <release-name>-mcp-server \
  -n mcp-servers \
  -o jsonpath='{.spec.ports[0].port}')
```

---

## Step 5: Register in GoClaw via register_mcp_server

**CRITICAL: This step connects the deployed K8s MCP server to GoClaw.**

After confirming the service is healthy, register it:

### NodePort registration

```
register_mcp_server({
  "name": "<service-name>",
  "transport": "streamable-http",
  "url": "http://<NODE_IP>:<NODE_PORT>",
  "display_name": "<Service Display Name>",
  "headers": {"Authorization": "Bearer <token>"},
  "timeout_sec": 30
})
```

### LoadBalancer registration

```
register_mcp_server({
  "name": "<service-name>",
  "transport": "streamable-http",
  "url": "http://<LB_IP>:<LB_PORT>",
  "display_name": "<Service Display Name>",
  "headers": {"Authorization": "Bearer <token>"},
  "timeout_sec": 30
})
```

### Concrete example

```bash
# Deployed github-mcp on NodePort 30080, node IP 10.0.1.50
```

```
register_mcp_server({
  "name": "github-mcp",
  "transport": "streamable-http",
  "url": "http://10.0.1.50:30080",
  "display_name": "GitHub MCP Server (K8s)",
  "headers": {"Authorization": "Bearer ghp_xxxxxxxxxxxx"},
  "tool_prefix": "github",
  "timeout_sec": 30
})
```

---

## Upgrade and Rollback

### Upgrade

```bash
helm upgrade <release-name> ./mcp-server-chart \
  --namespace mcp-servers \
  --set image.tag=1.3.0
```

After upgrade, GoClaw does NOT need re-registration — the URL stays the same. Only re-register if the NodePort/IP changes.

### Rollback

```bash
# List history
helm history <release-name> -n mcp-servers

# Rollback to previous
helm rollback <release-name> -n mcp-servers

# Rollback to specific revision
helm rollback <release-name> 2 -n mcp-servers
```

---

## Best Practices

### Security
- Never put secrets in `values.yaml` committed to git — use `--set secretEnv.KEY=val` or external secret managers (Vault, Sealed Secrets, External Secrets Operator)
- Use network policies to restrict MCP server traffic to GoClaw pods only
- Run containers as non-root user

### Reliability
- Set `replicaCount >= 2` for production
- Enable HPA for auto-scaling under load
- Configure proper liveness/readiness probes
- Use pod disruption budgets for zero-downtime upgrades

### Networking
- **NodePort** (30000-32767): Simplest option, good for internal/dev clusters
- **LoadBalancer**: For cloud providers (AWS ELB, GCP LB, Azure LB)
- **Ingress**: For domain-based routing with TLS termination
- Use `streamable-http` transport (not stdio) — K8s cannot expose stdio

### Resource Management
- Always set resource requests and limits
- Start conservative, scale up based on monitoring
- Use `resources.requests` for scheduling, `resources.limits` for protection

### Observability
- Implement `/health` endpoint in your MCP server for K8s probes
- Export metrics (Prometheus format) on `/metrics` if available
- Use structured logging (JSON) for log aggregation

---

## Troubleshooting

| Issue | Command | Fix |
|-------|---------|-----|
| Pod CrashLoopBackOff | `kubectl logs <pod> -n mcp-servers --previous` | Check startup errors, env vars |
| Service unreachable | `kubectl get endpoints <svc> -n mcp-servers` | Verify pods are ready |
| Wrong NodePort | `kubectl get svc -n mcp-servers -o wide` | Check port mapping |
| Image pull error | `kubectl describe pod <pod> -n mcp-servers` | Verify image name, registry auth |
| Health check failing | `kubectl exec <pod> -n mcp-servers -- curl localhost:3000/health` | Check health endpoint impl |
| register_mcp_server fails | Verify URL reachable from GoClaw host | Check network/firewall rules |
