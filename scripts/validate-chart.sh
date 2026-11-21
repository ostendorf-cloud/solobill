#!/usr/bin/env bash
# Validates charts/solobill: a must-succeed set for its default values and every documented
# non-default combination, and a must-fail set proving every fail and required guard in its
# templates actually rejects the values it is meant to reject. CI and a local run share the
# exact same logic. The must-succeed combinations below mirror
# charts/solobill/tests/*_test.yaml: default, OIDC enabled, ingress enabled, httpRoute
# enabled, NetworkPolicy fully scoped, NetworkPolicy with its egress half off.
#
# Takes exactly one mode argument:
#   lint        - helm lint over the must-succeed set, then helm template over the must-fail
#                 set, one job per check (Taskfile's lint:helm / CI's helm-lint job)
#   kubeconform - helm template piped into kubeconform, its own job (CI's kubeconform job)
# kept as two modes of one script, not two near-duplicate scripts, so the combinations
# above live in exactly one place.
#
# Mode "lint" also runs a must-fail set (the guard_* arrays below): every render-time
# "fail" guard in charts/solobill/templates, each isolated by satisfying every other
# guard and violating exactly one, run through "helm template" and asserted to fail
# naming that guard. "helm lint" cannot do this job itself: it calls the same funcMap
# fail the templates do, but only logs it at INFO and still exits 0, so a must-fail set
# built on "helm lint" alone would prove nothing about whether a guard still fires.
# charts/solobill/tests/*_test.yaml's own helm-unittest "failedTemplate" assertions
# check the identical guards independently; the two have to agree before either can be
# trusted alone to catch a guard quietly dropped out of both. Two guards have no
# must-fail case here, each for its own reason:
#   - templates/migration-configmap.yaml's "charts/solobill/migrations is empty" fires on
#     the chart's own migrations directory being empty, not on a --set value, so no
#     combination of --set flags isolates it.
#   - templates/deployment.yaml's "replicaCount above 1" is refused by values.schema.json
#     first, and only fires with --skip-schema-validation, which this loop does not pass.
#     Its helm-unittest suite covers it.
# A case that feeds a URL carries the fake token s3cr3tTOKEN as its password, and the loop
# fails when that token shows up in the error text: no message may print the URL.
#
# kubeconform resolves its Kubernetes schemas over the network with its normal default
# behaviour (no local schema clone, no relaxed TLS verification): a CI runner with normal
# internet access reaches raw.githubusercontent.com directly with no further setup needed.
# A second -schema-location, after the default one, adds the community CRDs-catalog
# (https://github.com/datreeio/CRDs-catalog): the default schema set carries no Gateway
# API schemas, and the httpRoute-enabled combination below renders an HTTPRoute.
# kubeconform tries each -schema-location in order and falls through to the next on a
# miss, so every other, non-CRD kind still resolves against the default location first.
set -euo pipefail

mode="${1:?usage: validate-chart.sh <lint|kubeconform>}"

case "$mode" in
  lint|kubeconform) ;;
  *)
    echo "validate-chart: unknown mode '${mode}', expected 'lint' or 'kubeconform'" >&2
    exit 1
    ;;
esac

chart="charts/solobill"

# The values every combination needs, must-fail cases included: existingSecret.name,
# config.databaseUrl and config.databaseUser have no usable default and each carries its own
# fail guard, and migrationJob.databaseUser is required whenever the migration Job is enabled
# and must differ from config.databaseUser (see charts/solobill/README.md's "One role, or
# two"). Every template below renders the Deployment, which holds all four guards, so a
# combination missing any of them would fail for that reason rather than its own.
baseline="existingSecret.name=solobill-secrets"
baseline="${baseline},config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require"
baseline="${baseline},config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"

# OIDC is off exactly while config.oidc.issuerUrl is empty, and the other three fields plus
# the client secret key are required together with it. The issuer names no explicit port, so
# templates/networkpolicy.yaml reads it as 443, matching networkPolicy.egress.oidc.port's own
# default.
oidc_valid="existingSecret.oidcClientSecretKey=OIDC_CLIENT_SECRET"
oidc_valid="${oidc_valid},config.oidc.issuerUrl=https://idp.example.com,config.oidc.clientId=solobill"
oidc_valid="${oidc_valid},config.oidc.redirectUrl=https://solobill.example.com/api/v1/auth/oidc/callback"
oidc_valid="${oidc_valid},config.oidc.defaultTenantSlug=acme"

# The two external routing combinations, each valid on its own: ingress enabled with its own
# required config.trustedProxies guard satisfied, httpRoute enabled with both of its own
# required parentRefs fields set and config.trustedProxies set as well.
ingress_valid="config.trustedProxies[0]=10.0.0.0/8,ingress.enabled=true,ingress.className=nginx"
ingress_valid="${ingress_valid},ingress.hosts[0].host=solobill.example.org"
ingress_valid="${ingress_valid},ingress.hosts[0].paths[0].path=/,ingress.hosts[0].paths[0].pathType=Prefix"
httproute_valid="config.trustedProxies[0]=10.0.0.0/8,httpRoute.enabled=true,httpRoute.hostnames[0]=solobill.example.org"
httproute_valid="${httproute_valid},httpRoute.parentRefs.name=shared-gateway,httpRoute.parentRefs.namespace=gateway-system"

# Passkeys on, with the relying party and its origin (issue #86).
passkey_valid="config.passkey.rpId=solobill.example.org,config.passkey.origins[0]=https://solobill.example.org"

# The NetworkPolicy renders on the default values already (networkPolicy.enabled defaults to
# true), so this combination is the one where every optional scope block is actually set: an
# admin peer, so the admin-port ingress rule renders at all, a public peer, an in-cluster
# PostgreSQL destination and an off-cluster one through an ipBlock in the same rule.
networkpolicy_scoped="networkPolicy.ingress.admin.namespaceSelector.kubernetes\\.io/metadata\\.name=monitoring"
networkpolicy_scoped="${networkpolicy_scoped},networkPolicy.ingress.admin.podSelector.app\\.kubernetes\\.io/name=prometheus"
networkpolicy_scoped="${networkpolicy_scoped},networkPolicy.ingress.public.namespaceSelector.kubernetes\\.io/metadata\\.name=ingress-nginx"
networkpolicy_scoped="${networkpolicy_scoped},networkPolicy.egress.postgres.namespaceSelector.kubernetes\\.io/metadata\\.name=postgres"
networkpolicy_scoped="${networkpolicy_scoped},networkPolicy.egress.postgres.ipBlock.cidr=203.0.113.7/32"

names=(
  "default values"
  "oidc enabled"
  "ingress enabled"
  "httpRoute enabled"
  "networkPolicy fully scoped"
  "networkPolicy with the egress half off"
  "passkeys enabled behind an ingress"
)

# Each entry is one combination's --set arguments, matching the corresponding
# charts/solobill/tests/*_test.yaml suite's "set:" block, always layered on top of baseline
# above.
sets=(
  "${baseline}"
  "${baseline},${oidc_valid}"
  "${baseline},${ingress_valid}"
  "${baseline},${httproute_valid}"
  "${baseline},${oidc_valid},${networkpolicy_scoped}"
  "${baseline},networkPolicy.egress.enabled=false"
  "${baseline},${ingress_valid},${passkey_valid}"
)

# kubeconform's default schema location carries no Gateway API schemas. This second
# location is only consulted for a kind the first one misses, HTTPRoute included; see
# this file's own header comment.
gateway_api_schema_location='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'

for i in "${!names[@]}"; do
  name="${names[$i]}"
  set_args="${sets[$i]}"

  echo "==> validating combination (${mode}): ${name}"

  case "$mode" in
    lint)
      if ! helm lint "${chart}" --set "${set_args}"; then
        echo "validate-chart: helm lint failed for combination: ${name}" >&2
        exit 1
      fi
      ;;
    kubeconform)
      if ! helm template "${chart}" --set "${set_args}" \
        | kubeconform -strict -summary \
          -schema-location default \
          -schema-location "${gateway_api_schema_location}"; then
        echo "validate-chart: kubeconform failed for combination: ${name}" >&2
        exit 1
      fi
      ;;
  esac
done

echo "all combinations valid (${mode})"

if [ "$mode" = "lint" ]; then
  # Each entry below is one guard's own --set arguments: every other guard's
  # requirement is satisfied, and exactly one is violated. That isolation is what
  # makes the assertion below mean something - if the guard under test were
  # deleted, this same combination would render clean (or fail for an unrelated
  # reason with a different message), and either way the grep below would no
  # longer find the expected text, turning this target red.
  guard_names=(
    "ingress.enabled and httpRoute.enabled both true"
    "config.databaseUrl empty"
    "config.databaseUser empty"
    "existingSecret.name empty"
    "migrationJob.databaseUser empty"
    "migrationJob.databaseUser equals config.databaseUser"
    "ingress.enabled true, config.trustedProxies empty"
    "httpRoute.enabled true, httpRoute.parentRefs.name empty"
    "httpRoute.enabled true, httpRoute.parentRefs.namespace empty"
    "config.databaseUrl port mismatching networkPolicy.egress.postgres.port"
    "config.oidc.issuerUrl port mismatching networkPolicy.egress.oidc.port"
    "config.databaseUrl names a non-numeric port in its \"port\" query parameter"
    "config.databaseUrl names a non-numeric port in its host part"
    "config.oidc.issuerUrl names a non-numeric port in its \"port\" query parameter"
    "config.oidc.issuerUrl names a non-numeric port in its host part"
    "networkPolicy.egress.postgres.port empty while egress enabled"
    "networkPolicy.egress.oidc.port empty while egress enabled"
    "service.port equals adminPort"
    "httpRoute.enabled true, config.trustedProxies empty"
    "config.passkey.rpId set, config.passkey.origins empty"
    "config.sessionAllowPlainHttp true behind an ingress"
    "config.databaseUrl port outside 1 to 65535"
    "config.oidc.issuerUrl port outside 1 to 65535"
    "objectStore.endpoint set, objectStore.bucket empty"
    "objectStore.endpoint carries user info"
    "objectStore.endpoint names a non-numeric port in its host part"
    "objectStore.endpoint holds a bad percent escape"
    "config.databaseUrl holds a bad percent escape"
    "config.databaseUrl holds whitespace"
    "config.oidc.issuerUrl holds a bad percent escape"
    "config.databaseUrl holds a DEL character"
    "config.databaseUrl holds a caret in its password"
    "config.databaseUrl holds an unclosed bracket"
    "objectStore.endpoint names a scheme this chart knows no port for"
    "objectStore.endpoint names no scheme"
    "config.oidc.issuerUrl holds a second colon in its host"
    "config.databaseUrl is scheme-relative"
  )

  # The DEL character (U+007F), set through printf: bash's $'\x7f' next to a quoted string
  # adds a stray U+0001 in front of it, which would make that case reject for the wrong
  # character.
  del=$(printf '\177')

  guard_sets=(
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.trustedProxies[0]=10.0.0.0/8,ingress.enabled=true,ingress.className=nginx,httpRoute.enabled=true,httpRoute.parentRefs.name=shared-gateway,httpRoute.parentRefs.namespace=gateway-system"
    "existingSecret.name=solobill-secrets,config.databaseUrl=,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser="
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_app"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,ingress.enabled=true,ingress.className=nginx"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.trustedProxies[0]=10.0.0.0/8,httpRoute.enabled=true,httpRoute.parentRefs.name=,httpRoute.parentRefs.namespace=gateway-system"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.trustedProxies[0]=10.0.0.0/8,httpRoute.enabled=true,httpRoute.parentRefs.name=shared-gateway,httpRoute.parentRefs.namespace="
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:6432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.oidc.issuerUrl=http://keycloak.identity.svc.cluster.local:8080/realms/solobill"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc/solobill?sslmode=require&port=abc,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:abc/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.oidc.issuerUrl=https://idp.example.com/realms/solobill?port=abc"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.oidc.issuerUrl=https://idp.example.com:abc/realms/solobill"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,networkPolicy.egress.postgres.port=null"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,networkPolicy.egress.oidc.port=null"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,service.port=8081"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,httpRoute.enabled=true,httpRoute.parentRefs.name=shared-gateway,httpRoute.parentRefs.namespace=gateway-system"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.passkey.rpId=solobill.example.org"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.trustedProxies[0]=10.0.0.0/8,ingress.enabled=true,ingress.className=nginx,config.sessionAllowPlainHttp=true"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:65536/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.oidc.issuerUrl=https://idp.example.com:0/realms/solobill"
    "${baseline},objectStore.endpoint=http://s3.example.svc:8333,objectStore.bucket="
    "${baseline},objectStore.endpoint=http://key:s3cr3tTOKEN@s3.example.svc:8333,objectStore.bucket=solobill"
    "${baseline},objectStore.endpoint=http://s3.example.svc:abc,objectStore.bucket=solobill"
    "${baseline},objectStore.endpoint=http://s3.example.svc:8333/%zz,objectStore.bucket=solobill"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://u:s3cr3tTOKEN@postgres.example.svc:5432/solobill?x=%zz,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://u:s3cr3tTOKEN@post gres.example.svc:5432/solobill,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "${baseline},config.oidc.issuerUrl=https://u:s3cr3tTOKEN@idp.example.com/realms/%zz"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://u:s3cr3tTOKEN@postgres.example.svc:5432/solobill${del},config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://u:s3cr^3tTOKEN@postgres.example.svc:5432/solobill,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://u:s3cr3tTOKEN@[::1:5432/solobill,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "${baseline},objectStore.endpoint=ftp://s3.example.svc,objectStore.bucket=solobill"
    "${baseline},objectStore.endpoint=s3.example.svc,objectStore.bucket=solobill"
    "${baseline},config.oidc.issuerUrl=https://u:s3cr3tTOKEN@::1/realms/solobill"
    "existingSecret.name=solobill-secrets,config.databaseUrl=//u:s3cr3tTOKEN@postgres.example.svc:abc/solobill,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
  )

  # The distinctive lead sentence of each guard's own fail message (see
  # charts/solobill/templates/deployment.yaml, templates/httproute.yaml and
  # templates/networkpolicy.yaml), not the full text: enough to tell the guards
  # apart and to prove this specific guard fired, short enough to survive a
  # reworded explanation. The two port-comparison guards below are matched on
  # the text before the port numbers they interpolate, not the numbers
  # themselves. The two port-range guards (a URL port outside 1 to 65535) name
  # their key only and interpolate nothing.
  guard_messages=(
    "ingress.enabled and httpRoute.enabled cannot both be true"
    "config.databaseUrl is required"
    "config.databaseUser is required"
    "existingSecret.name is required"
    "migrationJob.databaseUser is required when migrationJob.enabled is true"
    "migrationJob.databaseUser must not equal config.databaseUser"
    "ingress.enabled is true but config.trustedProxies is empty"
    "httpRoute.parentRefs.name is required when httpRoute.enabled is true"
    "httpRoute.parentRefs.namespace is required when httpRoute.enabled is true"
    "config.databaseUrl names port"
    "config.oidc.issuerUrl names port"
    "in its \"port\" query parameter"
    "in its host part"
    "in its \"port\" query parameter"
    "in its host part"
    "networkPolicy.egress.postgres.port is required while networkPolicy.egress.enabled is true"
    "networkPolicy.egress.oidc.port is required while networkPolicy.egress.enabled is true"
    "service.port and adminPort are both"
    "httpRoute.enabled is true but config.trustedProxies is empty"
    "config.passkey.rpId is set but config.passkey.origins is empty"
    "config.sessionAllowPlainHttp is true with an ingress or a gateway in front"
    "config.databaseUrl names a port outside 1 to 65535"
    "config.oidc.issuerUrl names a port outside 1 to 65535"
    "objectStore.bucket is required when objectStore.endpoint is set"
    "objectStore.endpoint must be an http or https URL with a host, scheme and host only"
    "objectStore.endpoint has a non-numeric port in its host part"
    "objectStore.endpoint is not a valid URL"
    "config.databaseUrl is not a valid URL"
    "config.databaseUrl is not a valid URL"
    "config.oidc.issuerUrl is not a valid URL"
    "config.databaseUrl is not a valid URL"
    "config.databaseUrl is not a valid URL"
    "config.databaseUrl is not a valid URL"
    "objectStore.endpoint names no scheme this chart knows a port for"
    "objectStore.endpoint names no scheme this chart knows a port for"
    "config.oidc.issuerUrl has a non-numeric port in its host part"
    "config.databaseUrl is not a valid URL"
  )

  for i in "${!guard_names[@]}"; do
    name="${guard_names[$i]}"
    set_args="${guard_sets[$i]}"
    message="${guard_messages[$i]}"

    echo "==> validating guard (must fail): ${name}"

    if output=$(helm template "${chart}" --set "${set_args}" 2>&1); then
      echo "validate-chart: guard did not fire, expected this combination to fail the render: ${name}" >&2
      echo "rendered output:" >&2
      echo "${output}" >&2
      exit 1
    fi

    if ! grep -qF "${message}" <<<"${output}"; then
      echo "validate-chart: combination failed, but not naming the guard under test: ${name}" >&2
      echo "expected the render's error to contain: ${message}" >&2
      echo "actual output:" >&2
      echo "${output}" >&2
      exit 1
    fi

    # No case may print the URL it was given. The fake token s3cr3tTOKEN stands for a
    # password. This branch does not echo the output, which would carry the token. The two
    # branches above echo it, they only run while a guard is broken.
    if grep -qF "s3cr3tTOKEN" <<<"${output}"; then
      echo "validate-chart: the error text prints the URL (the fake token is in it): ${name}" >&2
      exit 1
    fi
  done

  echo "all guards fail as expected, each naming itself (lint)"
fi
