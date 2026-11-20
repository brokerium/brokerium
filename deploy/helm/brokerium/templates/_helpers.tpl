{{/* SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0 */}}

{{- define "brokerium.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "brokerium.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "brokerium.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "brokerium.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "brokerium.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "brokerium.secretName" -}}
{{- default (include "brokerium.fullname" .) .Values.existingSecret -}}
{{- end -}}

{{- define "brokerium.image" -}}
{{- $repository := ternary "bvaliev/brokerium-server-enterprise" "bvaliev/brokerium-server" (eq .Values.edition "enterprise") -}}
{{- printf "%s:%s" (.Values.image.repository | default $repository) (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}

{{/* Secret with the license key in the entry license.secretKey; empty without a key. */}}
{{- define "brokerium.licenseSecret" -}}
{{- if .Values.license.existingSecret -}}
{{- .Values.license.existingSecret -}}
{{- else if .Values.license.key -}}
{{- if .Values.existingSecret -}}
{{- fail "license.key is stored in the chart's Secret, which existingSecret replaces; use license.existingSecret" -}}
{{- end -}}
{{- include "brokerium.fullname" . -}}
{{- end -}}
{{- end -}}

{{/* Secret and entry with the token of GET /metrics. */}}
{{- define "brokerium.metricsTokenSecret" -}}
{{- if .Values.metrics.token.existingSecret -}}
{{- .Values.metrics.token.existingSecret -}}
{{- else if .Values.existingSecret -}}
{{- fail "metrics.token.existingSecret is required with existingSecret, which replaces the chart's Secret" -}}
{{- else -}}
{{- include "brokerium.fullname" . -}}
{{- end -}}
{{- end -}}

{{- define "brokerium.metricsTokenKey" -}}
{{- ternary .Values.metrics.token.key "metrics-token" (ne .Values.metrics.token.existingSecret "") -}}
{{- end -}}

{{/* Checks the monitoring values, which only the Enterprise image reads. */}}
{{- define "brokerium.checkMonitoring" -}}
{{- with .Values.monitoring.prometheus -}}
{{- $credentials := or .username .passwordSecret.name .tokenSecret.name .caSecret.name -}}
{{- if and $credentials (not .url) -}}
{{- fail "monitoring.prometheus credentials need monitoring.prometheus.url" -}}
{{- end -}}
{{- if and .tokenSecret.name (or .username .passwordSecret.name) -}}
{{- fail "monitoring.prometheus.tokenSecret (bearer) cannot be combined with username and passwordSecret (basic)" -}}
{{- end -}}
{{- if and .passwordSecret.name (not .username) -}}
{{- fail "monitoring.prometheus.passwordSecret needs monitoring.prometheus.username" -}}
{{- end -}}
{{- end -}}
{{- if and (ne .Values.edition "enterprise") (or .Values.monitoring.prometheus.url .Values.metrics.enabled) -}}
{{- fail "monitoring.prometheus and metrics are features of Brokerium Enterprise: set edition to enterprise" -}}
{{- end -}}
{{- end -}}

{{- define "brokerium.lbHostname" -}}
{{- if .Values.loadBalancer.enabled }}{{ .Values.loadBalancer.hostname }}{{ end -}}
{{- end -}}

{{- define "brokerium.publicURL" -}}
{{- $lb := include "brokerium.lbHostname" . -}}
{{- if .Values.publicURL -}}
{{- .Values.publicURL -}}
{{- else if .Values.gateway.http.enabled -}}
{{- printf "https://%s" .Values.gateway.http.hostname -}}
{{- else if and $lb (has "http" .Values.loadBalancer.ports) -}}
{{- printf "http://%s:%d" $lb (int .Values.service.http) -}}
{{- else -}}
http://localhost:8080
{{- end -}}
{{- end -}}

{{- define "brokerium.agentEndpoint" -}}
{{- $lb := include "brokerium.lbHostname" . -}}
{{- if .Values.agents.endpoint -}}
{{- .Values.agents.endpoint -}}
{{- else if .Values.gateway.agents.enabled -}}
{{- printf "%s:%d" .Values.gateway.agents.hostname (int .Values.gateway.agents.port) -}}
{{- else if and $lb (has "grpc" .Values.loadBalancer.ports) -}}
{{- printf "%s:%d" $lb (int .Values.service.grpc) -}}
{{- else -}}
{{- printf "%s.%s.svc:%d" (include "brokerium.fullname" .) .Release.Namespace (int .Values.service.grpc) -}}
{{- end -}}
{{- end -}}

{{/* Names in the agent gateway certificate: agents check them against the address they dial. */}}
{{- define "brokerium.agentServerNames" -}}
{{- $name := include "brokerium.fullname" . -}}
{{- $names := list $name (printf "%s.%s.svc" $name .Release.Namespace) (include "brokerium.agentEndpoint" . | splitList ":" | first) -}}
{{- if .Values.gateway.agents.enabled }}{{ $names = append $names .Values.gateway.agents.hostname }}{{ end -}}
{{- with include "brokerium.lbHostname" . }}{{ $names = append $names . }}{{ end -}}
{{- concat $names .Values.agents.serverNames | uniq | join "," -}}
{{- end -}}

{{/* parentRefs of a route: the listener of the created Gateway or gateway.parentRefs. */}}
{{- define "brokerium.parentRefs" -}}
{{- if .root.Values.gateway.create -}}
- name: {{ include "brokerium.fullname" .root }}
  sectionName: {{ .listener }}
{{- else if .root.Values.gateway.parentRefs -}}
{{- toYaml .root.Values.gateway.parentRefs -}}
{{- else -}}
{{- fail "gateway.parentRefs is required when gateway.create is false" -}}
{{- end -}}
{{- end -}}
