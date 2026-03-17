{{/*
Expand the name of the chart.
*/}}
{{- define "keycloak.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "keycloak.fullname" -}}
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
{{- define "keycloak.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "keycloak.labels" -}}
helm.sh/chart: {{ include "keycloak.chart" . }}
{{ include "keycloak.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "keycloak.selectorLabels" -}}
app.kubernetes.io/name: {{ include "keycloak.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Secret name
*/}}
{{- define "keycloak.secretName" -}}
{{- default (printf "%s-secret" (include "keycloak.fullname" .)) .Values.secret.name }}
{{- end }}

{{/*
Secret key mappings
*/}}
{{- define "keycloak.secretKey.adminPassword" -}}
{{- .Values.secret.keys.adminPassword }}
{{- end }}

{{- define "keycloak.secretKey.dbPassword" -}}
{{- .Values.secret.keys.dbPassword }}
{{- end }}

{{- define "keycloak.secretKey.postgresPassword" -}}
{{- .Values.secret.keys.postgresPassword }}
{{- end }}

{{/*
ConfigMap name
*/}}
{{- define "keycloak.configMapName" -}}
{{- include "keycloak.fullname" . }}-config
{{- end }}

{{/*
Headless service FQDN for JGroups DNS discovery
*/}}
{{- define "keycloak.headlessServiceFQDN" -}}
{{- printf "%s-headless.%s.svc.cluster.local" (include "keycloak.fullname" .) .Release.Namespace }}
{{- end }}

{{/*
Database JDBC URL
*/}}
{{- define "keycloak.dbUrl" -}}
{{- printf "jdbc:postgresql://%s:%s/%s" .Values.database.host .Values.database.port .Values.database.name }}
{{- end }}
