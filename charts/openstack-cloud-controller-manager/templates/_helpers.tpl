{{/*
Expand the name of the chart.
*/}}
{{- define "occm.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "occm.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "occm.common.matchLabels" -}}
app: {{ template "occm.name" . }}
release: {{ .Release.Name }}
{{- end -}}

{{- define "occm.common.metaLabels" -}}
chart: {{ template "occm.chart" . }}
heritage: {{ .Release.Service }}
{{- end -}}

{{- define "occm.controllermanager.matchLabels" -}}
component: controllermanager
{{ include "occm.common.matchLabels" . }}
{{- end -}}

{{- define "occm.controllermanager.labels" -}}
{{ include "occm.controllermanager.matchLabels" . }}
{{ include "occm.common.metaLabels" . }}
{{ if .Values.podLabels }}
{{- toYaml .Values.podLabels }}
{{- end }}
{{- end -}}

{{/*
Common annotations and pod annotations
*/}}
{{- define "occm.controllermanager.annotations" -}}
{{- if .Values.commonAnnotations }}
{{- toYaml .Values.commonAnnotations }}
{{- end }}
{{ if .Values.podAnnotations }}
{{- toYaml .Values.podAnnotations }}
{{- end }}
{{- end -}}


{{/*
Create cloud-config makro.
*/}}
{{- define "cloudConfig" -}}
[Global]
{{- range $key, $value := .Values.cloudConfig.global }}
{{ $key }} = {{ $value | quote }}
{{- end }}

[Networking]
{{- range $key, $value := .Values.cloudConfig.networking }}
{{ $key }} = {{ $value | quote }}
{{- end }}

[LoadBalancer]
{{- range $key, $value := .Values.cloudConfig.loadBalancer }}
{{ $key }} = {{ $value | quote }}
{{- end }}

[BlockStorage]
{{- range $key, $value := .Values.cloudConfig.blockStorage }}
{{ $key }} = {{ $value | quote }}
{{- end }}

[Metadata]
{{- range $key, $value := .Values.cloudConfig.metadata }}
{{ $key }} = {{ $value | quote }}
{{- end }}

[Route]
{{- range $key, $value := .Values.cloudConfig.route }}
{{ $key }} = {{ $value | quote }}
{{- end }}
{{- end }}

{{/*
Generate string of enabled controllers. Might have a trailing comma (,) which needs to be trimmed.
*/}}
{{- define "occm.enabledControllers" }}
{{- range .Values.enabledControllers -}}{{ . }},{{- end -}}
{{- end }}
