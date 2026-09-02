# Salvia Integration Contract

## Architecture

Salvia is a static React SPA and Rosmarinus is its only backend. The former
Next.js backend, shared-database frontend access, and Ably Connector are legacy
architecture and must not receive new features.

```text
Salvia SPA
  | same-origin HTTPS
  |-- passkey/session endpoints
  |-- versioned JSON application API
  `-- authenticated server event stream
             |
             v
       Rosmarinus backend
         |          |
      MongoDB     Redis
                  queues, locks, rate limits, local Pub/Sub
```

MongoDB is the durable source of truth. Redis Pub/Sub carries best-effort
invalidation messages between local Rosmarinus processes so any instance can
refresh an attached browser. It is not durable and is never exposed directly
to the SPA. Browser reconnect always reconciles through API reads.

Rosmarinus may serve the SPA build or run behind a same-origin reverse proxy.
SPA history fallback must run only after application/API and federation routes
have been excluded.

## Ownership

Rosmarinus is the sole runtime writer and migration/index owner for all durable
collections, including:

- accounts, passkey credentials, WebAuthn challenges, sessions, and UI/Actor
  settings;
- local and remote Actors, key material, Notes, Polls, Follow relationships,
  reactions, blocks, emoji, instances, reports, and notifications; and
- delivery, queue-domain, idempotency, and operational records.

Salvia has no MongoDB user and no Redis credentials. It receives only
browser-safe projections through Rosmarinus APIs and must not depend on BSON
shapes as a frontend contract.

The legacy `salvia_accounts`, `salvia_sessions`, `salvia_ui_settings`,
`salvia_actor_settings`, and `connector_command_receipts` collections require
an explicit migration decision before removal. Preserve stable account IDs
during migration so existing Actor `ownerAccountId` values do not change.
Retire the old cross-service MongoDB roles only after migration has been
verified and rolled out.

## Passkey-Only Authentication

- Passkeys/WebAuthn are the only authentication method. Passwords, password
  reset, recovery passwords, and TOTP are outside the contract.
- When no account exists, Rosmarinus permits one initial administrator setup.
  The eligibility check and completed account creation must use a
  database-enforced atomic guard.
- Initial account creation completes only after successful passkey registration.
  Once an account exists, public setup and registration are rejected by the
  backend regardless of which SPA route is shown.
- Rosmarinus creates short-lived, single-use challenges bound to the account or
  bootstrap attempt, ceremony type, RP ID, allowed origin, and expiry.
- Rosmarinus verifies the WebAuthn response, user verification, signature, and
  credential counter behavior before changing account or session state.
- Successful authentication creates a revocable server-side session referenced
  by a random secure, HTTP-only, same-site cookie. Store only a digest of the
  session token in MongoDB.
- Rotate session identity after authentication and authorization changes.
  Suspended or deleted accounts cannot create sessions, mutate state, or open
  an event stream.
- Cookie-authenticated mutations require CSRF protection in addition to
  same-site cookie policy. Authentication and setup endpoints are rate-limited
  with Redis-backed controls.

The SPA may call browser WebAuthn APIs, but it never verifies a ceremony,
stores passkey private material, or receives session/database secrets.

## Account And Actor Authorization

An account is an authenticated local administrative identity. An Actor is a
local federated identity. One account may own multiple Actors; one user-managed
local Actor has exactly one `ownerAccountId`. Remote Actors and the optional
environment-provisioned system Actor have no user owner.

Actor-scoped handlers must load the Actor with a single ownership-aware filter
equivalent to:

```text
_id = requested Actor
ownerAccountId = authenticated account
host = null
isSuspended = false
deletedAt = null
```

The route or request Actor ID selects an acting identity but never proves
authorization. The API does not accept an account ID, username, session ID, or
client-provided ownership field as authorization evidence. Actor creation
derives `ownerAccountId` from the authenticated session. Ordinary API calls do
not transfer ownership.

Account suspension immediately revokes effective API and event-stream access
and triggers Rosmarinus's reversible Actor suspension policy. Account deletion
permanently tombstones owned Actors through the existing federation cleanup
path. These lifecycle changes are Rosmarinus-owned transactions/workflows; the
SPA never edits Actor state directly.

## Application API

The application API is versioned independently of ActivityPub. Its concrete
route and schema inventory must be finalized before implementation, using
`/api/v1` as the default namespace. It returns JSON projections designed for
the SPA, not raw MongoDB documents or a public Misskey-compatible API.

The first API milestone must cover:

- bootstrap status, passkey registration/authentication ceremonies, current
  session, and logout;
- owned Actor list/create/read/update/delete and Actor-scoped settings;
- public/home timelines, Note detail and thread/quote projections;
- post create/delete, poll vote, reaction create/delete;
- follow create/delete and mandatory inbound follow approve/reject;
- block create/delete;
- account- and Actor-scoped notifications and mark-read; and
- safe profile, follower/following, emoji, instance, and settings projections.

Use stable cursor pagination with deterministic tie-breakers. Enforce Note
visibility, block state, account ownership, and Actor ownership in backend
queries before projection. In particular, `specified` Note visibility is based
on its explicit allowed Actor URI set, not its mention list.

Mutations that can cause federation side effects accept an opaque idempotency
key scoped to the authenticated account and operation. A duplicate request with
the same validated intent returns the stored outcome without repeating the side
effect; reuse with different intent is rejected. Internal errors are logged
with correlation context and returned as stable public error codes.

Use conventional status semantics:

- `400` for malformed input;
- `401` for no valid session;
- `403` for an authenticated account lacking access;
- `404` when hiding existence is required or a resource is absent;
- `409` for state/idempotency conflicts; and
- `429` for rate limits.

Error bodies and successful projections must be runtime-validatable by the SPA
and versioned when compatibility cannot be preserved.

## Authenticated Event Stream

Rosmarinus exposes a same-origin, session-authenticated server-to-browser event
stream. Server-Sent Events is the default transport because browser commands
use HTTP; WebSocket requires a separately documented need and equivalent
origin/session/CSRF protections.

Events use a small versioned envelope:

```json
{
  "version": 1,
  "type": "notification.created",
  "event_id": "opaque-id",
  "actor_id": "optional-local-actor-id",
  "occurred_at": "RFC3339 timestamp",
  "data": {}
}
```

- Authenticate the session before opening the stream and re-check account
  status during long-lived connections.
- Publish only to the authenticated account. Include an Actor ID where it
  narrows invalidation; never trust a client-supplied subscription account.
- Keep payloads minimal and browser-safe. Prefer resource IDs and projection
  invalidation over copying federation documents into Pub/Sub.
- Use per-account Redis channel derivation exclusively inside Rosmarinus. Do
  not document internal channel names as a browser contract.
- Treat Redis delivery as at-most-best-effort. Slow clients are disconnected or
  coalesced rather than allowed to block domain processing.
- Duplicate or missed events are valid. The SPA re-reads API state on reconnect
  and after ambiguous outcomes; event receipt is not durable business success.

Initial event types should cover Actor lifecycle, Note changes, reaction
changes, notification creation/read state, follow approval state, account
authorization changes, and generic projection invalidation. Event additions
may be backward-compatible; envelope or meaning changes require a new version.

## Federation Projection Safety

Rosmarinus remains responsible for remote object validation and returns only
the fields required by each UI view. The SPA must treat all remote content and
URLs as untrusted.

- Preserve remote Actor move state only after Rosmarinus has validated the
  reciprocal alias relationship.
- Hide soft-deleted Notes immediately and tolerate dependent reactions, polls,
  votes, and notifications converging through retryable cleanup.
- Derive reply, renote, and reaction counts from normalized active records; do
  not let the client write cached federation counters.
- Keep poll choice ordering aligned with vote-count indexes and authorize local
  vote projections by the selected owned Actor.
- Return validated HTTPS media/emoji/avatar/banner URLs as untrusted remote
  resources. Do not forward cookies or authorization headers to them.
- Sanitize or convert ActivityPub HTML server-side. The SPA must not introduce
  a raw-HTML escape hatch for remote content.

## Runtime Configuration

The integrated target removes all `ABLY_*` and legacy Connector channel
variables. Rosmarinus configuration must add environment-backed values for:

- WebAuthn RP ID, RP display name, and allowed origins;
- session cookie name, lifetime, signing/encryption material if required, and
  secure-cookie policy;
- challenge/session retention and authentication rate limits;
- application API and event-stream timeouts/limits; and
- internal Redis Pub/Sub namespace and subscriber buffer limits.

Exact variable names are finalized with implementation. Defaults must be safe,
secrets must fail closed when missing, and no secret may enter the SPA bundle.

## Migration And Verification

1. Add Rosmarinus-owned account/passkey/session schemas and indexes.
2. Migrate stable account identity and Actor ownership references from legacy
   Salvia collections, with rollback-safe validation before cutover.
3. Implement and test passkey/session APIs, application endpoints, and the
   authenticated event stream while legacy Connector traffic is still
   isolated.
4. Move the React SPA to the HTTP/event contracts and verify feature parity for
   every existing domain command.
5. Disable and remove Ably subscribers/publishers, SDKs, environment variables,
   command receipts that are no longer needed, and cross-service MongoDB roles.
6. Remove the Next.js deployment after the static SPA and integrated backend
   pass end-to-end, security, and federation tests.

Required negative tests include concurrent initial setup, expired/replayed
WebAuthn challenges, invalid origin/RP ID, stolen/expired sessions, CSRF,
cross-account Actor IDs, suspended/deleted accounts, idempotency conflicts,
event-stream account isolation, slow clients, duplicate/missed Pub/Sub events,
and SPA history fallback over API/federation paths.
