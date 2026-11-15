{{/*
Expand the name of the chart.
*/}}
{{- define "solobill.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to
this (by the DNS naming spec).
*/}}
{{- define "solobill.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "solobill.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "solobill.labels" -}}
helm.sh/chart: {{ include "solobill.chart" . }}
{{ include "solobill.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: server
app.kubernetes.io/part-of: {{ include "solobill.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels. Deliberately excludes app.kubernetes.io/version: a selector
must stay stable across an image upgrade, matchLabels is immutable once a
Deployment exists.
*/}}
{{- define "solobill.selectorLabels" -}}
app.kubernetes.io/name: {{ include "solobill.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Create the name of the service account to use.
*/}}
{{- define "solobill.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "solobill.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Fully qualified name of the migration Job and its ConfigMap.
*/}}
{{- define "solobill.migration.fullname" -}}
{{- printf "%s-migrate" (include "solobill.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Build a NetworkPolicy peer list from one of networkPolicy's own scope blocks
(namespaceSelector, podSelector, ipBlock), rendered as YAML and returned as a
string. Empty when the block scopes nothing, which every caller tests for: an
empty string is falsy, so "with" skips it.

Two things about the shape this produces, both easy to get backwards:

  - namespaceSelector and podSelector go into ONE list item, so they are ANDed:
    the peer is "a pod carrying these labels, in a namespace carrying those".
    Two separate list items would be an OR instead, which silently admits more
    than it names.
  - ipBlock is its own list item, so it is ORed with the selector peer above.
    That is not a choice: the NetworkPolicy API rejects a peer that carries
    ipBlock alongside a selector. A scope block that sets both therefore means
    "the selected pods, or this CIDR", never "both at once".

Each selector value is a plain label map (a matchLabels block), not the full
Kubernetes LabelSelector shape: this chart has no use for matchExpressions.

Callers pass the block through "default dict", so a block set to null in values
renders as no peer rather than failing on a nil map.
*/}}
{{- define "solobill.networkPolicy.peers" -}}
{{- $scope := . -}}
{{- $peers := list -}}
{{- $selectorPeer := dict -}}
{{- with $scope.namespaceSelector -}}
{{- $_ := set $selectorPeer "namespaceSelector" (dict "matchLabels" .) -}}
{{- end -}}
{{- with $scope.podSelector -}}
{{- $_ := set $selectorPeer "podSelector" (dict "matchLabels" .) -}}
{{- end -}}
{{- if $selectorPeer -}}
{{- $peers = append $peers $selectorPeer -}}
{{- end -}}
{{- with $scope.ipBlock -}}
{{- if .cidr -}}
{{- $ipBlock := dict "cidr" .cidr -}}
{{- with .except -}}
{{- $_ := set $ipBlock "except" . -}}
{{- end -}}
{{- $peers = append $peers (dict "ipBlock" $ipBlock) -}}
{{- end -}}
{{- end -}}
{{- if $peers -}}
{{- toYaml $peers -}}
{{- end -}}
{{- end -}}

{{/*
The TCP port a URL names: its own "port" query parameter when it carries one,
otherwise its explicit host-embedded port, otherwise the default port of its
scheme. Empty for a scheme this template does not know, which every caller
treats as "cannot tell, do not guess".

Used only to compare a configured URL's port against the port the matching
NetworkPolicy egress rule allows, so a mismatch fails the render instead of
the connection. The query parameter takes priority over the host-embedded
port, not the other way around, because that is what pgx actually does:
pgconn's own URL parser documents an explicit "?port=" as overriding the
netloc-derived port (jackc/pgx/v5 pgconn/parse_url.go), matching libpq's own
URI format, which both the postgres and postgresql schemes follow. A URL
such as "postgres://pg.svc/db?port=5433" therefore connects on 5433, not 5432,
even though its host carries no port of its own; reading only the host would
miss that and let this guard pass a mismatch through unnoticed. sprig's
urlParse exposes "host" (host and port together) and "hostname", never a port
of its own, so the host-embedded port is read off "host" with a regex. That
also keeps an IPv6 literal correct: "[::1]:5432" matches, "[::1]" does not.
The query string is split by hand ("&" then "=") rather than through a full
query-string parser: this template has no access to one, and a port is never
percent-encoded or multi-valued in practice, so the simple split is exact for
what this helper is asked to answer. The last "port" pair wins when the query
string repeats it, matching pgconn's own last-occurrence-wins behaviour.
*/}}
{{- define "solobill.urlPort" -}}
{{- $parsed := urlParse . -}}
{{- $scheme := $parsed.scheme | default "" -}}
{{- $queryPort := "" -}}
{{- range splitList "&" ($parsed.query | default "") -}}
{{- $kv := splitList "=" . -}}
{{- if and (ge (len $kv) 2) (eq (index $kv 0) "port") -}}
{{- $queryPort = index $kv 1 -}}
{{- end -}}
{{- end -}}
{{- $explicit := regexFind ":[0-9]+$" ($parsed.host | default "") -}}
{{- if $queryPort -}}
{{- $queryPort -}}
{{- else if $explicit -}}
{{- trimPrefix ":" $explicit -}}
{{- else if eq $scheme "https" -}}
443
{{- else if eq $scheme "http" -}}
80
{{- else if or (eq $scheme "postgres") (eq $scheme "postgresql") -}}
5432
{{- end -}}
{{- end -}}

{{/*
Migration Job labels, mirroring solobill.labels with its own component.
*/}}
{{- define "solobill.migration.labels" -}}
helm.sh/chart: {{ include "solobill.chart" . }}
app.kubernetes.io/name: {{ include "solobill.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: migration
app.kubernetes.io/part-of: {{ include "solobill.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
