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
MCP connector image
*/}}
{{- define "dasha.mcpImage" -}}
{{ .Values.images.mcp.repository }}:{{ .Values.images.mcp.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
In-cluster Dasha API base URL the MCP connector calls.
Defaults to the backend Service (short in-namespace name, like the frontend, so it
works regardless of the cluster DNS domain); override with mcp.dashaUrl.
*/}}
{{- define "dasha.mcpDashaURL" -}}
{{- if .Values.mcp.dashaUrl -}}
{{- .Values.mcp.dashaUrl -}}
{{- else -}}
{{- printf "http://%s-backend:%v" (include "dasha.fullname" .) .Values.backend.port -}}
{{- end -}}
{{- end -}}

{{/*
Whether TLS is enabled at the ingress / gateway layer.
Returns "true" or "false" (string — use with eq).
*/}}
{{- define "dasha.tlsEnabled" -}}
{{- $ingressTLS := and .Values.ingress.enabled .Values.ingress.tls.enabled -}}
{{- $gwTLS := and .Values.gatewayAPI.enabled .Values.gatewayAPI.tls.enabled -}}
{{- ternary "true" "false" (or $ingressTLS $gwTLS) -}}
{{- end -}}

{{/*
Fail if both Ingress and Gateway API are enabled — they are mutually exclusive.
Invoke from a guaranteed-rendered template (e.g. configmap.yaml).
*/}}
{{- define "dasha.validateTrafficMode" -}}
{{- if and .Values.ingress.enabled .Values.gatewayAPI.enabled -}}
{{- fail "ingress.enabled and gatewayAPI.enabled are mutually exclusive — set only one to true" -}}
{{- end -}}
{{- end -}}

{{/*
Validate Gateway API configuration. When the chart owns the Gateway and it lives in a
different namespace than the release (e.g. istio-system), allowedRoutes.namespaces.from
must NOT be "Same" or HTTPRoute from the release namespace cannot attach to the Gateway.
With createGateway=false the listeners belong to someone else, so the checks are on the
parent reference instead: it must exist, and the redirect route must not share a listener
with the main route.
*/}}
{{- define "dasha.validateGatewayAPI" -}}
{{- if .Values.gatewayAPI.enabled -}}
{{- if .Values.gatewayAPI.createGateway -}}
{{- $gwNs := .Values.gatewayAPI.gatewayNamespace -}}
{{- $releaseNs := include "dasha.namespace" . -}}
{{- $from := dig "namespaces" "from" "Same" .Values.gatewayAPI.allowedRoutes -}}
{{- if and $gwNs (ne $gwNs $releaseNs) (eq $from "Same") -}}
{{- fail (printf "gatewayAPI.gatewayNamespace=%q differs from release namespace %q, but gatewayAPI.allowedRoutes.namespaces.from=\"Same\" — HTTPRoute cannot attach. Set allowedRoutes.namespaces.from to \"All\" or \"Selector\"." $gwNs $releaseNs) -}}
{{- end -}}
{{- else -}}
{{- $eg := .Values.gatewayAPI.existingGateway | default dict -}}
{{- if not (dig "name" "" $eg) -}}
{{- fail "gatewayAPI.createGateway=false requires gatewayAPI.existingGateway.name — the HTTPRoute has no Gateway to attach to" -}}
{{- end -}}
{{- $section := dig "sectionName" "" $eg -}}
{{- $redirectSection := dig "redirectSectionName" "" $eg -}}
{{- if and .Values.gatewayAPI.tls.enabled .Values.gatewayAPI.tls.redirect $redirectSection -}}
{{- if not $section -}}
{{- fail "gatewayAPI.existingGateway.redirectSectionName is set while existingGateway.sectionName is empty — the main HTTPRoute would attach to the HTTP listener as well and take precedence over the redirect route. Name the HTTPS listener in existingGateway.sectionName." -}}
{{- end -}}
{{- if eq $section $redirectSection -}}
{{- fail (printf "gatewayAPI.existingGateway.sectionName and existingGateway.redirectSectionName are both %q — both HTTPRoutes would attach to the same listener and the redirect would loop onto itself. Point redirectSectionName at the HTTP listener and sectionName at the HTTPS one." $redirectSection) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Gateway referenced by the HTTPRoutes: the chart's own Gateway, or an existing one
when createGateway=false.
*/}}
{{- define "dasha.routeGatewayName" -}}
{{- if .Values.gatewayAPI.createGateway -}}
{{- include "dasha.fullname" . -}}
{{- else -}}
{{- dig "name" "" (.Values.gatewayAPI.existingGateway | default dict) -}}
{{- end -}}
{{- end -}}

{{- define "dasha.routeGatewayNamespace" -}}
{{- if .Values.gatewayAPI.createGateway -}}
{{- include "dasha.gatewayNamespace" . -}}
{{- else -}}
{{- dig "namespace" "" (.Values.gatewayAPI.existingGateway | default dict) | default (include "dasha.gatewayNamespace" .) -}}
{{- end -}}
{{- end -}}

{{/*
Listener the main HTTPRoute attaches to. Empty for an existing Gateway unless set —
its listener names are not ours to guess.
*/}}
{{- define "dasha.routeSectionName" -}}
{{- if .Values.gatewayAPI.createGateway -}}
{{- ternary "https" "http" .Values.gatewayAPI.tls.enabled -}}
{{- else -}}
{{- dig "sectionName" "" (.Values.gatewayAPI.existingGateway | default dict) -}}
{{- end -}}
{{- end -}}

{{/*
HTTP listener the redirect HTTPRoute attaches to. Empty means no redirect route.
*/}}
{{- define "dasha.redirectSectionName" -}}
{{- if .Values.gatewayAPI.createGateway -}}
{{- "http" -}}
{{- else -}}
{{- dig "redirectSectionName" "" (.Values.gatewayAPI.existingGateway | default dict) -}}
{{- end -}}
{{- end -}}

{{/*
TLS secret name for the Gateway. Defaults to {fullname}-tls when not set explicitly.
*/}}
{{- define "dasha.gatewayTLSSecretName" -}}
{{- .Values.gatewayAPI.tls.certificateRef.name | default (printf "%s-tls" (include "dasha.fullname" .)) -}}
{{- end -}}

{{/*
Gateway resource namespace. Defaults to release namespace when not set.
*/}}
{{- define "dasha.gatewayNamespace" -}}
{{- .Values.gatewayAPI.gatewayNamespace | default (include "dasha.namespace" .) -}}
{{- end -}}

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
