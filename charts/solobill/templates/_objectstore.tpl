{{/*
Object store hooks, owned by issue #75 alone. deployment.yaml and
networkpolicy.yaml include these two templates at a fixed place, so #75
fills them here and edits neither of those files. Both render nothing
until then. #75 also owns the top level "objectStore" values block and its
property in values.schema.json.
*/}}

{{/*
solobill.objectStore.env renders the S3_* container env entries, as a
YAML list at column 0.
*/}}
{{- define "solobill.objectStore.env" -}}
{{- end -}}

{{/*
solobill.objectStore.egress renders the NetworkPolicy egress rule for the
S3 endpoint, as a YAML list at column 0.
*/}}
{{- define "solobill.objectStore.egress" -}}
{{- end -}}
