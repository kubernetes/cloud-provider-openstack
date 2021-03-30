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
auth-url                      = {{ .Values.cloudConfig.global.authUrl }}
os-endpoint-type              = {{ .Values.cloudConfig.global.osEndpointType }}
ca-file                       = {{ .Values.cloudConfig.global.caFile }}
cert-file                     = {{ .Values.cloudConfig.global.certFile }}
key-file                      = {{ .Values.cloudConfig.global.keyFile }}
username                      = {{ .Values.cloudConfig.global.username }}
password                      = {{ .Values.cloudConfig.global.password }}
region                        = {{ .Values.cloudConfig.global.region }}
domain-id                     = {{ .Values.cloudConfig.global.domainId }}
domain-name                   = {{ .Values.cloudConfig.global.domainName }}
tenant-id                     = {{ .Values.cloudConfig.global.tenantId }}
tenant-name                   = {{ .Values.cloudConfig.global.tenantName }}
tenant-domain-id              = {{ .Values.cloudConfig.global.tenantDomainId }}
tenant-domain-name            = {{ .Values.cloudConfig.global.tenantDomainName }}
user-domain-id                = {{ .Values.cloudConfig.global.userDomainId }}
user-domain-name              = {{ .Values.cloudConfig.global.userDomainName }}
trust-id                      = {{ .Values.cloudConfig.global.trustId }}
trustee-id                    = {{ .Values.cloudConfig.global.trusteeId }}
use-clouds                    = false
application-credential-id     = {{ .Values.cloudConfig.global.applicationCredentialId }}
application-credential-name   = {{ .Values.cloudConfig.global.applicationCredentialName }}
application-credential-secret = {{ .Values.cloudConfig.global.applicationCredentialSecret }}
tls-insecure                  = {{ .Values.cloudConfig.global.tlsInsecure }}

[Networking]
ipv6-support-disabled = {{ .Values.cloudConfig.networking.ipv6SupportDisabled }}
public-network-name   = {{ .Values.cloudConfig.networking.publicNetworkName }}
internal-network-name = {{ .Values.cloudConfig.networking.internalNetworkName }}

[LoadBalancer]
use-octavia             = {{ .Values.cloudConfig.loadbalancer.useOctavia  }}
floating-network-id     = {{ .Values.cloudConfig.loadbalancer.floatingNetworkId }}
floating-subnet-id      = {{ .Values.cloudConfig.loadbalancer.floatingSubnetId }}
floating-subnet         = {{ .Values.cloudConfig.loadbalancer.floatingSubnet }}
floating-subnet-tags    = {{ .Values.cloudConfig.loadbalancer.floatingSubnetTags }}
lb-method               = {{ .Values.cloudConfig.loadbalancer.lbMethod }}
lb-provider             = {{ .Values.cloudConfig.loadbalancer.lbProvider }}
lb-version              = {{ .Values.cloudConfig.loadbalancer.lbVersion }}
subnet-id               = {{ .Values.cloudConfig.loadbalancer.subnetId }}
network-id              = {{ .Values.cloudConfig.loadbalancer.networkId }}
manage-security-groups  = {{ .Values.cloudConfig.loadbalancer.manageSecurityGroups }}
create-monitor          = {{ .Values.cloudConfig.loadbalancer.createMonitor }}
monitor-delay           = {{ .Values.cloudConfig.loadbalancer.monitorDelay }}
monitor-max-retries     = {{ .Values.cloudConfig.loadbalancer.monitorMaxRetries }}
monitor-timeout         = {{ .Values.cloudConfig.loadbalancer.monitorTimeout }}
internal-lb             = {{ .Values.cloudConfig.loadbalancer.internalLb }}
cascade-delete          = {{ .Values.cloudConfig.loadbalancer.cascadeDelete }}
flavor-id               = {{ .Values.cloudConfig.loadbalancer.flavorId }}
availability-zone       = {{ .Values.cloudConfig.loadbalancer.availabilityZone }}
enable-ingress-hostname = {{ .Values.cloudConfig.loadbalancer.enableIngressHostname }}

[Metadata]
search-order = {{ .Values.cloudConfig.metadata.searchOrder }}
{{- end -}}
