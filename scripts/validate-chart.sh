#!/usr/bin/env bash
# Validates charts/solobill for its default values and every documented non-default
# combination, so CI and a local run share the exact same logic. The combinations below
# mirror charts/solobill/tests/*_test.yaml exactly: default, OIDC enabled, ingress enabled,
# httpRoute enabled.
#
# Takes exactly one mode argument:
#   lint        - helm lint, one job per check (Taskfile's lint:helm / CI's helm-lint job)
#   kubeconform - helm template piped into kubeconform, its own job (CI's kubeconform job)
# kept as two modes of one script, not two near-duplicate scripts, so the four --set
# combinations above live in exactly one place.
#
# Mode "lint" also runs a must-fail set (the guard_* arrays below): every render-time
# "fail" guard in charts/solobill/templates, each isolated by satisfying every other
# guard and violating exactly one, run through "helm template" and asserted to fail
# naming that guard. "helm lint" cannot do this job itself: it calls the same funcMap
# fail the templates do, but only logs it at INFO and still exits 0, so a must-fail set
# built on "helm lint" alone would prove nothing about whether a guard still fires.
# charts/solobill/tests/*_test.yaml's own helm-unittest "failedTemplate" assertions
# check the identical guards independently; the two have to agree before either can be
# trusted alone to catch a guard quietly dropped out of both. One guard,
# templates/migration-configmap.yaml's "charts/solobill/migrations is empty", has no
# must-fail case here: it fires on the chart's own migrations directory being empty,
# not on a --set value, so no combination of --set flags isolates it.
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

names=(
  "default values"
  "oidc enabled"
  "ingress enabled"
  "httpRoute enabled"
)

# Each entry is one combination's --set arguments, matching the corresponding
# charts/solobill/tests/*_test.yaml suite's "set:" block. migrationJob.databaseUser is set in
# every combination, distinct from config.databaseUser: this chart's default now requires it
# (see charts/solobill/README.md's "One role, or two"), and templates/deployment.yaml, which
# every combination below renders, fails the render otherwise.
sets=(
  "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
  "existingSecret.name=solobill-secrets,existingSecret.oidcClientSecretKey=OIDC_CLIENT_SECRET,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.oidc.issuerUrl=https://idp.example.com,config.oidc.clientId=solobill,config.oidc.redirectUrl=https://solobill.example.com/api/v1/auth/oidc/callback,config.oidc.defaultTenantSlug=acme"
  "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.trustedProxies[0]=10.0.0.0/8,ingress.enabled=true,ingress.className=nginx,ingress.hosts[0].host=solobill.example.org,ingress.hosts[0].paths[0].path=/,ingress.hosts[0].paths[0].pathType=Prefix"
  "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,httpRoute.enabled=true,httpRoute.hostnames[0]=solobill.example.org,httpRoute.parentRefs.name=shared-gateway,httpRoute.parentRefs.namespace=gateway-system"
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
  )

  guard_sets=(
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,config.trustedProxies[0]=10.0.0.0/8,ingress.enabled=true,ingress.className=nginx,httpRoute.enabled=true,httpRoute.parentRefs.name=shared-gateway,httpRoute.parentRefs.namespace=gateway-system"
    "existingSecret.name=solobill-secrets,config.databaseUrl=,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser="
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_app"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,ingress.enabled=true,ingress.className=nginx"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,httpRoute.enabled=true,httpRoute.parentRefs.name=,httpRoute.parentRefs.namespace=gateway-system"
    "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,migrationJob.databaseUser=solobill_migrator,httpRoute.enabled=true,httpRoute.parentRefs.name=shared-gateway,httpRoute.parentRefs.namespace="
  )

  # The distinctive lead sentence of each guard's own fail message (see
  # charts/solobill/templates/deployment.yaml and templates/httproute.yaml),
  # not the full text: enough to tell the guards apart and to prove this
  # specific guard fired, short enough to survive a reworded explanation.
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
  done

  echo "all guards fail as expected, each naming itself (lint)"
fi
