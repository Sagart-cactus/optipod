{{/* Validate the credential choice without including its value in errors. */}}
{{- define "optipod-console.agentSecretName" -}}
{{- $secret := .Values.agent.secret | default dict -}}
{{- if $secret.create -}}
  {{- if .Values.agent.existingSecret -}}
    {{- fail "choose either agent.existingSecret or agent.secret.create, not both" -}}
  {{- end -}}
  {{- printf "%s-agent-auth" .Release.Name -}}
{{- else -}}
  {{- if $secret.token -}}
    {{- fail "agent.secret.token requires agent.secret.create=true" -}}
  {{- end -}}
  {{- required "agent.existingSecret is required unless agent.secret.create=true" .Values.agent.existingSecret -}}
{{- end -}}
{{- end -}}

{{/* Normalize trailing newlines from --set-file, just as the agent does. */}}
{{- define "optipod-console.agentToken" -}}
{{- $secret := .Values.agent.secret | default dict -}}
{{- $token := trim (default "" $secret.token) -}}
{{- if or (lt (len $token) 32) (regexMatch "[[:space:]]" $token) -}}
  {{- fail "agent.secret.token must contain at least 32 characters and no internal whitespace" -}}
{{- end -}}
{{- $token -}}
{{- end -}}
