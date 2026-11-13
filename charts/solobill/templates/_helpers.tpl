{{/*
Expand the name of the chart.
*/}}
{{- define "solobill.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to
this (by the DNS naming spec).
*/}}
{{- define "solobill.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "solobill.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "solobill.labels" -}}
helm.sh/chart: {{ include "solobill.chart" . }}
{{ include "solobill.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: server
app.kubernetes.io/part-of: {{ include "solobill.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels. Deliberately excludes app.kubernetes.io/version: a selector
must stay stable across an image upgrade, matchLabels is immutable once a
Deployment exists.
*/}}
{{- define "solobill.selectorLabels" -}}
app.kubernetes.io/name: {{ include "solobill.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Create the name of the service account to use.
*/}}
{{- define "solobill.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "solobill.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Fully qualified name of the migration Job and its ConfigMap.
*/}}
{{- define "solobill.migration.fullname" -}}
{{- printf "%s-migrate" (include "solobill.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Migration Job labels, mirroring solobill.labels with its own component.
*/}}
{{- define "solobill.migration.labels" -}}
helm.sh/chart: {{ include "solobill.chart" . }}
app.kubernetes.io/name: {{ include "solobill.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: migration
app.kubernetes.io/part-of: {{ include "solobill.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
