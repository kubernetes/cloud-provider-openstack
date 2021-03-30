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

{{/*
Common labels and app labels
*/}}
{{- define "occm.labels" -}}
app.kubernetes.io/name: {{ include "occm.name" . }}
helm.sh/chart: {{ include "occm.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
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
{{- end -}}

{{/*
Create cloud-config makro.
*/}}
{{- define "cloudConfig" -}}
[Global]
auth-url    = {{ .Values.cloudConfig.global.authUrl }}
username    = {{ .Values.cloudConfig.global.username }}
password    = {{ .Values.cloudConfig.global.password }}
tenant-name = {{ .Values.cloudConfig.global.tenantName }}
domain-name = {{ .Values.cloudConfig.global.domainName }}
region      = {{ .Values.cloudConfig.global.region }}

[LoadBalancer]
subnet-id           = {{ .Values.cloudConfig.loadbalancer.subnetId }}
floating-network-id = {{ .Values.cloudConfig.loadbalancer.floatingNetworkId }}
create-monitor      = {{ .Values.cloudConfig.loadbalancer.createMonitor }}
monitor-delay       = {{ .Values.cloudConfig.loadbalancer.monitorDelay }}
monitor-timeout     = {{ .Values.cloudConfig.loadbalancer.monitorTimeout }}
monitor-max-retries = {{ .Values.cloudConfig.loadbalancer.monitorMaxRetries }}
use-octavia         = {{ .Values.cloudConfig.loadbalancer.useOctavia  }}
lb-provider         = {{ .Values.cloudConfig.loadbalancer.lbProvider  }}

[Metadata]
search-order = {{ .Values.cloudConfig.metadata.searchOrder }}
{{- end -}}
