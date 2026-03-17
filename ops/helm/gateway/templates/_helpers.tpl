{{/*
Expand the name of the chart.
*/}}
{{- define "cloudflared.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "cloudflared.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "cloudflared.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "cloudflared.labels" -}}
helm.sh/chart: {{ include "cloudflared.chart" . }}
{{ include "cloudflared.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: nilclaw
{{- end }}

{{/*
Selector labels
*/}}
{{- define "cloudflared.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cloudflared.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: ingress
{{- end }}

{{/*
Service account name
*/}}
{{- define "cloudflared.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- include "cloudflared.fullname" . }}
{{- end }}
{{- end }}

{{/* ─── Secret Contract (mirrors infrastructure pattern) ─── */}}

{{/*
Secret mode
*/}}
{{- define "cloudflared.secret.mode" -}}
{{- default "existingSecret" .Values.secret.mode -}}
{{- end }}

{{/*
Secret name consumed by workloads
*/}}
{{- define "cloudflared.secretName" -}}
{{- if .Values.secret.name -}}
{{- .Values.secret.name -}}
{{- else -}}
{{- printf "%s-secret" (include "cloudflared.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Canonical secret key mapping
*/}}
{{- define "cloudflared.secret.key.tunnelToken" -}}
{{- default "tunnel-token" .Values.secret.keys.tunnelToken -}}
{{- end }}

{{/*
Legacy inline secret value (deprecated)
*/}}
{{- define "cloudflared.secret.value.tunnelToken" -}}
{{- default "" .Values.auth.tunnelToken -}}
{{- end }}

{{/*
Render legacy inline Secret only for backward compatibility
*/}}
{{- define "cloudflared.secret.inline.enabled" -}}
{{- if and (eq (include "cloudflared.secret.mode" .) "existingSecret") (not .Values.secret.name) .Values.auth.tunnelToken -}}
true
{{- else -}}
false
{{- end -}}
{{- end }}
