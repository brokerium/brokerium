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

{{- define "brokerium.agentEndpoint" -}}
{{- default (printf "%s.%s.svc:%d" (include "brokerium.fullname" .) .Release.Namespace (int .Values.service.grpc)) .Values.agents.endpoint -}}
{{- end -}}
