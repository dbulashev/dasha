{{/*
Expand the name of the chart.
*/}}
{{- define "dasha.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "dasha.fullname" -}}
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
{{- define "dasha.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "dasha.labels" -}}
helm.sh/chart: {{ include "dasha.chart" . }}
{{ include "dasha.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "dasha.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dasha.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Allow the release namespace to be overridden
*/}}
{{- define "dasha.namespace" -}}
{{- if .Values.namespaceOverride -}}
{{- .Values.namespaceOverride -}}
{{- else -}}
{{- .Release.Namespace -}}
{{- end -}}
{{- end -}}

{{/*
Backend image
*/}}
{{- define "dasha.backendImage" -}}
{{ .Values.images.backend.repository }}:{{ .Values.images.backend.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Frontend image
*/}}
{{- define "dasha.frontendImage" -}}
{{ .Values.images.frontend.repository }}:{{ .Values.images.frontend.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
Name of the secret containing password env vars
*/}}
{{- define "dasha.envSecretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else }}
{{- include "dasha.fullname" . }}-env
{{- end }}
{{- end }}
