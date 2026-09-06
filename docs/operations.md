# Rosmarinus Operations

## MongoDB and Redis

`MONGO_URI` and `MONGO_DATABASE` select Rosmarinus's durable federation store.
Startup requires a writable primary and bootstraps indexes only for
Rosmarinus-owned collections. Use the collection-scoped `rosmarinusService`
role documented in `salvia-integration.md`; Salvia has no MongoDB role. Back up
the database consistently with the deployment's normal MongoDB snapshot or
replica-set procedure because Actors, signing keys, API idempotency receipts,
and federation relationships are not reconstructible from Redis.

`REDIS_ADDR`, `REDIS_PASSWORD`, and `REDIS_DB` select the Asynq queue, AP locks,
rate-limit buckets, bounded caches, and local SSE Pub/Sub fan-out. Use a
dedicated Redis database or instance and enable persistence suitable for queued
federation work. Pub/Sub is deliberately best-effort: losing a browser event
requires REST reconciliation, not event replay. Losing Redis may also discard
pending deliveries, but MongoDB receipts and unique indexes still prevent
replayed inbound activities from duplicating completed domain side effects.
Never use broad key deletion or database flushes as routine queue maintenance;
use the inspection and promotion commands below.

## Actor ID migration

`go run ./cmd/migrateactorids` inventories Actor IDs and every known internal
Actor-ID reference without writing. With a current backup available, rerun it
as `go run ./cmd/migrateactorids --apply` to replace non-ObjectID IDs and
references in one MongoDB transaction. The command reads `MONGO_URI` and
`MONGO_DATABASE`, records the old-to-new mapping in `migration_audits`, and
verifies that the migrated fields contain only ObjectID hexadecimal strings.

The migration preserves public ActivityPub URIs. Migrated Actor documents keep
their former IDs in the indexed `legacyIds` compatibility field so inbound
requests to an established `/users/{oldId}` URI continue to resolve. Deploy the
matching Rosmarinus build before migrating: it uses the new Actor cache
namespace and understands these aliases. Do not remove `legacyIds`; remote
servers may retain an Actor URI indefinitely.

Rosmarinus verifies MongoDB and Redis connectivity before serving traffic.
Shutdown allows up to 30 seconds for workers and network servers to stop. The
TTL indexes on inbox and API idempotency receipts provide intentional bounded
retention. Actor, Note, relationship, and signing-key data require normal
deployment backups rather than TTL cleanup.

## Queue capacity

Rosmarinus runs one Asynq server by default and applies Redis-backed global
per-second limits before federation handlers run. The defaults follow current
Misskey:

```text
INBOX_CONCURRENCY=16
INBOX_RATE_PER_SECOND=32
DELIVER_CONCURRENCY=128
DELIVER_RATE_PER_SECOND=128
```

`INBOX_MAX_RETRY`, `DELIVER_MAX_RETRY`, `INBOX_TIMEOUT`, and
`DELIVER_TIMEOUT` control retry and execution limits independently. All
concurrency and rate values must be positive. Redis rate buckets are shared by
split worker processes using the same Redis database, so adding processes does
not multiply the configured federation request rate.

## Inbox activity receipts

After signature verification, inbox workers claim the Activity URI in the
MongoDB `inbox_activity_receipts` collection. A processing lease prevents
concurrent execution, and a completed receipt prevents sequential peer retries
or promoted queue jobs from repeating side effects. `INBOX_ACTIVITY_RECEIPT_TTL`
controls retention and defaults to `168h`; it must exceed `INBOX_TIMEOUT` plus
the one-minute lease margin. MongoDB removes expired receipts through the
`expiresAt` TTL index.

Handler mutations still use their own unique keys because a crash can happen
after a mutation but before its receipt is completed. If processing fails, the
worker releases only its token-matched lease so an Asynq retry can safely
resume. This receipt collection is internal to Rosmarinus and does not change
the Salvia REST or SSE contract.

## Salvia authentication, REST, and SSE

Passkey and session policy is configured with `SESSION_COOKIE_NAME`,
`SESSION_TTL`, `SESSION_COOKIE_SECURE`, `WEBAUTHN_RP_ID`,
`WEBAUTHN_RP_NAME`, `WEBAUTHN_ALLOWED_ORIGINS`, and
`WEBAUTHN_CEREMONY_TTL`. `AUTH_RATE_LIMIT` and `AUTH_RATE_WINDOW` apply a
Redis-backed fixed-window limit to setup and login ceremonies.
`API_IDEMPOTENCY_TTL` controls completed REST mutation receipt retention.

Serve Salvia under the same HTTPS origin as Rosmarinus. Browsers use `/api/v1`
for REST and `GET /api/v1/events` for authenticated SSE; they never receive
Redis credentials or channel names. The SSE stream is an invalidation channel,
not a durable log. Clients re-read REST views after reconnect or an ambiguous
mutation result.

The real-Misskey federation workflow includes Phase 24, which publishes an
account-scoped event through one Redis broker instance and verifies delivery to
the matching subscription on another while a different account receives
nothing. Run that workflow when changing Redis configuration, realtime event
fan-out, or the MongoDB role bootstrap.
Phase 15 stores an original Salvia image without server-side transformation,
attaches its Actor-owned media ID to a local Note, and verifies that current
Misskey receives the resulting attachment. Run it when changing local upload,
media ownership, or outbound attachment rendering.

## Outbound network safety

ActivityPub object fetches and deliveries resolve hosts locally, reject every
non-public DNS answer, and connect to the validated address without using an
environment proxy. The same private-address boundary applies to media and
instance metadata fetches. Redirect targets are revalidated before they are
followed.

`MEDIA_ALLOWED_PRIVATE_NETWORKS` is the historical name of the shared CIDR
allowlist for these remote HTTP clients. Leave it empty in production. Set it
only for controlled private federation topologies such as the Docker
compatibility fixture; allowing a network makes any service on that network a
possible federation target.

## Failed task inspection

Asynq archives a task after its configured retries are exhausted. Inspect the
latest archived tasks as JSON without starting MongoDB or the application
server:

```sh
rosmarinus queue failed deliver
rosmarinus queue failed inbox 100
```

The optional limit is between 1 and 100 and defaults to 30. Each record includes
the task ID, type, versioned payload, retry counts, last error, and failure
timestamp. Treat payloads as operational data: inbox payloads contain remote
HTTP Signature material and activities may contain private federation content.

## Promoting federation work

Move an archived, retry-delayed, or scheduled inbox/deliver task back to the
pending queue with its Asynq task ID:

```sh
rosmarinus queue promote deliver TASK_ID
rosmarinus queue promote inbox TASK_ID
```

Promotion preserves the existing task payload. Only the `inbox` and `deliver`
queues are accepted by this command. Inspect the failure first and avoid
re-running a task whose target or authorization boundary is no longer valid.
