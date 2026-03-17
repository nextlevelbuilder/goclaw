{{/*
Expand the name of the chart.
*/}}
{{- define "nilclaw.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "nilclaw.fullname" -}}
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
{{- define "nilclaw.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nilclaw.labels" -}}
helm.sh/chart: {{ include "nilclaw.chart" . }}
{{ include "nilclaw.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: nilclaw
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nilclaw.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nilclaw.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: gateway
{{- end }}

{{/*
Service account name
*/}}
{{- define "nilclaw.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- include "nilclaw.fullname" . }}
{{- end }}
{{- end }}

{{/* ─── Secret Contract ─── */}}

{{- define "nilclaw.secret.mode" -}}
{{- default "existingSecret" .Values.secret.mode -}}
{{- end }}

{{- define "nilclaw.secretName" -}}
{{- if .Values.secret.name -}}
{{- .Values.secret.name -}}
{{- else -}}
{{- printf "%s-secret" (include "nilclaw.fullname" .) -}}
{{- end -}}
{{- end }}

{{/* Key mappings */}}
{{- define "nilclaw.secret.key.postgresDsn" -}}
{{- default "postgres-dsn" .Values.secret.keys.postgresDsn -}}
{{- end }}
{{- define "nilclaw.secret.key.encryptionKey" -}}
{{- default "encryption-key" .Values.secret.keys.encryptionKey -}}
{{- end }}
{{- define "nilclaw.secret.key.gatewayToken" -}}
{{- default "gateway-token" .Values.secret.keys.gatewayToken -}}
{{- end }}
{{- define "nilclaw.secret.key.openaiApiKey" -}}
{{- default "openai-api-key" .Values.secret.keys.openaiApiKey -}}
{{- end }}
{{- define "nilclaw.secret.key.openaiBaseUrl" -}}
{{- default "openai-base-url" .Values.secret.keys.openaiBaseUrl -}}
{{- end }}
{{- define "nilclaw.secret.key.model" -}}
{{- default "model" .Values.secret.keys.model -}}
{{- end }}
{{- define "nilclaw.secret.key.provider" -}}
{{- default "provider" .Values.secret.keys.provider -}}
{{- end }}

{{/* Inline enabled check */}}
{{- define "nilclaw.secret.inline.enabled" -}}
{{- if and (eq (include "nilclaw.secret.mode" .) "existingSecret") (not .Values.secret.name) (or .Values.auth.postgresDsn .Values.auth.encryptionKey .Values.auth.openaiApiKey) -}}
true
{{- else -}}
false
{{- end -}}
{{- end }}
