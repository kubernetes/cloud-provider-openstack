{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "cinder-csi.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "cinder-csi.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "cinder-csi.labels" -}}
app.kubernetes.io/name: {{ include "cinder-csi.name" . }}
helm.sh/chart: {{ include "cinder-csi.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}



{{/*
Create unified labels for cinder-csi components
*/}}
{{- define "cinder-csi.common.matchLabels" -}}
app.kubernetes.io/name: {{ template "cinder-csi.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "cinder-csi.common.metaLabels" -}}
helm.sh/chart: {{ template "cinder-csi.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- if .Values.extraLabels }}
{{ toYaml .Values.extraLabels -}}
{{- end }}
{{- end -}}

{{- define "cinder-csi.controllerplugin.matchLabels" -}}
app.kubernetes.io/component: controllerplugin
{{ include "cinder-csi.common.matchLabels" . }}
{{- end -}}

{{- define "cinder-csi.controllerplugin.labels" -}}
{{ include "cinder-csi.controllerplugin.matchLabels" . }}
{{ include "cinder-csi.common.metaLabels" . }}
{{- end -}}

{{- define "cinder-csi.controllerplugin.podLabels" -}}
{{ include "cinder-csi.controllerplugin.labels" . }}
{{ if .Values.csi.plugin.controllerPlugin.podLabels }}
{{- toYaml .Values.csi.plugin.controllerPlugin.podLabels }}
{{- end }}
{{- end -}}

{{- define "cinder-csi.nodeplugin.matchLabels" -}}
app.kubernetes.io/component: nodeplugin
{{ include "cinder-csi.common.matchLabels" . }}
{{- end -}}

{{- define "cinder-csi.nodeplugin.labels" -}}
{{ include "cinder-csi.nodeplugin.matchLabels" . }}
{{ include "cinder-csi.common.metaLabels" . }}
{{- end -}}

{{- define "cinder-csi.nodeplugin.podLabels" -}}
{{ include "cinder-csi.nodeplugin.labels" . }}
{{ if .Values.csi.plugin.nodePlugin.podLabels }}
{{- toYaml .Values.csi.plugin.nodePlugin.podLabels }}
{{- end }}
{{- end -}}

{{/*
Common annotations
*/}}
{{- define "cinder-csi.annotations" -}}
{{- if .Values.commonAnnotations }}
{{- toYaml .Values.commonAnnotations }}
{{- end }}
{{- end -}}


{{/*
Create unified annotations for cinder-csi components
*/}}
{{- define "cinder-csi.controllerplugin.podAnnotations" -}}
{{ include "cinder-csi.annotations" . }}
{{ if .Values.csi.plugin.controllerPlugin.podAnnotations }}
{{- toYaml .Values.csi.plugin.controllerPlugin.podAnnotations }}
{{- end }}
{{- end -}}

{{- define "cinder-csi.nodeplugin.podAnnotations" -}}
{{ include "cinder-csi.annotations" . }}
{{ if .Values.csi.plugin.nodePlugin.podAnnotations }}
{{- toYaml .Values.csi.plugin.nodePlugin.podAnnotations }}
{{- end }}
{{- end -}}
