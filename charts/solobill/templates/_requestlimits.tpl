{{/*
Request limit hooks, owned by issue #82 alone. ingress.yaml and
httproute.yaml include solobill.requestLimits.annotations in their
metadata annotations, so #82 fills it here from the top level
"requestLimits" values block and edits neither of those files. Renders
nothing until then.
*/}}

{{/*
solobill.requestLimits.annotations renders annotation entries, as a YAML
map at column 0.
*/}}
{{- define "solobill.requestLimits.annotations" -}}
{{- end -}}
