{{/*
Expand the name of the chart.
*/}}
{{- define "postgresql.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "postgresql.fullname" -}}
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
{{- define "postgresql.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "postgresql.labels" -}}
helm.sh/chart: {{ include "postgresql.chart" . }}
{{ include "postgresql.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "postgresql.selectorLabels" -}}
app.kubernetes.io/name: {{ include "postgresql.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Master fullname
*/}}
{{- define "postgresql.master.fullname" -}}
{{ include "postgresql.fullname" . }}-master
{{- end }}

{{/*
Slave fullname
*/}}
{{- define "postgresql.slave.fullname" -}}
{{ include "postgresql.fullname" . }}-slave
{{- end }}

{{/*
HAProxy fullname
*/}}
{{- define "postgresql.haproxy.fullname" -}}
{{ include "postgresql.fullname" . }}-haproxy
{{- end }}

{{/*
Secret mode (existingSecret|externalSecret)
*/}}
{{- define "postgresql.secret.mode" -}}
{{- default "existingSecret" .Values.secret.mode -}}
{{- end }}

{{/*
Secret name consumed by workloads
*/}}
{{- define "postgresql.secretName" -}}
{{- if .Values.secret.name -}}
{{- .Values.secret.name -}}
{{- else -}}
{{- printf "%s-secret" (include "postgresql.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Canonical secret key mapping for postgres password
*/}}
{{- define "postgresql.secret.key.postgresPassword" -}}
{{- if .Values.secret.keys.postgresPassword -}}
{{- .Values.secret.keys.postgresPassword -}}
{{- else if .Values.secret.keys.password -}}
{{- .Values.secret.keys.password -}}
{{- else -}}
postgres-password
{{- end -}}
{{- end }}

{{/*
Canonical secret key mapping for replication password
*/}}
{{- define "postgresql.secret.key.replicationPassword" -}}
{{- if .Values.secret.keys.replicationPassword -}}
{{- .Values.secret.keys.replicationPassword -}}
{{- else if .Values.secret.keys.replication -}}
{{- .Values.secret.keys.replication -}}
{{- else -}}
replication-password
{{- end -}}
{{- end }}

{{/*
Legacy inline secret values (deprecated)
*/}}
{{- define "postgresql.secret.value.postgresPassword" -}}
{{- default "" .Values.auth.postgresPassword -}}
{{- end }}
{{- define "postgresql.secret.value.replicationPassword" -}}
{{- default "" .Values.auth.replicationPassword -}}
{{- end }}

{{/*
Render legacy inline Secret only for backward compatibility
*/}}
{{- define "postgresql.secret.inline.enabled" -}}
{{- if and (eq (include "postgresql.secret.mode" .) "existingSecret") (not .Values.secret.name) (or .Values.auth.postgresPassword .Values.auth.replicationPassword) -}}
true
{{- else -}}
false
{{- end -}}
{{- end }}

{{/*
Master config name
*/}}
{{- define "postgresql.master.configName" -}}
{{ include "postgresql.fullname" . }}-master-config
{{- end }}

{{/*
Slave config name
*/}}
{{- define "postgresql.slave.configName" -}}
{{ include "postgresql.fullname" . }}-slave-config
{{- end }}

{{/*
HAProxy config name
*/}}
{{- define "postgresql.haproxy.configName" -}}
{{ include "postgresql.fullname" . }}-haproxy-config
{{- end }}

{{/*
Master selector labels
*/}}
{{- define "postgresql.master.selectorLabels" -}}
app: {{ include "postgresql.name" . }}
role: master
{{- end }}

{{/*
Slave selector labels
*/}}
{{- define "postgresql.slave.selectorLabels" -}}
app: {{ include "postgresql.name" . }}
role: slave
{{- end }}

{{/*
HAProxy selector labels
*/}}
{{- define "postgresql.haproxy.selectorLabels" -}}
app: {{ include "postgresql.haproxy.fullname" . }}
{{- end }}
