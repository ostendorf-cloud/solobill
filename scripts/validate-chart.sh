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
