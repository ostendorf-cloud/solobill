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
- Only if `httpRoute.enabled` is set: the Gateway API CRDs and a running `Gateway` with a
  listener that allows this release's namespace. See "External routing" below; this chart
  ships neither.

## Installing

```sh
helm install solobill charts/solobill \
  --set existingSecret.name=solobill-secrets \
  --set config.databaseUrl=postgres://postgres.example.svc:5432/solobill?sslmode=require \
  --set config.databaseUser=solobill_app
```

The chart has no usable default for `existingSecret.name`, `config.databaseUrl` or
`config.databaseUser`: `templates/deployment.yaml` fails an install or upgrade that leaves any
of them empty, with a message naming the missing key.

This chart is also published as an OCI artifact, versioned in lock-step with every release, once
a publish workflow for it exists; none does yet, so for now install or `helm pull` it by path
from a checkout of this repository, as above.

## One role, or two

Unlike `docker-compose.yaml`'s own local development setup, which brings its own throwaway
Postgres and therefore has to script a second, restricted role itself (a bootstrap superuser
used only to apply `db/migrations`, and a separate `NOSUPERUSER NOBYPASSRLS` role the
application actually connects as, see that file's own comment for the full reasoning), this
chart's Postgres already runs in the cluster it installs into.

By default, this chart's own migration Job (below) and its Deployment authenticate as the
exact same role, configured once by `config.databaseUser` and
`existingSecret.databasePasswordKey`. A correctly-provisioned `NOSUPERUSER NOBYPASSRLS` role
is still fully bound by `FORCE ROW LEVEL SECURITY` for every ordinary application query:
ownership is irrelevant to `FORCE`, only a superuser or a `BYPASSRLS` role bypasses it. But
that same role, because it is the one that ran `db/migrations` and therefore OWNS `tenants`,
`accounts` and `invites`, can also run `ALTER TABLE ... NO FORCE ROW LEVEL SECURITY` or drop
the policy outright: PostgreSQL's own `ALTER TABLE` reference documents ownership, not
`FORCE`, as what that command checks. A single role that both migrates and serves the
application therefore accepts a weaker boundary than `docker-compose.yaml`'s own two-role
split: a compromised or buggy application process could not just read another tenant's rows
through a live connection, but could also switch the protection off for every tenant at once,
by virtue of owning the tables.

Set `migrationJob.databaseUser` (and, if its password lives under a different key in the same
Secret, `existingSecret.migrationDatabasePasswordKey`) to a separate role the application
itself never authenticates as, to get the stronger, two-role boundary instead, mirroring
`docker-compose.yaml`'s own bootstrap-superuser-for-migrations-only split. Left empty, the
default, this chart keeps the single-role behaviour above.

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
`migrationJob.databaseUser` (falling back to `config.databaseUser` when unset; see
"One role, or two" above) and the existing Secret's password with plain POSIX shell parameter
expansion, not a URL encoder: the password must therefore contain no character that needs
percent-encoding in a URL (no `@`, `/`, `:`, `%`, whitespace, ...). This organisation's own
secret-generation convention (`openssl rand -hex ...`, see this repository's own
`.env.example`) already produces hex-only passwords, which have none; generate the password the
same way.

## External routing

This chart reaches external traffic to its own `Service` two ways, chosen by
`ingress.enabled` and `httpRoute.enabled`. Both default to `false`: a default install creates
neither, reachable only from inside the cluster, until one is deliberately turned on. They are
mutually exclusive: turning both on fails the install or upgrade with a readable message
naming which one to turn off, enforced once in `templates/deployment.yaml`, the one template
that always renders regardless of which routing flag is set.

### Ingress

Set `ingress.enabled: true`. `ingress.hosts` names the host and path rules, and `ingress.tls`
carries the certificate configuration directly on the `Ingress` object: this chart owns that
configuration. `ingress.enabled: true` also requires `config.trustedProxies`; see "Values"
below for why.

### Gateway API HTTPRoute

Set `httpRoute.enabled: true`, `httpRoute.parentRefs.name` and `httpRoute.parentRefs.namespace`
to the `Gateway` this release attaches to. The namespace is required, not optional the way
`ingress.className` is: a `Gateway` is almost always platform owned and lives in a different
namespace than this release, and the Gateway API's own default for an unset
`parentRefs.namespace` is the `HTTPRoute`'s own namespace, normally wrong here.

This chart never creates the `Gateway` and never ships the Gateway API CRDs (`Gateway`,
`HTTPRoute` and the rest): both are cluster-level prerequisites, installed and operated once
per cluster by the cluster operator, same as any other CRD (see `kubernetes-conventions`'s
`install-crds` convention), outside this chart. The `Gateway`'s own `allowedRoutes` decides
whether it actually accepts this `HTTPRoute`; a `httpRoute.enabled: true` release that the
`Gateway` does not allow installs without error but never actually receives traffic.

Unlike `Ingress`, an `HTTPRoute` carries no `tls` field of its own. TLS terminates on the
`Gateway`'s listener instead, which the cluster operator, not this chart, owns and configures;
`httpRoute.hostnames` only has to match a hostname the listener itself already serves for
traffic to reach this `HTTPRoute` at all.

Routing itself is fixed to this chart's own `Service`: no values-driven way to add a second
backend or a per-path rule, the same single-backend assumption `ingress.hosts` already makes.
An `HTTPRoute` rule with no `matches` defaults to a `/` prefix match per the Gateway API spec,
the same default an `Ingress` with one `/` path rule gives, so this chart's `HTTPRoute` sets
none explicitly.

`httpRoute.hostnames` is a list today because `HTTPRoute` itself accepts more than one
hostname for the one set of rules above; solobill's own concept (the application on one
hostname, each tenant's shop on its own) does not need that yet; it is set up front to one or
a few hostnames, not resolved per request from a tenant's own record. A `Gateway` capable of
hosting tenant shops with per-tenant certificates, and a values shape that renders one
`HTTPRoute` (or one rule) per tenant from a list instead of this chart's current single fixed
one, stay future work; this values shape does not rule either out, but does not build toward
them either.

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

## Metrics and probes live on their own port

`cmd/solobill` runs two listeners, through govue's `httpserver.RunAdmin`: the public one,
`config.listenAddr`/`service.port`, which this chart's `Ingress` routes to, and a second,
administrative one, `config.adminListenAddr`/`adminPort`, which it does not. `/metrics`,
`/healthz` and `/readyz` are served only on the second: anything that can reach this
application through its `Ingress` can no longer scrape its metrics or poll its probes, which
it previously could, since all three used to share the public listener. `templates/ingress.yaml`
only ever references `service.port`, never `adminPort`, so the admin listener is reachable only
by a caller already inside the cluster, pod to pod.

`adminPort` is deliberately not added to `templates/service.yaml`'s own ports list.
`livenessProbe` and `readinessProbe` reach it by naming the container port directly
(`port: admin`), which a kubelet resolves against the pod's own IP, never through the
`Service`. No `ServiceMonitor` or scrape config exists in this chart yet; add `adminPort` to
the `Service` as its own named port only once something in-cluster needs to reach it that way,
rather than exposing it on the `Service` ahead of an actual need.

## Values

| Key | Description | Default |
| --- | --- | --- |
| `replicaCount` | Number of pods. Fixed at `1`: `cmd/solobill`'s session store is in-memory, correct for exactly one replica. A second pod would keep its own, separate sessions, so a request load-balanced to it would see its caller as logged out. | `1` |
| `image.repository` | Container image repository. | `ghcr.io/ostendorf-cloud/solobill` |
| `image.tag` | Image tag. Defaults to the chart's `appVersion` when left empty. | `""` |
| `image.pullPolicy` | Image pull policy. | `IfNotPresent` |
| `imagePullSecrets` | Pull secrets for a private registry. | `[]` |
| `nameOverride` | Overrides the chart name used in generated resource names. | `""` |
| `fullnameOverride` | Overrides the full generated resource name. | `""` |
| `serviceAccount.create` | Create a dedicated `ServiceAccount`, as a pre-install,pre-upgrade hook so it exists before the migration Job that names it. No RBAC is attached and no pod ever mounts its token (`automountServiceAccountToken: false`): this application makes no Kubernetes API calls. | `true` |
| `serviceAccount.annotations` | Annotations for the `ServiceAccount`. | `{}` |
| `serviceAccount.name` | Name of the `ServiceAccount` to use or create. Defaults to the full release name. | `""` |
| `podAnnotations` | Extra annotations on the pod template. | `{}` |
| `podLabels` | Extra labels on the pod template. | `{}` |
| `podSecurityContext` | Pod-level security context. Matches the image's distroless `nonroot` user, uid/gid `65532`. | see `values.yaml` |
| `securityContext` | Container-level security context: read-only root filesystem, no privilege escalation, all capabilities dropped. Safe: `cmd/solobill` keeps its session store in memory and writes nothing else to disk. | see `values.yaml` |
| `service.type` | `Service` type. | `ClusterIP` |
| `service.port` | `Service` and public container port. Keep in sync with `config.listenAddr`. | `8080` |
| `adminPort` | Administrative container port (`/metrics`, `/healthz`, `/readyz`). Keep in sync with `config.adminListenAddr`. Not exposed on the `Service`; see "Metrics and probes live on their own port" above. | `8081` |
| `ingress.enabled` | Create an `Ingress`. Requires `config.trustedProxies` to be set; see that row. Mutually exclusive with `httpRoute.enabled`; see "External routing" above. | `false` |
| `ingress.className` | `ingressClassName` to set. | `""` |
| `ingress.annotations` | Annotations for the `Ingress`. | `{}` |
| `ingress.hosts` | Host/path rules. | see `values.yaml` |
| `ingress.tls` | TLS configuration for the `Ingress`. | `[]` |
| `httpRoute.enabled` | Create a Gateway API `HTTPRoute`. Mutually exclusive with `ingress.enabled`; see "External routing" above. | `false` |
| `httpRoute.hostnames` | Hostnames the `HTTPRoute` matches. | `[]` |
| `httpRoute.annotations` | Annotations for the `HTTPRoute`. | `{}` |
| `httpRoute.parentRefs.name` | Name of the `Gateway` to attach to. Required when `httpRoute.enabled` is true. | `""` |
| `httpRoute.parentRefs.namespace` | Namespace the `Gateway` lives in. Required when `httpRoute.enabled` is true; see "External routing" above for why this has no same-namespace fallback. | `""` |
| `httpRoute.parentRefs.sectionName` | Specific listener on the `Gateway` to attach to, when it exposes more than one. | `""` |
| `resources.requests` | Resource requests, always set. | `cpu: 100m`, `memory: 128Mi` |
| `resources.limits` | Resource limits. No CPU limit: request handling is bursty, not broken, under brief CPU pressure. A memory limit stays as a leak safety net. | `memory: 256Mi` |
| `startupProbe.enabled` | Enable a startup probe, against `adminPort`'s `/healthz`. Off by default: the migration Job, not `cmd/solobill`'s own startup, is what waits on the database. | `false` |
| `livenessProbe.*` | Liveness probe timing against `adminPort`'s `/healthz`. | see `values.yaml` |
| `readinessProbe.*` | Readiness probe timing against `adminPort`'s `/readyz`. | see `values.yaml` |
| `nodeSelector` | Node selector. | `{}` |
| `tolerations` | Tolerations. | `[]` |
| `affinity` | Affinity rules. | `{}` |
| `config.listenAddr` | `LISTEN_ADDR`. | `:8080` |
| `config.adminListenAddr` | `ADMIN_LISTEN_ADDR`. Serves `/metrics`, `/healthz` and `/readyz`; see "Metrics and probes live on their own port" above. Keep in sync with `adminPort`. | `:8081` |
| `config.logLevel` | `LOG_LEVEL`. One of `debug`, `info`, `warn`, `error`. | `info` |
| `config.databaseUrl` | `DATABASE_URL`. Credential-free; see "Why `config.databaseUrl` is plain" above. Required. | `""` |
| `config.databaseUser` | `DATABASE_USER`. Required. Must be `NOSUPERUSER NOBYPASSRLS`; see "Prerequisites" above. | `""` |
| `config.sessionAllowPlainHttp` | `SESSION_ALLOW_PLAIN_HTTP`. Keep `false` behind a TLS-terminating ingress; `true` is `docker-compose.yaml`'s own local-dev-only setting. | `false` |
| `config.allowedOrigins` | `ALLOWED_ORIGINS`, joined with a comma. Empty means no CORS middleware (same-origin SPA). | `[]` |
| `config.trustedProxies` | `TRUSTED_PROXIES`, joined with a comma. Empty trusts only the direct TCP peer. Required (and enforced with a template `fail`) when `ingress.enabled` is true: otherwise every client's `X-Forwarded-For` is ignored and the rate limiter keys every request to the ingress controller's own address, one shared bucket for every caller. Set it to the ingress controller's pod or service CIDR. | `[]` |
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
| `existingSecret.migrationDatabasePasswordKey` | Password key for `migrationJob.databaseUser`, in this same Secret. Empty falls back to `databasePasswordKey`. | `""` |
| `migrationJob.enabled` | Run the `db/migrations` hook Job on install and upgrade. | `true` |
| `migrationJob.databaseUser` | Separate role that owns and migrates `accounts`/`invites`, distinct from `config.databaseUser`. Empty falls back to `config.databaseUser`; see "One role, or two" above. | `""` |
| `migrationJob.image.repository` | The official dbmate image. | `ghcr.io/amacneil/dbmate` |
| `migrationJob.image.tag` | dbmate image tag, matching `docker-compose.yaml`'s own pin. | `2.38.1` |
| `migrationJob.annotations` | Extra annotations on the migration Job. | `{}` |
| `migrationJob.resources` | Resource requests/limits for the migration Job. | see `values.yaml` |

`existingSecret.name`, `config.databaseUrl` and `config.databaseUser` have no usable default and
are enforced by a `fail` guard in `templates/deployment.yaml` instead of `values.schema.json`'s
`minLength`: an install or upgrade with any of them missing or empty fails the render with a
readable message. Every other required field above is still enforced by the schema.

`httpRoute.parentRefs.name` and `httpRoute.parentRefs.namespace` are the same kind of
conditionally-required field, enforced the same way: a `fail` guard in `templates/httproute.yaml`
that only fires when `httpRoute.enabled` is true. The `ingress.enabled`/`httpRoute.enabled`
mutual exclusion is its own `fail` guard, in `templates/deployment.yaml`; see "External routing"
above.

## Upgrading

A plain `helm upgrade` is safe for any change to this chart's values: the migration Job
re-applies `db/migrations` (a no-op when nothing changed) before the Deployment is updated.
There is no persistent state in the chart itself: every durable fact lives in your own
PostgreSQL instance, outside this release.

## Uninstalling

```sh
helm uninstall solobill
```

Removes every ordinary object this chart created: the `Deployment`, `Service`, and the `Ingress`
or `HTTPRoute`, whichever was enabled. The `ServiceAccount`, the migration Job and its
`ConfigMap` are all Helm
hooks, which Helm does not track as part of the release for `helm uninstall` to remove; each is
governed only by its own `helm.sh/hook-delete-policy` instead. The Job and its `ConfigMap` carry
`before-hook-creation,hook-succeeded`, so a completed one is already gone by the time the install
or upgrade that created it finishes, well before any uninstall. The `ServiceAccount` carries
`before-hook-creation` alone, deliberately without `hook-succeeded`, since the Deployment needs
it to still exist after the install succeeds: an old copy is only deleted, and replaced, right
before the next install or upgrade's hooks recreate it, which means a release that is uninstalled
and never reinstalled leaves this `ServiceAccount` behind. Nothing stateful lives in the cluster
from this chart either way: the `existingSecret` referenced by name, and your PostgreSQL
instance and its data, are never touched, since this chart never owned them.

## Verification

This chart is verified with:

- `helm lint`
- `helm template`, for the default values, OIDC-enabled, ingress-enabled and
  httpRoute-enabled combinations
- `kubeconform`, against every one of those rendered manifests. The default schema set
  carries no Gateway API schemas, so the httpRoute-enabled combination's `HTTPRoute` is
  validated with a second `-schema-location` added after the default one, pointing at the
  community [CRDs-catalog](https://github.com/datreeio/CRDs-catalog):

  ```sh
  kubeconform -strict -summary \
    -schema-location default \
    -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'
  ```

- [`helm-unittest`](https://github.com/helm-unittest/helm-unittest), plugin version `v1.2.1`,
  pinned in `mise.toml` as a generic GitHub release backend entry (not installed with
  `helm plugin install`, which Helm 4 refuses for an unsigned plugin source) and run with
  `helm unittest charts/solobill`.

Every check above runs on every push and pull request, through `task lint:helm`,
`task validate:kubeconform` and `task test:helm`, each its own CI job (`helm-lint`,
`kubeconform` and `helm-unittest`), one job per check. A live end-to-end install into a real
cluster is a separate, tracked follow-up, not part of this chart's static verification.
