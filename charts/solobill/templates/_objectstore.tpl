{{/*
Object store hooks, owned by issue #75 alone. deployment.yaml and
networkpolicy.yaml include these two templates at a fixed place, so #75
fills them here and edits neither of those files. #75 also owns the top
level "objectStore" values block, its property in values.schema.json and
templates/files-verify-cronjob.yaml.

The bucket itself (versioning, object lock, the user and its key) is not
created by this chart: it comes from the infrastructure repository and
OpenBao. This chart only takes the endpoint, the bucket name and a Secret
reference. No credential is ever a value here.
*/}}

{{/*
solobill.objectStore.secretName is the Secret the S3 key pair is read
from: objectStore.existingSecret.name, or existingSecret.name when empty.
*/}}
{{- define "solobill.objectStore.secretName" -}}
{{- .Values.objectStore.existingSecret.name | default .Values.existingSecret.name -}}
{{- end -}}

{{/*
solobill.objectStore.env renders the S3_* container env entries, as a
YAML list at column 0. S3_OBJECT_LOCK and S3_REQUIRE_VERSIONING are always
rendered, so GET /api/v1/status reports the configured requirement even
without a store. Everything else only when objectStore.endpoint is set:
without it the process still starts, and the file routes answer 503
object_store_unavailable.
*/}}
{{- define "solobill.objectStore.env" -}}
{{- $os := .Values.objectStore -}}
- name: S3_OBJECT_LOCK
  value: {{ $os.objectLock | quote }}
- name: S3_REQUIRE_VERSIONING
  value: {{ $os.requireVersioning | quote }}
{{- if $os.endpoint }}
{{- /*
The shape of the endpoint is checked here and not by a "pattern" in values.schema.json:
helm prints the rejected value in a schema pattern error, user info included, and an
error text reaches CI and "helm upgrade" logs. This message names only the key.
Scheme and host only: no user info, path, query or fragment, one trailing "/" allowed.
*/ -}}
{{- if not (regexMatch "^https?://[^/?#@]+/?$" $os.endpoint) }}
{{ fail "objectStore.endpoint must be an http or https URL with a host, scheme and host only: no user info, path, query or fragment. The value is not printed here because a URL may carry a password." }}
{{- end }}
- name: S3_ENDPOINT
  value: {{ $os.endpoint | quote }}
- name: S3_BUCKET
  value: {{ required "objectStore.bucket is required when objectStore.endpoint is set" $os.bucket | quote }}
- name: S3_REGION
  value: {{ $os.region | quote }}
- name: S3_USE_PATH_STYLE
  value: {{ $os.usePathStyle | quote }}
{{- if $os.existingSecret.accessKeyIdKey }}
- name: S3_ACCESS_KEY_ID
  valueFrom:
    secretKeyRef:
      name: {{ include "solobill.objectStore.secretName" . }}
      key: {{ $os.existingSecret.accessKeyIdKey }}
{{- end }}
{{- if $os.existingSecret.secretAccessKeyKey }}
- name: S3_SECRET_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "solobill.objectStore.secretName" . }}
      key: {{ $os.existingSecret.secretAccessKeyKey }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
solobill.objectStore.egress renders the NetworkPolicy egress rule for the
S3 endpoint, as a YAML list at column 0. The port is read from
objectStore.endpoint (its explicit port, else 80 or 443), so the policy and
the URL cannot disagree. The destination is objectStore.egress's own
selectors; left empty, the rule admits any destination on that port, the
same story as PostgreSQL in networkpolicy.yaml.
*/}}
{{- define "solobill.objectStore.egress" -}}
{{- $os := .Values.objectStore -}}
{{- if $os.endpoint -}}
{{- $port := include "solobill.urlPort" (dict "key" "objectStore.endpoint" "url" $os.endpoint) -}}
{{- $peers := include "solobill.networkPolicy.peers" ($os.egress | default dict) -}}
# The S3 compatible object store (objectStore.endpoint).
- ports:
    - protocol: TCP
      port: {{ required "objectStore.endpoint names no scheme this chart knows a port for: use an http or https URL." $port }}
  {{- with $peers }}
  to:
    {{- . | nindent 4 }}
  {{- end }}
{{- end -}}
{{- end -}}
