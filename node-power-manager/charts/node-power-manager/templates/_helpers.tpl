{{- define "node-power-manager.name" -}}
node-power-manager
{{- end -}}

{{- define "node-power-manager.fullname" -}}
{{ include "node-power-manager.name" . }}
{{- end -}}
