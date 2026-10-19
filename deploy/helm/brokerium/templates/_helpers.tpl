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
