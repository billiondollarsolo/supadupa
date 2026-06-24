{{- define "supadupa.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "supadupa.fullname" -}}
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

{{- define "supadupa.labels" -}}
app.kubernetes.io/name: {{ include "supadupa.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "supadupa.selectorLabels" -}}
app.kubernetes.io/name: {{ include "supadupa.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "supadupa.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "supadupa.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "supadupa.controlPlaneServiceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{- .Values.serviceAccount.name -}}
{{- else if .Values.serviceAccount.controlPlaneName -}}
{{- .Values.serviceAccount.controlPlaneName -}}
{{- else if .Values.serviceAccount.create -}}
{{- include "supadupa.fullname" . -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.controlPlaneName -}}
{{- end -}}
{{- end -}}

{{- define "supadupa.operatorServiceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{- .Values.serviceAccount.name -}}
{{- else if .Values.serviceAccount.operatorName -}}
{{- .Values.serviceAccount.operatorName -}}
{{- else if .Values.serviceAccount.create -}}
{{- printf "%s-operator" (include "supadupa.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.operatorName -}}
{{- end -}}
{{- end -}}

{{- define "supadupa.secretName" -}}
{{- default (printf "%s-secrets" (include "supadupa.fullname" .)) .Values.secrets.existingSecret -}}
{{- end -}}

{{- define "supadupa.metaDbHost" -}}
{{- printf "%s-meta-db" (include "supadupa.fullname" .) -}}
{{- end -}}

{{- define "supadupa.runtimeNamespace" -}}
{{- if .Values.operator.runtimeNamespaceOverride -}}
{{- .Values.operator.runtimeNamespaceOverride -}}
{{- else if .Values.controlPlane.kubernetesNamespaceOverride -}}
{{- .Values.controlPlane.kubernetesNamespaceOverride -}}
{{- else -}}
{{- .Release.Namespace -}}
{{- end -}}
{{- end -}}

{{/*
Render a string map as a comma-separated key=value list (sorted by key) for
passing label selectors to the operator via an environment variable, e.g.
{team: platform, tier: edge} -> "team=platform,tier=edge".
*/}}
{{- define "supadupa.kvCsv" -}}
{{- $pairs := list -}}
{{- range $k, $v := . -}}
{{- $pairs = append $pairs (printf "%s=%s" $k $v) -}}
{{- end -}}
{{- join "," (sortAlpha $pairs) -}}
{{- end -}}
