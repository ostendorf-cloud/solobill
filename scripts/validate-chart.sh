#!/usr/bin/env bash
# Lints and schema-validates charts/solobill for its default values and every documented
# non-default combination, so CI and a local run share the exact same logic. The combinations
# below mirror charts/solobill/tests/*_test.yaml exactly: default, OIDC enabled, ingress enabled.
#
# kubeconform resolves its Kubernetes schemas over the network with its normal default
# behaviour (no local schema clone, no relaxed TLS verification): a CI runner with normal
# internet access reaches raw.githubusercontent.com directly with no further setup needed.
set -euo pipefail

chart="charts/solobill"

names=(
  "default values"
  "oidc enabled"
  "ingress enabled"
)

# Each entry is one combination's --set arguments, matching the corresponding
# charts/solobill/tests/*_test.yaml suite's "set:" block.
sets=(
  "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app"
  "existingSecret.name=solobill-secrets,existingSecret.oidcClientSecretKey=OIDC_CLIENT_SECRET,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,config.oidc.issuerUrl=https://idp.example.com,config.oidc.clientId=solobill,config.oidc.redirectUrl=https://solobill.example.com/api/v1/auth/oidc/callback,config.oidc.defaultTenantSlug=acme"
  "existingSecret.name=solobill-secrets,config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require,config.databaseUser=solobill_app,ingress.enabled=true,ingress.className=nginx,ingress.hosts[0].host=solobill.example.org,ingress.hosts[0].paths[0].path=/,ingress.hosts[0].paths[0].pathType=Prefix"
)

for i in "${!names[@]}"; do
  name="${names[$i]}"
  set_args="${sets[$i]}"

  echo "==> validating combination: ${name}"

  if ! helm lint "${chart}" --set "${set_args}"; then
    echo "validate-chart: helm lint failed for combination: ${name}" >&2
    exit 1
  fi

  if ! helm template "${chart}" --set "${set_args}" | kubeconform -strict -summary; then
    echo "validate-chart: kubeconform failed for combination: ${name}" >&2
    exit 1
  fi
done

echo "all combinations valid"
