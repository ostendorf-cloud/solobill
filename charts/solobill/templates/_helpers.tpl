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
Common labels of the API part. The API keeps the chart's plain name and
fullname, and the selector it had as the single image, so an upgrade never
has to replace its Deployment's immutable selector. The frontend part has
its own name below.
*/}}
{{- define "solobill.labels" -}}
helm.sh/chart: {{ include "solobill.chart" . }}
{{ include "solobill.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: api
app.kubernetes.io/part-of: {{ include "solobill.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Labels of an object that belongs to no single part: the Ingress and the
HTTPRoute, which route to the API and the frontend alike.
*/}}
{{- define "solobill.release.labels" -}}
helm.sh/chart: {{ include "solobill.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: {{ include "solobill.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Frontend fullname: the release's fullname with "-frontend" appended.
*/}}
{{- define "solobill.frontend.fullname" -}}
{{- printf "%s-frontend" (include "solobill.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Frontend selector labels. Its own app.kubernetes.io/name: the API's selector
is name and instance, so a frontend pod sharing the name would be selected
by the API Service and the API NetworkPolicy too. No version, for the same
reason as solobill.selectorLabels.
*/}}
{{- define "solobill.frontend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "solobill.name" . }}-frontend
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Frontend common labels.
*/}}
{{- define "solobill.frontend.labels" -}}
helm.sh/chart: {{ include "solobill.chart" . }}
{{ include "solobill.frontend.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: frontend
app.kubernetes.io/part-of: {{ include "solobill.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Frontend ServiceAccount name.
*/}}
{{- define "solobill.frontend.serviceAccountName" -}}
{{- if .Values.frontend.serviceAccount.create -}}
{{- default (include "solobill.frontend.fullname" .) .Values.frontend.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.frontend.serviceAccount.name -}}
{{- end -}}
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

The argument is a dict with the keys "url" and "key", where "key" is the values
key the URL came from (for example "config.databaseUrl"). Every error message
names that key. There is no plain string form: every caller passes the dict, so
a message cannot fall back to a vague "a URL".

A port that is not plain digits fails the render here, naming the values key,
the part of the URL that holds it and the fix, rather than either reaching a caller's own
mismatch guard with nothing to compare against sensibly ("abc" is never equal
to a numeric NetworkPolicy port, so that guard already rejected it, but its
own message, built for a numeric mismatch, told a reader to set the
NetworkPolicy's port to "abc") or crashing inside urlParse itself: Go's
net/url.Parse, behind urlParse, refuses a URL whose host carries a
non-numeric port at all ("invalid port \"...\" after host"), aborting the
whole render with its own raw Go error before this helper, or any caller,
gets a chance to say anything readable. The host-embedded case is therefore
extracted and checked by hand, matching net/url's own authority grammar
closely enough for the URLs this chart actually takes (an optional
"user:pass@", an optional IPv6 literal in brackets, then an optional
":port"), so it fails readably before urlParse ever sees it; the query-
parameter case is checked the same way, after the safe call to urlParse that
reads the rest of the URL.

None of the errors this helper raises prints the URL, any part of it or the bad
value: the URL may carry userinfo, a database password included, and an error
text reaches CI and "helm upgrade" logs.

A URL that net/url rejects outright aborts the render inside sprig's urlParse,
and that error prints the whole URL. This helper therefore checks the raw string
before urlParse and fails naming only the values key. The check is an allowlist,
not a list of known bad characters: the URL may hold only the RFC 3986 characters
(A-Za-z0-9 - . _ ~, the reserved set : / ? # [ ] @ ! $ & ' ( ) * + , ; =, and "%"
followed by two hex digits). Whitespace, control characters (DEL included),
^ { } | < > ` " \ and anything outside ASCII fail. Three shapes the allowlist
cannot see are checked on top of it: a scheme part that is not a scheme (":x",
"a!b:x"), a scheme-relative URL ("//host"), a "[" or "]" in the user info, a
bracketed host that is not a valid IPv6 literal (RFC 4291 text forms, an IPv4 tail
allowed, an optional "%25" zone, an unclosed "[" fails), and a "%" in a host other
than that zone. The host port is read after the FIRST colon of the host, as net/url
does for http and https, so a second colon in the host or the port is a non-numeric
port. Consequence: a host with a percent-encoded non-ASCII name, an IPv4 address in
brackets, a scheme-relative URL and a "postgres://" URL with a second colon in the
host are rejected too, none of them is a usable URL for this chart.

Tested with helm and the fake token s3cr3tTOKEN over 2610 renders of the three keys
(punctuation, control characters, percent escapes, IPv6 forms, scheme-relative and
opaque URLs, long values): none prints the URL. That is a test, not a proof: a shape
net/url rejects that none of those renders hit would still abort in urlParse and
print the URL. A URL whose password holds an unencoded "/", "?", "#" or "@" is not a
valid URL either and is out of scope: the text after such a character is read as
part of the host or the port, and this helper makes no promise for it.
*/}}
{{- define "solobill.urlPort" -}}
{{- $url := .url -}}
{{- $key := .key -}}
{{- $invalid := not (regexMatch `^[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=-]*$` (regexReplaceAll "%[0-9A-Fa-f]{2}" $url "")) -}}
{{- $head := regexFind `^[^:/?#]*:` $url -}}
{{- if and $head (not (regexMatch `^[A-Za-z][A-Za-z0-9+.-]*:$` $head)) -}}
{{- $invalid = true -}}
{{- end -}}
{{- if hasPrefix "//" $url -}}
{{- $invalid = true -}}
{{- end -}}
{{- $hex := "[0-9A-Fa-f]{1,4}" -}}
{{- $v4 := `((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])` -}}
{{- $ip6 := `(HEX:){7}HEX|(HEX:){1,7}:|(HEX:){1,6}:HEX|(HEX:){1,5}(:HEX){1,2}|(HEX:){1,4}(:HEX){1,3}|(HEX:){1,3}(:HEX){1,4}|(HEX:){1,2}(:HEX){1,5}|HEX:(:HEX){1,6}|:((:HEX){1,7}|:)|(HEX:){6}IPV4|::([Ff]{4}(:0{1,4})?:)?IPV4|(HEX:){1,5}:IPV4` | replace "HEX" $hex | replace "IPV4" $v4 -}}
{{- if contains "://" $url -}}
{{- $pcAuthority := regexReplaceAll "^[a-zA-Z][a-zA-Z0-9+.-]*://([^/?#]*).*$" $url "$1" -}}
{{- if and (contains "@" $pcAuthority) (regexMatch `[\[\]]` (regexReplaceAll "@[^@]*$" $pcAuthority "")) -}}
{{- $invalid = true -}}
{{- end -}}
{{- $pcHost := regexReplaceAll "^.*@" $pcAuthority "" -}}
{{- if regexMatch `[\[\]]` $pcHost -}}
{{- if not (regexMatch (print `^\[(` $ip6 `)(%25[A-Za-z0-9._~-]+)?\][^\[\]]*$`) $pcHost) -}}
{{- $invalid = true -}}
{{- end -}}
{{- else if contains "%" $pcHost -}}
{{- $invalid = true -}}
{{- end -}}
{{- end -}}
{{- if $invalid -}}
{{ fail (printf "%s is not a valid URL: it holds a character or shape a URL may not have: whitespace, a control character, a percent sign not followed by two hex digits, a non-ASCII character, one of ^ { } | < > ` the double quote or the backslash, a misplaced bracket, a percent escape in the host or a missing scheme. Fix it. The value is not printed here because a URL may carry a password." $key) }}
{{- end -}}
{{- if contains "://" $url -}}
{{- $authority := regexReplaceAll "^[a-zA-Z][a-zA-Z0-9+.-]*://([^/?#]*).*$" $url "$1" -}}
{{- $hostport := regexReplaceAll "^.*@" $authority "" -}}
{{- $hostPortCandidate := "" -}}
{{- if contains "]" $hostport -}}
{{- $afterBracket := regexReplaceAll "^.*\\]" $hostport "" -}}
{{- $hostPortCandidate = trimPrefix ":" $afterBracket -}}
{{- else if contains ":" $hostport -}}
{{- $hostPortCandidate = regexReplaceAll "^[^:]*:" $hostport "" -}}
{{- end -}}
{{- if and $hostPortCandidate (not (regexMatch "^[0-9]+$" $hostPortCandidate)) -}}
{{ fail (printf "%s has a non-numeric port in its host part: a URL's port must be digits only. Fix the port after the host's \":\", or remove it so this chart falls back to the scheme's own default port." $key) }}
{{- end -}}
{{- end -}}
{{- $parsed := urlParse $url -}}
{{- $scheme := $parsed.scheme | default "" -}}
{{- $queryPort := "" -}}
{{- range splitList "&" ($parsed.query | default "") -}}
{{- $kv := splitList "=" . -}}
{{- if and (ge (len $kv) 2) (eq (index $kv 0) "port") -}}
{{- $queryPort = index $kv 1 -}}
{{- end -}}
{{- end -}}
{{- if and $queryPort (not (regexMatch "^[0-9]+$" $queryPort)) -}}
{{ fail (printf "%s has a non-numeric port in its \"port\" query parameter: a URL's port must be digits only. Fix or remove that query parameter." $key) }}
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
