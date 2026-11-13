# solobill

Installs [solobill](https://github.com/ostendorf-cloud/solobill) on a Kubernetes cluster.

## Prerequisites

- Kubernetes 1.27 or newer.
- A PostgreSQL database reachable from the cluster, with `db/migrations` either already
  applied or applicable by this chart's own migration Job (see "Migrations" below).
- **The database role this chart connects as must be `NOSUPERUSER` and `NOBYPASSRLS`.**
  `db/migrations` enables `FORCE ROW LEVEL SECURITY` on `accounts` and `invites`: that is the
  actual multi-tenancy guarantee, not application code. `FORCE` binds the policy to the
  table's own owning role too, but a Postgres superuser, or any role carrying the `BYPASSRLS`
  attribute, is exempt from row level security regardless of `FORCE`. If the role named by
  `config.databaseUser` has either property, every tenant-isolation policy this application
  depends on provides no actual protection, silently. Provisioning that role correctly is the
  cluster operator's responsibility: this chart has no way to enforce it from here, and never
  creates the role itself.
- A `Secret` already in the target namespace, created by your own secret manager operator,
  carrying the required database password and signing keys (see "Secrets" below). This chart
  never creates that `Secret` itself.

## Installing

```sh
helm install solobill charts/solobill \
  --set existingSecret.name=solobill-secrets \
  --set config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require \
  --set config.databaseUser=solobill_app
```

The chart has no usable default for `existingSecret.name`, `config.databaseUrl` or
`config.databaseUser`: `values.schema.json` rejects an install or upgrade that leaves any of
them empty.

This chart is also published as an OCI artifact, versioned in lock-step with every release, once
a publish workflow for it exists; none does yet, so for now install or `helm pull` it by path
from a checkout of this repository, as above.

## One role is enough

Unlike `docker-compose.yaml`'s own local development setup, which brings its own throwaway
Postgres and therefore has to script a second, restricted role itself (a bootstrap superuser
used only to apply `db/migrations`, and a separate `NOSUPERUSER NOBYPASSRLS` role the
application actually connects as, see that file's own comment for the full reasoning), this
chart's Postgres already runs in the cluster it installs into. A single, correctly-provisioned
`NOSUPERUSER NOBYPASSRLS` role that owns its own tables is still fully bound by
`FORCE ROW LEVEL SECURITY`: ownership is irrelevant once `FORCE` is set, only a superuser or a
`BYPASSRLS` role bypasses it. So this chart's own migration Job (below) and its Deployment
authenticate as the exact same role, configured once by `config.databaseUser` and
`existingSecret.databasePasswordKey`. This chart does not need, and does not implement,
`docker-compose.yaml`'s two-role split.

## Migrations

A pre-install,pre-upgrade Helm hook Job applies `db/migrations` with
[dbmate](https://github.com/amacneil/dbmate) before this chart's own Deployment is created or
updated, an upgrade included, so a new image's pods never start against an old schema. The
migration files themselves are embedded into the chart as a `ConfigMap`, through a
`charts/solobill/migrations` symlink to the repository's own `db/migrations` (not a hand-copied
duplicate: `helm.Files.Glob` only ever reads inside a chart's own directory, so the symlink is
what keeps this chart's migrations always matching what `internal/storage` actually needs).
`dbmate up` is idempotent (it tracks what it already applied in its own `schema_migrations`
table), so re-running it on every upgrade against an already-migrated database is safe.

Disable it with `migrationJob.enabled: false` for a deployment that applies `db/migrations`
some other way; see "Values" below.

The migration Job assembles its own connection URL from `config.databaseUrl`,
`config.databaseUser` and the existing Secret's password with plain POSIX shell parameter
expansion, not a URL encoder: the password must therefore contain no character that needs
percent-encoding in a URL (no `@`, `/`, `:`, `%`, whitespace, ...). This organisation's own
secret-generation convention (`openssl rand -hex ...`, see this repository's own
`.env.example`) already produces hex-only passwords, which have none; generate the password the
same way.

## Secrets

Every secret-shaped setting is read from one existing `Secret`, named by
`existingSecret.name`, key by key. This chart never offers a values-driven way to create its
own `Secret` from plaintext values: the `Secret` itself comes from your own secret manager
operator (External Secrets, Vault, SOPS, or similar), and this chart only ever references it
by name.

| Env var | `values.yaml` key | Default key in the Secret | Required |
| --- | --- | --- | --- |
| `DATABASE_PASSWORD` | `existingSecret.databasePasswordKey` | `DATABASE_PASSWORD` | Yes |
| `SESSION_CODEC_KEY` | `existingSecret.sessionCodecKeyKey` | `SESSION_CODEC_KEY` | Yes |
| `CSRF_KEY` | `existingSecret.csrfKeyKey` | `CSRF_KEY` | Yes |
| `OIDC_CLIENT_SECRET` | `existingSecret.oidcClientSecretKey` | unset (omitted) | Only with OIDC |

A required key has no fallback: a `Secret` missing one of `databasePasswordKey`,
`sessionCodecKeyKey` or `csrfKeyKey` fails the pod at the Kubernetes level
(`CreateContainerConfigError`), not silently with an empty value. `oidcClientSecretKey` left
empty omits `OIDC_CLIENT_SECRET` entirely, rather than referencing a key that may not exist in
your `Secret`; set it together with `config.oidc.issuerUrl`, `config.oidc.clientId`,
`config.oidc.redirectUrl` and `config.oidc.defaultTenantSlug`, all four of which
`internal/config.Load` requires together once OIDC login is actually configured.

`SESSION_CODEC_KEY` and `CSRF_KEY` are hex encoded, at least 32 bytes decoded, for example
generated with `openssl rand -hex 32`. `DATABASE_PASSWORD` must be free of characters that need
percent-encoding in a URL; see "Migrations" above.

## Why `config.databaseUrl` is plain here, unlike some other charts' `DATABASE_URL`

`config.databaseUrl` is a plain value, not read from `existingSecret`, because
`internal/config.Config`'s own `DatabaseURL` field is documented as "the connection URL without
credentials: scheme, host, port, database name and query parameters only" — `config.databaseUser`
and the secret-only `DATABASE_PASSWORD` are assembled into it by `cmd/solobill` itself
(`Config.DatabaseDSN`), not carried in the URL. A chart for an application whose own
`DATABASE_URL` carries full credentials in the URL (user:pass@host/db) would need to treat that
field as secret-only instead; this is solobill's own contract, verified by reading
`internal/config/config.go`, not a convention copied from elsewhere.

## Values

| Key | Description | Default |
| --- | --- | --- |
| `replicaCount` | Number of pods. | `1` |
| `image.repository` | Container image repository. | `ghcr.io/ostendorf-cloud/solobill` |
| `image.tag` | Image tag. Defaults to the chart's `appVersion` when left empty. | `""` |
| `image.pullPolicy` | Image pull policy. | `IfNotPresent` |
| `imagePullSecrets` | Pull secrets for a private registry. | `[]` |
| `nameOverride` | Overrides the chart name used in generated resource names. | `""` |
| `fullnameOverride` | Overrides the full generated resource name. | `""` |
| `serviceAccount.create` | Create a dedicated `ServiceAccount`. No RBAC is attached: this application makes no Kubernetes API calls. | `true` |
| `serviceAccount.annotations` | Annotations for the `ServiceAccount`. | `{}` |
| `serviceAccount.name` | Name of the `ServiceAccount` to use or create. Defaults to the full release name. | `""` |
| `podAnnotations` | Extra annotations on the pod template. | `{}` |
| `podLabels` | Extra labels on the pod template. | `{}` |
| `podSecurityContext` | Pod-level security context. Matches the image's distroless `nonroot` user, uid/gid `65532`. | see `values.yaml` |
| `securityContext` | Container-level security context: read-only root filesystem, no privilege escalation, all capabilities dropped. Safe: `cmd/solobill` keeps its session store in memory and writes nothing else to disk. | see `values.yaml` |
| `service.type` | `Service` type. | `ClusterIP` |
| `service.port` | `Service`, container and probe port. Keep in sync with `config.listenAddr`. | `8080` |
| `ingress.enabled` | Create an `Ingress`. | `false` |
| `ingress.className` | `ingressClassName` to set. | `""` |
| `ingress.annotations` | Annotations for the `Ingress`. | `{}` |
| `ingress.hosts` | Host/path rules. | see `values.yaml` |
| `ingress.tls` | TLS configuration for the `Ingress`. | `[]` |
| `resources.requests` | Resource requests, always set. | `cpu: 100m`, `memory: 128Mi` |
| `resources.limits` | Resource limits. No CPU limit: request handling is bursty, not broken, under brief CPU pressure. A memory limit stays as a leak safety net. | `memory: 256Mi` |
| `startupProbe.enabled` | Enable a startup probe. Off by default: the migration Job, not `cmd/solobill`'s own startup, is what waits on the database. | `false` |
| `livenessProbe.*` | Liveness probe timing against `/healthz`. | see `values.yaml` |
| `readinessProbe.*` | Readiness probe timing against `/readyz`. | see `values.yaml` |
| `nodeSelector` | Node selector. | `{}` |
| `tolerations` | Tolerations. | `[]` |
| `affinity` | Affinity rules. | `{}` |
| `config.listenAddr` | `LISTEN_ADDR`. | `:8080` |
| `config.logLevel` | `LOG_LEVEL`. One of `debug`, `info`, `warn`, `error`. | `info` |
| `config.databaseUrl` | `DATABASE_URL`. Credential-free; see "Why `config.databaseUrl` is plain" above. Required. | `""` |
| `config.databaseUser` | `DATABASE_USER`. Required. Must be `NOSUPERUSER NOBYPASSRLS`; see "Prerequisites" above. | `""` |
| `config.sessionAllowPlainHttp` | `SESSION_ALLOW_PLAIN_HTTP`. Keep `false` behind a TLS-terminating ingress; `true` is `docker-compose.yaml`'s own local-dev-only setting. | `false` |
| `config.allowedOrigins` | `ALLOWED_ORIGINS`, joined with a comma. Empty means no CORS middleware (same-origin SPA). | `[]` |
| `config.trustedProxies` | `TRUSTED_PROXIES`, joined with a comma. Empty trusts only the direct TCP peer. | `[]` |
| `config.rateLimitBurst` | `RATE_LIMIT_BURST`. | `10` |
| `config.rateLimitPer` | `RATE_LIMIT_PER`. | `1m` |
| `config.inviteDefaultTtl` | `INVITE_DEFAULT_TTL`. | `168h` |
| `config.oidc.issuerUrl` | `OIDC_ISSUER_URL`. OIDC login is off exactly when this is empty. | `""` |
| `config.oidc.clientId` | `OIDC_CLIENT_ID`. Required together with `issuerUrl`. | `""` |
| `config.oidc.redirectUrl` | `OIDC_REDIRECT_URL`. Required together with `issuerUrl`. | `""` |
| `config.oidc.scopes` | `OIDC_SCOPES`, joined with a comma. | `[]` |
| `config.oidc.defaultTenantSlug` | `OIDC_DEFAULT_TENANT_SLUG`. Required together with `issuerUrl`. | `""` |
| `existingSecret.name` | Name of the existing `Secret` carrying every secret-shaped value. Required. | `""` |
| `existingSecret.databasePasswordKey` | Key for `DATABASE_PASSWORD`. Required. | `DATABASE_PASSWORD` |
| `existingSecret.sessionCodecKeyKey` | Key for `SESSION_CODEC_KEY`. Required. | `SESSION_CODEC_KEY` |
| `existingSecret.csrfKeyKey` | Key for `CSRF_KEY`. Required. | `CSRF_KEY` |
| `existingSecret.oidcClientSecretKey` | Key for `OIDC_CLIENT_SECRET`. Empty omits the variable. | `""` |
| `migrationJob.enabled` | Run the `db/migrations` hook Job on install and upgrade. | `true` |
| `migrationJob.image.repository` | The official dbmate image. | `ghcr.io/amacneil/dbmate` |
| `migrationJob.image.tag` | dbmate image tag, matching `docker-compose.yaml`'s own pin. | `2.38.1` |
| `migrationJob.annotations` | Extra annotations on the migration Job. | `{}` |
| `migrationJob.resources` | Resource requests/limits for the migration Job. | see `values.yaml` |

`values.schema.json` enforces the required fields above; an install or upgrade with any of them
missing or empty fails validation before it reaches the cluster.

## Upgrading

A plain `helm upgrade` is safe for any change to this chart's values: the migration Job
re-applies `db/migrations` (a no-op when nothing changed) before the Deployment is updated.
There is no persistent state in the chart itself: every durable fact lives in your own
PostgreSQL instance, outside this release.

## Uninstalling

```sh
helm uninstall solobill
```

Removes every object this chart created: the `Deployment`, `Service`, `ServiceAccount`, and the
`Ingress` if it was enabled. The migration Job and its `ConfigMap` are Helm hooks with a
`before-hook-creation,hook-succeeded` delete policy, so a completed one is already gone by the
time an install or upgrade finishes. Nothing stateful lives in the cluster from this chart: the
`existingSecret` referenced by name, and your PostgreSQL instance and its data, are never
touched, since this chart never owned them.

## Verification

This chart is verified with:

- `helm lint`
- `helm template`, for the default values, OIDC-enabled and ingress-enabled combinations
- `kubeconform`, against every one of those rendered manifests
- [`helm-unittest`](https://github.com/helm-unittest/helm-unittest), plugin version `v1.2.1`,
  installed with:

  ```sh
  helm plugin install https://github.com/helm-unittest/helm-unittest --version v1.2.1
  ```

  and run with `helm unittest charts/solobill`.

Every check above runs on every push and pull request, through `task lint:helm` and
`task test:helm`. A live end-to-end install into a real cluster is a separate, tracked
follow-up, not part of this chart's static verification.
