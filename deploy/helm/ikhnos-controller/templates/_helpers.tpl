{{- define "controller.name" -}}ikhnos-controller{{- end -}}
{{- define "controller.labels" -}}
app.kubernetes.io/name: ikhnos-controller
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: ikhnos
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}
{{- define "controller.image" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end -}}
{{- end -}}
{{- define "controller.pullPolicy" -}}
{{- if .Values.image.pullPolicy -}}
{{- .Values.image.pullPolicy -}}
{{- else if eq (.Values.image.tag | default .Chart.AppVersion | toString) "edge" -}}
Always
{{- else -}}
IfNotPresent
{{- end -}}
{{- end -}}
{{/* The names of the agent chart's cluster-scoped objects, which are fixed by that chart: a role per tier and its binding in a namespace,
     and the roles of the telemetry's collectors and of Kepler (named whether or not allowTelemetry is on, so that the controller can still
     remove them when telemetry is taken off). */}}
{{- define "controller.agentClusterNames" -}}
{{- $ns := .Release.Namespace -}}
{{- range $t := until 3 }}
- ikhnos-agent-t{{ $t }}
- ikhnos-agent-t{{ $t }}-{{ $ns }}
{{- end }}
{{- range $r := list "telemetry" "kepler" }}
- ikhnos-agent-{{ $r }}
- ikhnos-agent-{{ $r }}-{{ $ns }}
{{- end }}
{{- end -}}
{{- define "controller.validate" -}}
{{- $t := .Values.maxAccessTier | int -}}
{{- if not (has $t (list 0 1 2)) -}}{{- fail "maxAccessTier must be 0, 1 or 2" -}}{{- end -}}
{{- end -}}
{{/* The spec of the IkhnosAgent the bootstrap values ask for, as JSON in the resource's own field names; empty when no server address is
     set. The controller makes the resource from it. Keys are sorted by toJson, so the same values are always the same text. */}}
{{- define "controller.bootstrapSpec" -}}
{{- $b := .Values.bootstrap | default dict -}}
{{- if $b.server -}}
{{- if not $b.caPin -}}{{- fail "bootstrap.server is set without bootstrap.caPin: the agent must know which CA to trust (the Connect page shows it)" -}}{{- end -}}
{{- $spec := dict "server" (dict "address" $b.server "caPin" $b.caPin) "enrollment" (dict "tokenSecretRef" (dict "name" "ikhnos-enrollment")) -}}
{{- if kindIs "invalid" $b.accessTier -}}{{- $_ := set $spec "accessTier" (.Values.maxAccessTier | int) -}}{{- else -}}{{- $_ := set $spec "accessTier" ($b.accessTier | int) -}}{{- end -}}
{{- $c := dict -}}
{{- range $k := list "nodeProbe" "flowObserver" "measurements" -}}{{- if index ($b.collectors | default dict) $k -}}{{- $_ := set $c $k true -}}{{- end -}}{{- end -}}
{{- if $c -}}{{- $_ := set $spec "collectors" $c -}}{{- end -}}
{{- $sc := dict -}}
{{- range $k := list "namespaces" "exclude" "selector" -}}{{- $v := index ($b.scope | default dict) $k -}}{{- if $v -}}{{- $_ := set $sc $k $v -}}{{- end -}}{{- end -}}
{{- if $sc -}}{{- $_ := set $spec "scope" $sc -}}{{- end -}}
{{- if $b.purgeIdentityOnDelete -}}{{- $_ := set $spec "purgeIdentityOnDelete" true -}}{{- end -}}
{{- $im := dict -}}
{{- range $k := list "repository" "tag" "digest" -}}{{- $v := index ($b.image | default dict) $k -}}{{- if $v -}}{{- $_ := set $im $k $v -}}{{- end -}}{{- end -}}
{{- if $im -}}{{- $_ := set $spec "image" $im -}}{{- end -}}
{{- $spec | toJson -}}
{{- end -}}
{{- end -}}
