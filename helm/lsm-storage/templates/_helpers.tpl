{{/*
Expand the name of the chart.
*/}}
{{- define "lsm-storage.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "lsm-storage.fullname" -}}
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
{{- define "lsm-storage.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "lsm-storage.labels" -}}
helm.sh/chart: {{ include "lsm-storage.chart" . }}
{{ include "lsm-storage.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "lsm-storage.selectorLabels" -}}
app.kubernetes.io/name: {{ include "lsm-storage.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Backend-specific selector labels
*/}}
{{- define "lsm-storage.backendSelectorLabels" -}}
app.kubernetes.io/name: {{ include "lsm-storage.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
Frontend-specific selector labels
*/}}
{{- define "lsm-storage.frontendSelectorLabels" -}}
app.kubernetes.io/name: {{ include "lsm-storage.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "lsm-storage.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "lsm-storage.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Return the name of the Secret containing auth credentials.
If auth.existingSecret is set, use that; otherwise use the chart-generated secret name.
*/}}
{{- define "lsm-storage.secretName" -}}
{{- if .Values.auth.existingSecret }}
{{- .Values.auth.existingSecret }}
{{- else }}
{{- printf "%s-auth" (include "lsm-storage.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Return the name of the ConfigMap for engine configuration.
*/}}
{{- define "lsm-storage.configMapName" -}}
{{- printf "%s-config" (include "lsm-storage.fullname" .) }}
{{- end }}

{{/*
Return the name of the PVC for data storage.
*/}}
{{- define "lsm-storage.pvcName" -}}
{{- printf "%s-data" (include "lsm-storage.fullname" .) }}
{{- end }}

{{/*
Return the backend service name and port as a URL for the frontend.
*/}}
{{- define "lsm-storage.backendServiceName" -}}
{{- printf "%s-backend" (include "lsm-storage.fullname" .) }}
{{- end }}

{{/*
Return the frontend service name.
*/}}
{{- define "lsm-storage.frontendServiceName" -}}
{{- printf "%s-frontend" (include "lsm-storage.fullname" .) }}
{{- end }}
