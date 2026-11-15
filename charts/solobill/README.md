# solobill

Installs [solobill](https://github.com/ostendorf-cloud/solobill) on a Kubernetes cluster.

## Prerequisites

- Kubernetes 1.27 or newer.
- A PostgreSQL database reachable from the cluster, with `db/migrations` either already
  applied or applicable by this chart's own migration Job (see "Migrations" below).
- **Two Postgres roles, not one.** This chart's default is the two-role split "One role, or
  two" below describes in full: a migration role (`migrationJob.databaseUser`) that runs
  `db/migrations` and owns the tables it creates, and a separate application role
  (`config.databaseUser`) that never does. Both roles must already exist; this chart creates
  neither, and fails an install or upgrade outright when it is not told two distinct roles (see
  "Installing" below).
- **The role named by `config.databaseUser` must be `NOSUPERUSER` and `NOBYPASSRLS`, and must
  never own `tenants`, `accounts`, `invites`, `documents` or `document_lines`.** `db/migrations`
  enables `FORCE ROW LEVEL SECURITY` on every tenant-scoped table: that is the actual
  multi-tenancy guarantee, not application code, and `documents`/`document_lines` each also
  carry a trigger that is the actual mechanism keeping an issued invoice from ever being
  changed again. `FORCE` binds the row-level-security policy to a table's own owning role too,
  but a Postgres superuser, or any role carrying the `BYPASSRLS` attribute, is exempt from row
  level security regardless of `FORCE`; and disabling a trigger
  (`ALTER TABLE ... DISABLE TRIGGER`) takes the same thing bypassing `FORCE` does: table
  ownership or superuser. If the role named by `config.databaseUser` has any of those three
  properties, every guarantee this application depends on provides no actual protection,
  silently. Provisioning both roles correctly, and never granting `config.databaseUser`
  ownership some other way, is the cluster operator's responsibility: this chart has no way to
  enforce it from here, and never creates either role itself.
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
  --set config.databaseUser=solobill_app \
  --set migrationJob.databaseUser=solobill_migrator
```

The chart has no usable default for `existingSecret.name`, `config.databaseUrl`,
`config.databaseUser` or `migrationJob.databaseUser`: `templates/deployment.yaml` fails an
install or upgrade that leaves any of the first three empty, or that leaves
`migrationJob.databaseUser` empty or equal to `config.databaseUser`, with a message naming what
is missing and what to do about it. See "One role, or two" below for why
`migrationJob.databaseUser` has no fallback to `config.databaseUser`.

This chart is also published as an OCI artifact, versioned in lock-step with every release, once
a publish workflow for it exists; none does yet, so for now install or `helm pull` it by path
from a checkout of this repository, as above.

## One role, or two

This chart's default is two roles, mirroring `docker-compose.yaml`'s own local development
setup: that compose file brings its own throwaway Postgres and therefore has to script a
second, restricted role itself (a bootstrap role used only to apply `db/migrations`, and a
separate `NOSUPERUSER NOBYPASSRLS` role the application actually connects as, see that file's
own comment for the full reasoning). This chart's Postgres already runs in the cluster it
installs into instead, so provisioning the two roles is the cluster operator's job, not a
script this chart runs; the shape is otherwise the same, with one difference noted below.
`migrationJob.databaseUser` runs the migration Job and owns every table `db/migrations`
creates; `config.databaseUser`, the Deployment's own role, never does. There is no fallback
between the two any more: `templates/deployment.yaml` fails an install or upgrade that leaves
`migrationJob.databaseUser` empty or equal to `config.databaseUser`, naming the problem and
pointing back here, rather than quietly authenticating the migration Job and the Deployment as
the same role.

A correctly-provisioned `NOSUPERUSER NOBYPASSRLS` `config.databaseUser` is fully bound by
`FORCE ROW LEVEL SECURITY` for every ordinary application query regardless of ownership: only
a superuser or a `BYPASSRLS` role bypasses it, `FORCE` notwithstanding. But PostgreSQL's own
`ALTER TABLE` reference documents ownership, not `FORCE`, as what `ALTER TABLE` itself checks,
and disabling a trigger (`ALTER TABLE ... DISABLE TRIGGER`) needs that same thing: table
ownership or superuser, nothing row-level-security specific. `documents` and `document_lines`
each carry a `BEFORE UPDATE OR DELETE`/`BEFORE INSERT OR UPDATE OR DELETE` trigger
(`documents_block_issued_mutation`, `document_lines_block_issued_document_mutation`) that is
the actual mechanism keeping an issued invoice from ever being changed again; see
`db/migrations`' own comments on both for the full reasoning. A role that owns these tables can
disable either trigger, or drop a row-level-security policy outright, and then mutate an
issued invoice or read another tenant's rows, regardless of what either guarantee is supposed
to prevent - which is exactly why `config.databaseUser` must never be that role, and why this
chart no longer defaults to letting it be.

Grant `config.databaseUser` exactly the privileges `docker-compose.yaml`'s own
`grant-app-role` service grants `solobill_app`, table by table, never ownership:
`SELECT, INSERT` on `tenants`; `SELECT, INSERT, UPDATE` on `accounts`, `invites`, `customers`
and `catalogue_items`; `SELECT, INSERT, UPDATE, DELETE` on `documents`; `SELECT, INSERT, DELETE`
on `document_lines`; `SELECT, INSERT, UPDATE` on `document_numbers`; and the matching
`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT ON TABLES` rule to cover a later
migration's new table. `db/migrations`' own comments on `documents` and `document_lines`
explain why each one needs exactly the privileges granted there (for example, why
`document_lines` is granted `DELETE` but never `UPDATE`); that reasoning, not this list, is
what to re-check before changing either one. This chart never issues a `GRANT` itself and has
no way to: it only ever runs `db/migrations` as `migrationJob.databaseUser`.

Set `existingSecret.migrationDatabasePasswordKey` alongside `migrationJob.databaseUser` if its
password lives under a different key in the same Secret than
`existingSecret.databasePasswordKey` names; left unset, it falls back to that same key, which
only works when both roles happen to share one password.

This chart has no way to provision either role itself, or to verify what it is told, the same
reason the "Prerequisites" section above cannot enforce `config.databaseUser`'s own
`NOSUPERUSER NOBYPASSRLS` requirement from here. Three things specifically stay the cluster
operator's responsibility and this chart's render-time guard cannot see: `migrationJob.databaseUser`
not being a superuser or carrying `BYPASSRLS` (it only ever needs to own the schema it creates,
nothing more - unlike `docker-compose.yaml`'s own bootstrap role, which the official Postgres
image makes a full superuser rather than only a schema owner); `config.databaseUser` staying
`NOSUPERUSER NOBYPASSRLS`; and `config.databaseUser` never being granted ownership of
`tenants`, `accounts`, `invites`, `documents` or `document_lines` some other way after install,
directly or through membership in the role that owns them. Setting `migrationJob.databaseUser`
and `config.databaseUser` to two distinct roles, and satisfying the render-time guard above, is
necessary for the guarantee this section describes; it is not sufficient, and provisioning both
roles correctly remains the cluster operator's job either way.

Disable the migration Job outright with `migrationJob.enabled: false` for a deployment that
applies `db/migrations` some other way; see "Migrations" below. That route owns this same
two-role contract itself, outside this chart's own render-time guard, which only ever checks
`migrationJob.databaseUser` while `migrationJob.enabled` is true.

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
`migrationJob.databaseUser` (required, with no fallback to `config.databaseUser`; see
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
`config.listenAddr`/`service.port`, and a second, administrative one,
`config.adminListenAddr`/`adminPort`, serving `/metrics`, `/healthz` and `/readyz`. The
`Service` exposes both, each as its own named port (`http` and `admin`); `templates/ingress.yaml`
routes only to `service.port`, never to `adminPort`, so a caller reaching this application
through the `Ingress` still cannot scrape its metrics or poll its probes that way.

Earlier, `adminPort` was deliberately left off the `Service`, on the reasoning that this
narrowed who could reach it. That reasoning does not hold: a pod IP is routable from anywhere
inside the cluster regardless of whether a `Service` also exposes that port, so leaving it off
removed a convenient path to the admin listener, not the only path. Reaching it from inside the
cluster is a `NetworkPolicy`'s job, not the Service's absence of a port, and that policy is what
`networkPolicy` below renders: with it enforced and no admin scope configured, this policy's own
rules admit nothing on `adminPort`. That is not the same as nothing reaching it at all: a
`NetworkPolicy` only adds access, so a wider policy already selecting this release's pods in the
same namespace can still admit it. See "NetworkPolicy" below, including the additive-policy
caveat there.

`livenessProbe` and `readinessProbe` still reach `adminPort` by naming the container port
directly (`port: admin`), which a kubelet resolves against the pod's own IP, never through the
`Service`; that choice is unrelated to whether the `Service` also exposes the port. No scrape
config exists in this chart yet. A `PodMonitor` remains the better default once one is added: it
scrapes the pod IP and its named container port directly, the same way the probes already do,
needs no `Service` port of its own, and keeps working for a pod that is not yet `Ready` (not
yet behind the `Service`'s endpoints), which matters for catching a startup failure. A
`ServiceMonitor` is only the fallback for a cluster whose Prometheus Operator is not configured
to discover `PodMonitor` resources; it now has the `Service` port it would need either way.

## NetworkPolicy

`networkPolicy.enabled` defaults to `true`.

**A `NetworkPolicy` does nothing at all unless your CNI enforces `NetworkPolicy`.** A cluster
whose network plugin does not implement it accepts and stores this object with no error and no
warning, and enforces none of it. The Kubernetes documentation is explicit: "Creating a
NetworkPolicy resource without a controller that implements it will have no effect." Confirm
your own CNI enforces `NetworkPolicy` before treating anything in this section as a boundary.
That cuts both ways, and it is the whole cost of the default being on: on such a cluster this
chart renders an object that reads like protection and provides none. The reason it is on
anyway is that the alternative is worse in the common case. `adminPort` carries `/metrics`,
`/healthz` and `/readyz` and is reachable from any pod in the cluster on the pod IP, whether or
not the `Service` exposes that port, so an install that does not opt in has no boundary around
it at all. One value turns the policy off; nothing turns on a boundary nobody knew was missing.

**A second caveat, independent of the first: a `NetworkPolicy` only adds access, never removes
it.** The effective access for a pod is the union of every `NetworkPolicy` that selects it;
nothing in the API lets one policy narrow what another, wider policy already admits. Every
denial this section describes, the admin port included, holds only as long as no other
`NetworkPolicy` in this namespace also selects this release's pods with a wider ingress rule. A
namespace that already carries a broad, namespace-wide allow-ingress policy (admitting traffic
from every pod in the namespace, say) leaves `/metrics`, `/healthz` and `/readyz` reachable from
every pod in that namespace regardless of what `networkPolicy.ingress.admin` says here, and
nothing in this chart's output would say so. Check for one before relying on this chart's
denial: `kubectl get networkpolicy -n <namespace>` for every other policy in the release's
namespace, then read each one for an ingress rule that selects this release's pods (by label or
by selecting none, which matches every pod) and admits a wider `from` than the rule this chart
renders.

### What the policy selects

This chart's own pods, by the same `app.kubernetes.io/name` and `app.kubernetes.io/instance`
labels the `Deployment`'s `matchLabels` uses. That also selects the migration Job's pods, which
carry the same two labels and differ only in their component label. Deliberate: the Job needs
DNS and PostgreSQL egress and no ingress at all, a strict subset of what the rules below allow.
On a fresh install it runs before this object exists, being a pre-install hook; on an upgrade
the previous release's copy of the policy applies to it.

### Ingress rules

- The public port, `service.port`. **Unscoped by default**, and that is the documented
  behaviour rather than an omission: traffic arrives from an ingress controller or a Gateway in
  a cluster-owned namespace this chart cannot name in advance, and a public port that admits
  nothing is a broken install. Scope it with `networkPolicy.ingress.public` where you do know.
- The admin port, `adminPort`. **Admits nothing by default**: no rule for that port is rendered
  at all. This is the opposite of the public port above, deliberately, and it is not
  expressible as an empty rule, because an ingress rule carrying ports and no `from` means "any
  source" per the `NetworkPolicy` API. Omitting the rule is what denies the port, within this
  policy; see the additive-policy caveat above for what else has to hold true in the namespace
  for that to be the whole story. Set `networkPolicy.ingress.admin` to the pods that legitimately
  scrape it:

  ```yaml
  networkPolicy:
    ingress:
      admin:
        namespaceSelector:
          kubernetes.io/metadata.name: monitoring
        podSelector:
          app.kubernetes.io/name: prometheus
  ```

  `namespaceSelector` and `podSelector` land in one peer, so they are ANDed: that example
  admits a Prometheus pod in the `monitoring` namespace, not every pod in that namespace and
  not every Prometheus pod anywhere. An `ipBlock` is a peer of its own, ORed with them, because
  the API rejects a peer carrying both. The same shape applies to every scope block in this
  section.

### Kubelet probe traffic needs no rule, with one caveat

No rule admits the kubelet's `livenessProbe` and `readinessProbe` requests to `adminPort`, and
none is needed. Per the
[Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/network-policies/),
listing the entities a pod can communicate with: "traffic to and from the node where a Pod is
running is always allowed, regardless of the IP address of the Pod or the node". Probes
originate from the node the pod runs on, so they fall under that exception rather than under
the admin rule above, and they keep working with the admin port admitting nothing.

The caveat is that this is a documented property, not a conformance-tested one, and individual
CNI implementations have got it wrong: the AWS VPC CNI carried a bug where a `NetworkPolicy`
blocked liveness and readiness probes
([aws/amazon-vpc-cni-k8s#2571](https://github.com/aws/amazon-vpc-cni-k8s/issues/2571)). If
probes start failing on this chart's pods the moment the policy is enforced, that is the
failure mode, and `networkPolicy.ingress.admin.ipBlock` set to your node CIDR is the escape
hatch; it is not something this chart can default, since it cannot know that CIDR.

### Egress rules

`networkPolicy.egress.enabled` defaults to `true` and is a separate flag on purpose. The egress
half is the half whose mistakes break an installation rather than expose one: a rule whose port
or destination does not match what the application dials fails as a connection timeout at
runtime, not at render. Turning it off drops the `Egress` policy type and every rule below while
keeping the admin-port isolation that is the point of the policy. Turning the whole policy off
gives up both.

Said plainly, since a policy that scopes one rule while reading as if it scopes all of them is
worse than one that names what it still admits: **DNS is scoped by destination and by port.
PostgreSQL and the identity provider are scoped by port alone unless you set their own scope
blocks, and both default to admitting any destination on their port.** Reply traffic needs no
rule: `NetworkPolicy` is applied per connection, so responses to admitted ingress are allowed
by the ingress rules themselves.

- **DNS**, UDP and TCP port 53, scoped by `networkPolicy.egress.dns`. Both selectors default to
  kube-dns's own conventional namespace and pod label (`kube-system` / `k8s-app: kube-dns`),
  which CoreDNS also carries on most clusters for compatibility, kubeadm's default install
  included. **If your cluster's DNS pods do not carry these labels, the pod resolves no name at
  all**, so it reaches neither PostgreSQL nor the identity provider by name, and it stays
  unready with a connection error naming the database rather than DNS. A `NetworkPolicy` peer's
  `podSelector` matches pods, not the `Service` in front of them, and the two can disagree, so
  verify the actual labels before relying on the default, for example with
  `kubectl get pods -n kube-system -l k8s-app=kube-dns --show-labels`. NodeLocal DNSCache is a
  second way the default misses: a cluster running it points the pod's resolver at a per-node
  pod labelled `k8s-app: node-local-dns`, and whether that traffic is admitted then rests on
  the "traffic to and from the node is always allowed" exception above.

  Helm merges an override into the default map key by key, not key for key, and `matchLabels`
  ANDs the result: an override under a key the default does not use is added alongside the
  default rather than replacing it. `podSelector: {app: coredns}` on a cluster whose CoreDNS
  pods carry `app: coredns` therefore renders both keys, a selector matching no pod carrying
  only one of the two, and DNS stays blocked. Null the specific default key you mean to
  replace:

  ```yaml
  networkPolicy:
    egress:
      dns:
        podSelector:
          k8s-app: null
          app: coredns
  ```

  The same applies to `namespaceSelector`'s own `kubernetes.io/metadata.name` key. Set a
  selector to `null` entirely, not `{}` (Helm merges a map default with an empty override
  instead of clearing it, so `{}` silently keeps the default), to drop it and leave the rule
  scoped by port alone. That per-key `null` is what to reach for: nulling `networkPolicy.egress.dns`
  as a whole does the same thing to the entire rule, not just to one key, and does it silently.
  With no selector left at all the DNS rule still renders, scoped by port alone, admitting any
  destination on 53 rather than being removed - the one egress rule this chart otherwise keeps
  scoped by destination as well as by port.
- **PostgreSQL**, on `networkPolicy.egress.postgres.port` (default `5432`). The port is an
  explicit value rather than read out of `config.databaseUrl`, and a guard fails the render
  when the two disagree; see "Three guards" below. The destination is left to that block's own
  selectors, which **default to nothing, so this rule admits any destination on its port**. No
  default could do better: `config.databaseUrl` may name a managed PostgreSQL instance outside
  this cluster entirely, which no namespace or pod selector can describe, and a guessed
  namespace fails as a timeout rather than at render. Scope an in-cluster database with
  selectors, an off-cluster one with an `ipBlock`:

  ```yaml
  networkPolicy:
    egress:
      postgres:
        namespaceSelector:
          kubernetes.io/metadata.name: postgres
        podSelector:
          cnpg.io/cluster: solobill
  ```

  ```yaml
  networkPolicy:
    egress:
      postgres:
        ipBlock:
          cidr: 203.0.113.7/32
  ```

- **The identity provider**, on `networkPolicy.egress.oidc.port` (default `443`), rendered only
  while `config.oidc.issuerUrl` is set. With OIDC off, which is this chart's default,
  `cmd/solobill` builds no OIDC client and this policy allows no outbound HTTPS at all. Same
  destination story as PostgreSQL: **unscoped by default, admitting any destination on its
  port**, because an identity provider is as often outside the cluster as in it. An in-cluster
  Keycloak on plain HTTP behind the cluster's own TLS needs `port` set to its own, commonly
  `8080`.

There is no SMTP rule, because nothing in this application sends mail: `internal/config` has no
SMTP setting and no package builds a mail client. Mail is part of a later milestone
(`docs/concepts/solobill.md`'s document delivery), and the rule lands with it rather than now,
as a guess at a port and a destination that nothing dials.

### Four guards turn a runtime failure, a silent inversion, or a raw template crash, into a render failure

A default-deny egress policy whose allowed port does not match the port the application
actually dials does not fail at render. It fails later, as a connection that times out, with
nothing in the rendered output to point at. `templates/networkpolicy.yaml` therefore compares
the port each configured URL names against the port its own rule allows, and fails the render
naming both:

- `config.databaseUrl`'s port against `networkPolicy.egress.postgres.port`.
- `config.oidc.issuerUrl`'s port against `networkPolicy.egress.oidc.port`, only while OIDC is
  configured.

Each reads the port from the URL's own `port` query parameter first, then its explicit
host-embedded port, then its scheme's own default (`443` for `https`, `80` for `http`, `5432`
for `postgres` and `postgresql`). The query parameter wins over the host-embedded port rather
than the other way round, because that is what `pgx` (and the libpq URI format it follows)
actually does with one: `postgres://pg.svc/db?port=5433` connects on `5433`, not `5432`, even
though the host carries no port of its own. A URL whose scheme names no default and carries
neither yields no port, and the render goes ahead rather than guessing.

A URL whose port cannot be read as a number at all, a `?port=abc` query parameter or a
non-numeric host-embedded port such as `host:abc`, fails on its own guard instead, naming the
bad value, where it was found (the query parameter or the host part) and that a URL's port must
be digits only. Left to the two guards above, a non-numeric query parameter still failed the
render, but with a message built for a numeric mismatch ("set `networkPolicy.egress.postgres.port`
to `abc`"); a non-numeric host-embedded port crashed inside `urlParse` itself, aborting the whole
render with a raw Go template error instead of a message this chart authored.

A fourth guard is not about a URL at all: `service.port` and `adminPort` select two different
listeners, and the public ingress rule above, unscoped by default, renders on whichever port
number `service.port` names. Equal, that same rule now also covers `adminPort`, admitting it
from any source regardless of `networkPolicy.ingress.admin` and inverting, silently, the
boundary this policy exists to build. The render fails naming both values instead.

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
| `adminPort` | Administrative container port (`/metrics`, `/healthz`, `/readyz`). Keep in sync with `config.adminListenAddr`. Exposed on the `Service` as its own named port; the `Ingress` still never routes to it. See "Metrics and probes live on their own port" above. | `8081` |
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
| `networkPolicy.enabled` | Create a `NetworkPolicy` over this chart's own pods. On by default. **Does nothing unless your CNI enforces `NetworkPolicy`**; see "NetworkPolicy" above. | `true` |
| `networkPolicy.ingress.public.*` | Scopes who may reach `service.port`. Empty admits any source on that port. `namespaceSelector`/`podSelector` are ANDed in one peer; `ipBlock` (`cidr`, `except`) is a peer of its own, ORed with them. | `{}` |
| `networkPolicy.ingress.admin.*` | Scopes who may reach `adminPort`. Empty admits **nothing from this rule**: the rule is omitted, which is the only way to deny a port from within this policy. A wider policy already selecting these pods elsewhere in the namespace still can; see "NetworkPolicy" above for the additive-policy caveat and how to check for one. Set it to the pods that scrape `/metrics`. Kubelet probes need nothing here. | `{}` |
| `networkPolicy.egress.enabled` | Include the `Egress` half of the policy. Its own flag: turning it off keeps the admin-port isolation while dropping every egress rule, for a cluster where one of them is wrong. | `true` |
| `networkPolicy.egress.dns.*` | Destination scope for the DNS rule, on top of UDP/TCP 53. Defaults to kube-dns's conventional namespace and pod label. **Wrong labels mean the pod resolves no name at all**; see "NetworkPolicy" above for the per-key merge trap and how to replace a default key. | `kube-system` / `k8s-app: kube-dns` |
| `networkPolicy.egress.postgres.port` | Port the PostgreSQL egress rule allows. A render guard fails the install when `config.databaseUrl` names a different one. | `5432` |
| `networkPolicy.egress.postgres.*` | Destination scope for the PostgreSQL rule. Empty admits **any destination** on the port above: the database may sit outside the cluster, where no selector applies. Use `ipBlock` for that case. | `{}` |
| `networkPolicy.egress.oidc.port` | Port the identity provider egress rule allows. Same render guard against `config.oidc.issuerUrl`. | `443` |
| `networkPolicy.egress.oidc.*` | Destination scope for the identity provider rule, which renders only while `config.oidc.issuerUrl` is set. Empty admits **any destination** on the port above. | `{}` |
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
| `migrationJob.databaseUser` | Separate role that owns the schema and runs `db/migrations`, distinct from `config.databaseUser`. Required whenever `migrationJob.enabled` is true, with no fallback: see "One role, or two" above. | `""` |
| `migrationJob.image.repository` | The official dbmate image. | `ghcr.io/amacneil/dbmate` |
| `migrationJob.image.tag` | dbmate image tag, matching `docker-compose.yaml`'s own pin. | `2.38.1` |
| `migrationJob.annotations` | Extra annotations on the migration Job. | `{}` |
| `migrationJob.resources` | Resource requests/limits for the migration Job. | see `values.yaml` |

`existingSecret.name`, `config.databaseUrl` and `config.databaseUser` have no usable default and
are enforced by a `fail` guard in `templates/deployment.yaml` instead of `values.schema.json`'s
`minLength`: an install or upgrade with any of them missing or empty fails the render with a
readable message. Every other required field above is still enforced by the schema.

`migrationJob.databaseUser` is the same kind of conditionally-required field, enforced the same
way, but only while `migrationJob.enabled` is true: a `fail` guard in `templates/deployment.yaml`
fires when it is left empty or set equal to `config.databaseUser`; see "One role, or two" above.

`httpRoute.parentRefs.name` and `httpRoute.parentRefs.namespace` are the same kind of
conditionally-required field, enforced the same way: a `fail` guard in `templates/httproute.yaml`
that only fires when `httpRoute.enabled` is true. The `ingress.enabled`/`httpRoute.enabled`
mutual exclusion is its own `fail` guard, in `templates/deployment.yaml`; see "External routing"
above.

`networkPolicy.egress.postgres.port` and `networkPolicy.egress.oidc.port` are required while
`networkPolicy.egress.enabled` is true, enforced by a `required` call in
`templates/networkpolicy.yaml`, which also holds the two port-mismatch guards, the non-numeric
port guard and the `service.port`/`adminPort` collision guard; see "NetworkPolicy" above.

`config.listenAddr` and `config.adminListenAddr` are not: nothing fails the render when either
disagrees with `service.port` or `adminPort`, only the comment on each in `values.yaml` and the
"Keep in sync with" notes in the values table above. A mismatch there does not invert a boundary
the way the four guards above do, but it still breaks the pod: the `Service`, the probes and
this policy's own ingress rules all read `service.port` and `adminPort`, while `cmd/solobill`
listens on whatever `config.listenAddr` and `config.adminListenAddr` actually say, so a mismatch
means the probes and the `Service` reach a port nothing is listening on. Keep the two pairs in
sync by hand until this has a guard of its own.

## Upgrading

A plain `helm upgrade` is safe for a values change on a release already running this chart
version or later. It is not automatically safe for an existing release upgrading onto this
version from an earlier one: `networkPolicy.enabled` defaults to `true`, so that upgrade turns
on a default-deny ingress and egress boundary the previous release did not have, on an already
running Deployment. Check both of the following first:

- **DNS.** `networkPolicy.egress.dns` defaults to kube-dns's conventional namespace and pod
  label. A cluster running NodeLocal DNSCache points pods at a per-node pod labelled
  `k8s-app: node-local-dns` instead, which that default does not select: the rolled-out pods
  resolve no name at all, readiness never passes, and the rollout stalls. Verify your cluster's
  actual DNS pod labels before upgrading, for example with
  `kubectl get pods -n kube-system -l k8s-app=kube-dns --show-labels`; see "NetworkPolicy" above,
  "Egress rules", for the full reasoning and the per-key override shape.
- **Admin-port scraping.** If anything already reaches `adminPort` today, from Prometheus or
  otherwise, that scrape stops the moment this policy is enforced, with no error anywhere in
  this chart's own output: `networkPolicy.ingress.admin` admits nothing until it is set. Set it
  to that scraper's pods before or in the same upgrade that turns the policy on; see
  "NetworkPolicy" above.

Once both check out, the migration Job re-applies `db/migrations` (a no-op when nothing
changed) before the Deployment is updated, and there is no other persistent state in the chart
itself: every durable fact lives in your own PostgreSQL instance, outside this release.

## Uninstalling

```sh
helm uninstall solobill
```

Removes every ordinary object this chart created: the `Deployment`, `Service`, the
`NetworkPolicy` if `networkPolicy.enabled` was left on, and the `Ingress` or `HTTPRoute`,
whichever was enabled. The `ServiceAccount`, the migration Job and its
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

- `helm lint`, over the default values and every documented non-default combination:
  OIDC-enabled, ingress-enabled, httpRoute-enabled, `NetworkPolicy` fully scoped, and
  `NetworkPolicy` with its egress half off
- `helm template`, over a must-fail set: one case per `fail` and `required` guard in the
  chart's templates, asserting both that the render fails and that it fails with that guard's
  own message. This set is deliberately not run through `helm lint`: a template's `fail` or
  `required` call is only logged by `helm lint`, which still reports no failed chart and exits
  zero, so a lint pass proves nothing about a guard built on either
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
