{{/*
Expand the name of the chart.
*/}}
{{- define "portal.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "portal.fullname" -}}
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
{{- define "portal.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "portal.labels" -}}
helm.sh/chart: {{ include "portal.chart" . }}
{{ include "portal.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: nilclaw
{{- end }}

{{/*
Selector labels
*/}}
{{- define "portal.selectorLabels" -}}
app.kubernetes.io/name: {{ include "portal.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Service account name
*/}}
{{- define "portal.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- include "portal.fullname" . }}
{{- end }}
{{- end }}

{{/* ─── Secret Contract (mirrors infrastructure pattern) ─── */}}

{{/*
Secret mode
*/}}
{{- define "portal.secret.mode" -}}
{{- default "existingSecret" .Values.secret.mode -}}
{{- end }}

{{/*
Secret name consumed by workloads
*/}}
{{- define "portal.secretName" -}}
{{- if .Values.secret.name -}}
{{- .Values.secret.name -}}
{{- else -}}
{{- printf "%s-secret" (include "portal.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Canonical secret key mappings
*/}}
{{- define "portal.secret.key.keycloakClientSecret" -}}
{{- default "keycloak-client-secret" .Values.secret.keys.keycloakClientSecret -}}
{{- end }}
{{- define "portal.secret.key.keycloakRealm" -}}
{{- default "keycloak-realm" .Values.secret.keys.keycloakRealm -}}
{{- end }}

{{/*
Legacy inline secret values (deprecated)
*/}}
{{- define "portal.secret.value.keycloakClientSecret" -}}
{{- default "" .Values.auth.keycloakClientSecret -}}
{{- end }}
{{- define "portal.secret.value.keycloakRealm" -}}
{{- default "" .Values.auth.keycloakRealm -}}
{{- end }}

{{/*
Render legacy inline Secret only for backward compatibility
*/}}
{{- define "portal.secret.inline.enabled" -}}
{{- if and (eq (include "portal.secret.mode" .) "existingSecret") (not .Values.secret.name) (or .Values.auth.keycloakClientSecret .Values.auth.keycloakRealm) -}}
true
{{- else -}}
false
{{- end -}}
{{- end }}
