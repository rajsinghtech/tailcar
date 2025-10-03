{{/*
Expand the name of the chart.
*/}}
{{- define "tailcar.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "tailcar.fullname" -}}
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
{{- define "tailcar.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "tailcar.labels" -}}
helm.sh/chart: {{ include "tailcar.chart" . }}
{{ include "tailcar.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "tailcar.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tailcar.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "tailcar.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "tailcar.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Leader election namespace
*/}}
{{- define "tailcar.leaderElectionNamespace" -}}
{{- if .Values.leaderElection.namespace }}
{{- .Values.leaderElection.namespace }}
{{- else }}
{{- .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Watch namespace
*/}}
{{- define "tailcar.watchNamespace" -}}
{{- if .Values.watchNamespace }}
{{- .Values.watchNamespace }}
{{- else }}
{{- "" }}
{{- end }}
{{- end }}

{{/*
Image reference
*/}}
{{- define "tailcar.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Webhook service name
*/}}
{{- define "tailcar.webhookServiceName" -}}
{{- printf "%s-webhook-service" (include "tailcar.fullname" .) }}
{{- end }}

{{/*
Webhook certificate secret name
*/}}
{{- define "tailcar.webhookCertSecretName" -}}
{{- printf "%s-webhook-cert" (include "tailcar.fullname" .) }}
{{- end }}
