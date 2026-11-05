{{/* Chart name for labels. */}}
{{- define "service.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{- define "service.fullname" -}}
{{- .Release.Name -}}-{{- .Chart.Name -}}
{{- end -}}

{{- define "service.labels" -}}
app.kubernetes.io/name: {{ include "service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "service.selectorLabels" -}}
app.kubernetes.io/name: {{ include "service.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "service.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end -}}

{{/*
Pod security context satisfying OpenShift restricted-v2 (SPEC 1.8): no
fixed UID (the cluster assigns one), non-root, no escalation, every
capability dropped, read-only root filesystem.
*/}}
{{- define "service.podSecurityContext" -}}
runAsNonRoot: true
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "service.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
{{- end -}}

{{/*
terminationGracePeriodSeconds: drain + shutdown + closer, plus a margin.
Values are plain whole seconds written as "5s" (matching config/config.yaml).
*/}}
{{- define "service.terminationGracePeriodSeconds" -}}
{{- $drain := .Values.lifecycle.drainDelay | trimSuffix "s" | int -}}
{{- $shutdown := .Values.lifecycle.shutdownTimeout | trimSuffix "s" | int -}}
{{- $closer := .Values.lifecycle.closerTimeout | trimSuffix "s" | int -}}
{{- add (add (add $drain $shutdown) $closer) 10 -}}
{{- end -}}

{{/* envFrom: the rendered ConfigMap, plus the two referenced Secrets when set. */}}
{{- define "service.envFrom" -}}
- configMapRef:
    name: {{ include "service.fullname" . }}
{{- if .Values.secret.existingSecret }}
- secretRef:
    name: {{ .Values.secret.existingSecret }}
{{- end }}
{{- end -}}

{{/*
The migrate Job is a pre-install/pre-upgrade hook, which Helm runs before
regular manifests (the ConfigMap included) exist. It inlines .Values.config
as `env` instead of an envFrom configMapRef, so it never depends on hook
ordering; the referenced Secret (not rendered by this chart) still works as
a plain envFrom.
*/}}
{{- define "service.hookEnv" -}}
{{- range $key, $value := .Values.config }}
- name: {{ $key }}
  value: {{ $value | quote }}
{{- end }}
- name: DRAIN_DELAY
  value: {{ .Values.lifecycle.drainDelay | quote }}
{{- end -}}

{{- define "service.hookEnvFrom" -}}
{{- if .Values.secret.existingSecret }}
- secretRef:
    name: {{ .Values.secret.existingSecret }}
{{- end }}
{{- end -}}
