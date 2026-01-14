{{/*
Expand the name of the chart.
*/}}
{{- define "optipod.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "optipod.fullname" -}}
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
{{- define "optipod.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "optipod.labels" -}}
helm.sh/chart: {{ include "optipod.chart" . }}
{{ include "optipod.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: optipod
{{- end }}

{{/*
Selector labels
*/}}
{{- define "optipod.selectorLabels" -}}
app.kubernetes.io/name: {{ include "optipod.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Webhook labels
*/}}
{{- define "optipod.webhook.labels" -}}
{{ include "optipod.labels" . }}
app.kubernetes.io/component: webhook
{{- end }}

{{/*
Webhook selector labels
*/}}
{{- define "optipod.webhook.selectorLabels" -}}
{{ include "optipod.selectorLabels" . }}
app.kubernetes.io/component: webhook
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "optipod.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "optipod.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the webhook service account to use
*/}}
{{- define "optipod.webhook.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- printf "%s-webhook" (include "optipod.fullname" .) }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Get the namespace to use
*/}}
{{- define "optipod.namespace" -}}
{{- if .Values.namespaceOverride }}
{{- .Values.namespaceOverride }}
{{- else }}
{{- .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Get the image to use
*/}}
{{- define "optipod.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Webhook service name
*/}}
{{- define "optipod.webhook.serviceName" -}}
{{- printf "%s-webhook" (include "optipod.fullname" .) }}
{{- end }}

{{/*
Webhook deployment name
*/}}
{{- define "optipod.webhook.deploymentName" -}}
{{- printf "%s-webhook" (include "optipod.fullname" .) }}
{{- end }}

{{/*
MutatingWebhookConfiguration name
*/}}
{{- define "optipod.webhook.configName" -}}
{{- printf "%s-mutating-webhook" (include "optipod.fullname" .) }}
{{- end }}

{{/*
Certificate issuer name
*/}}
{{- define "optipod.webhook.issuerName" -}}
{{- .Values.certManager.issuer.name | default (printf "%s-issuer" (include "optipod.fullname" .)) }}
{{- end }}

{{/*
Certificate secret name
*/}}
{{- define "optipod.webhook.certificateSecretName" -}}
{{- .Values.certManager.certificate.secretName | default "webhook-server-certs" }}
{{- end }}

{{/*
Certificate DNS names
*/}}
{{- define "optipod.webhook.certificateDnsNames" -}}
{{- $serviceName := include "optipod.webhook.serviceName" . }}
{{- $namespace := include "optipod.namespace" . }}
{{- if .Values.certManager.certificate.dnsNames }}
{{- .Values.certManager.certificate.dnsNames | toYaml }}
{{- else }}
- {{ $serviceName }}.{{ $namespace }}.svc
- {{ $serviceName }}.{{ $namespace }}.svc.cluster.local
{{- end }}
{{- end }}

{{/*
Check if cert-manager is installed in the cluster
Uses lookup to check for cert-manager CRDs
*/}}
{{- define "optipod.certManager.isInstalled" -}}
{{- $certManagerCRD := lookup "apiextensions.k8s.io/v1" "CustomResourceDefinition" "" "certificates.cert-manager.io" }}
{{- if $certManagerCRD }}
{{- true }}
{{- else }}
{{- false }}
{{- end }}
{{- end }}

{{/*
Determine if we should install cert-manager
*/}}
{{- define "optipod.certManager.shouldInstall" -}}
{{- $installValue := .Values.certManager.install | toString }}
{{- if or (eq $installValue "true") (eq $installValue "1") }}
{{- print "true" }}
{{- else }}
{{- print "false" }}
{{- end }}
{{- end }}

{{/*
cert-manager CA injection annotation
*/}}
{{- define "optipod.webhook.certManagerAnnotation" -}}
{{- $namespace := include "optipod.namespace" . }}
{{- $issuerName := include "optipod.webhook.issuerName" . }}
{{- printf "%s/%s" $namespace $issuerName }}
{{- end }}
