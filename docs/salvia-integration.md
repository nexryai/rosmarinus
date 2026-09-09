# Salvia Integration Contract

## Architecture

Salvia is a static React SPA and Rosmarinus is its only backend. The former
Next.js backend, shared-database frontend access, and Ably Connector are legacy
architecture and must not receive new features.

```text
Salvia SPA
  | same-origin HTTPS
  |-- passkey/session endpoints
  |-- versioned REST API
  `-- authenticated SSE
             |
             v
       Rosmarinus backend
         |          |
      MongoDB     Redis
                  queues, locks, rate limits, local Pub/Sub
```

MongoDB is the durable source of truth. Redis Pub/Sub carries best-effort
invalidation messages between local Rosmarinus processes so any instance can
refresh an attached browser through SSE. It is not durable and is never
exposed directly to the SPA. Browser reconnect always reconciles through REST
reads.

The production Salvia build is generated in `internal/salvia/dist`, embedded in
the Rosmarinus executable, and served by the Rosmarinus HTTP server. Hashed
assets use immutable caching; `index.html` uses revalidation and acts as the
browser history fallback. Explicit REST, ActivityPub, WebFinger, NodeInfo,
inbox, Actor, Note, emoji, follow, and media routes always take precedence.
Missing assets and unknown routes that explicitly request JSON or ActivityPub
receive `404` instead of the SPA fallback. A reverse proxy may front the single
binary but must preserve this same-origin routing behavior.

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

The deployment bootstrap grants only the `rosmarinusService` MongoDB role.
Rosmarinus owns `accounts` with embedded passkey credentials,
`webauthn_challenges`, `sessions`, `ui_settings`, `actor_settings`, and
`api_idempotency_receipts`; no Salvia database role is created.

Rosmarinus does not read legacy `salvia_*` collections at runtime. A deployment
that contains legacy account or settings data must migrate it offline before
cutover. Preserve stable account IDs so existing Actor `ownerAccountId` values
do not change. Legacy sessions are not accepted by the integrated backend and
users authenticate again with a migrated passkey credential.

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
  an SSE stream.
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

All Rosmarinus-owned entity IDs are opaque strings at the REST and SSE
boundary. New Account, Actor, Note, media, relationship, reaction,
notification, poll, vote, session, receipt, and other persisted record IDs are
collision-checked MongoDB ObjectIDs encoded as 24 lowercase hexadecimal
characters and stored as BSON strings rather than BSON ObjectID values.
Protocol-owned identifiers such as ActivityPub URIs and WebAuthn credential
IDs remain in their protocol representation and are not database entity IDs.
Salvia must not parse ObjectID timestamps, infer locality or resource type from
an ID, or construct IDs. Rosmarinus determines locality from Actor state such
as `host`; migrated public Actor, Note, reaction, notification, and media
lookups continue to accept indexed legacy aliases where those IDs were part of
an established route.

Account suspension immediately revokes effective REST and SSE access
and triggers Rosmarinus's reversible Actor suspension policy. Account deletion
permanently tombstones owned Actors through the existing federation cleanup
path. These lifecycle changes are Rosmarinus-owned transactions/workflows; the
SPA never edits Actor state directly.

## REST API

The REST API is versioned independently of ActivityPub under `/api/v1`. It
returns JSON projections designed for the SPA, not raw MongoDB documents or a
public Misskey-compatible API. JSON responses contain top-level `version: 1`;
successful resource responses use `data`, and paginated responses also use an
opaque `next` cursor.

The first REST milestone must cover:

- bootstrap status, passkey registration/authentication ceremonies, current
  session, and logout;
- owned Actor list/create/read/update/delete and Actor-scoped settings;
- public/home timelines, Note detail and thread/quote projections;
- post create/delete, poll vote, reaction create/delete;
- follow create/delete and mandatory inbound follow approve/reject;
- block create/delete;
- account- and Actor-scoped notifications and mark-read; and
- safe profile, follower/following, emoji, instance, and settings projections.

### Implemented authentication endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/auth/setup` | Return whether initial administrator setup is required |
| `POST` | `/api/v1/auth/setup/start` | Reserve the sole initial account and create passkey registration options |
| `POST` | `/api/v1/auth/setup/finish` | Consume and verify the registration ceremony, activate the account, and create a session |
| `POST` | `/api/v1/auth/login/start` | Create discoverable passkey assertion options |
| `POST` | `/api/v1/auth/login/finish` | Consume and verify the assertion and create a session |
| `POST` | `/api/v1/auth/logout` | Revoke the current session and expire its cookie |
| `GET` | `/api/v1/session` | Return the authenticated account projection and CSRF token |

The two finish endpoints receive the standard WebAuthn credential JSON body
and require `X-WebAuthn-Ceremony-ID` from their matching start response.
Ceremonies are short-lived, stored server-side, and atomically consumed before
verification. A finish request therefore cannot be replayed. Successful setup
or login sets a random HTTP-only session cookie; only its SHA-256 digest is
stored in MongoDB.

### Implemented Actor and mutation endpoints

| Method | Path | Domain operation |
| --- | --- | --- |
| `GET`, `POST` | `/api/v1/actors` | List owned Actors or create one |
| `GET`, `PATCH`, `DELETE` | `/api/v1/actors/{actorId}` | Read, update, or delete an owned Actor |
| `POST` | `/api/v1/actors/{actorId}/posts` | Create a Note, Poll, or pure renote |
| `POST` | `/api/v1/actors/{actorId}/media` | Store an original image and its browser-generated WebP thumbnail |
| `DELETE` | `/api/v1/actors/{actorId}/posts/{noteId}` | Delete an owned Note |
| `POST` | `/api/v1/actors/{actorId}/poll-votes` | Vote in a Poll |
| `PUT`, `DELETE` | `/api/v1/actors/{actorId}/reactions/{noteId}` | Create/replace or remove a reaction |
| `POST`, `DELETE` | `/api/v1/actors/{actorId}/follows` | Follow or unfollow the body `target` |
| `POST` | `/api/v1/actors/{actorId}/profiles/resolve` | Resolve a remote Actor handle or URL and return its safe profile |
| `POST`, `DELETE` | `/api/v1/actors/{actorId}/blocks` | Block or unblock the body `target` |
| `PATCH` | `/api/v1/actors/{actorId}/follow-requests/{followerId}` | Set `status` to `accepted`, `rejected`, or `rejected_and_blocked` |
| `PATCH` | `/api/v1/actors/{actorId}/notifications/{notificationId}` | Set `is_read` to `true` |

Every endpoint is session-scoped. Mutations require `X-CSRF-Token` and an
opaque 16–200 character `Idempotency-Key`. Receipt keys are scoped to the
authenticated account; reusing one for a different operation or Actor returns
`409`. Request JSON is limited to 1 MiB, rejects unknown fields, and cannot
supply account ownership. Actor responses explicitly omit owner IDs, public
key material, private keys, inboxes, and internal delivery state.

Block and unblock mutations apply the remote target to every non-deleted local
Actor owned by the authenticated account. Rosmarinus stores the relationship
for suspended owned Actors as well, but sends ActivityPub `Block` or
`Undo(Block)` only from active Actors. The resulting `block.changed` SSE event
omits `actor_id` because every selected Actor projection may have changed.
The safe Actor projection includes `moved_to_uri` and `is_suspended` so Salvia
can show migration and moderation state without reading federation or database
records directly.

Remote profile resolution accepts `{ "target": "@user@example.com" }` (also
without the leading `@`) or an HTTP(S) Actor URL. It verifies ownership of the
Actor in the path before WebFinger or ActivityPub network access, applies the
configured federation and private-network policies, persists only the validated
remote Actor cache, and returns the same safe, viewer-specific profile projection
as `GET /api/v1/profiles/{actorId}`. The authenticated POST requires CSRF.

Image upload uses `multipart/form-data` fields `file`, `thumbnail`, `width`,
and `height`. `thumbnail` must be the WebP derivative generated by Salvia's
Canvas flow; `file` remains the unmodified original. Rosmarinus validates byte
limits and detected media types, stores both blobs without decoding or
transforming them, and returns an Actor-owned media ID. Note creation accepts
at most four `media_ids` and rejects IDs not owned by the posting Actor.

`POST /api/v1/actors/{actorId}/posts` does not accept `note_id`. Rosmarinus
allocates the Note ID, checks it against the Note collection, derives the local
ActivityPub URI from it, and returns the stored ID as `data.note_id`. The
idempotency key identifies a logical retry; it is not used as the Note ID.

### Implemented read and settings endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/timelines/public?actor_id={viewer}` | Public timeline filtered for the selected owned Actor |
| `GET` | `/api/v1/timelines/home?actor_id={viewer}` | Home timeline for the selected owned Actor |
| `GET` | `/api/v1/notes/{noteId}?actor_id={viewer}` | Visibility-checked Note detail with reply/quote/renote projections |
| `GET` | `/api/v1/notes/{noteId}/thread?actor_id={viewer}` | Visibility-checked direct replies |
| `GET` | `/api/v1/profiles/{actorId}?actor_id={viewer}` | Safe local or remote Actor profile with visible pinned Notes |
| `GET` | `/api/v1/profiles/{actorId}/notes?actor_id={viewer}` | Cursor-paginated, visibility- and block-filtered Notes authored by the profile Actor |
| `GET` | `/api/v1/profiles/{actorId}/followers?actor_id={viewer}` | Block-filtered followers |
| `GET` | `/api/v1/profiles/{actorId}/following?actor_id={viewer}` | Block-filtered following list |
| `GET` | `/api/v1/actors/{actorId}/followers` | Followers of an owned Actor |
| `GET` | `/api/v1/actors/{actorId}/following` | Actors followed by an owned Actor |
| `GET` | `/api/v1/actors/{actorId}/follow-requests` | Pending requests for an owned Actor |
| `GET` | `/api/v1/actors/{actorId}/notifications` | Actor-scoped notifications |
| `GET` | `/api/v1/notifications` | Account-scoped notifications |
| `GET` | `/api/v1/emojis` | Local custom emoji catalog |
| `GET` | `/api/v1/instance` | Browser-safe instance metadata |
| `GET`, `PATCH` | `/api/v1/settings` | Account UI settings |
| `GET`, `PATCH` | `/api/v1/actors/{actorId}/settings` | Per-Actor UI and compose defaults |

`actor_id` on read routes is the viewing identity. It is required and must be
an active local Actor owned by the authenticated account; it is never accepted
as proof of ownership. Note queries enforce visibility and bilateral blocks
before projection. The public and home timelines, profile Note lists, Note
threads, and notifications use opaque created-time/ID cursors. Connection and
emoji cursors are likewise opaque to the SPA even where their current
representation is a stable record ID or name.

Note detail, timeline, profile Note, and thread responses project reply,
quote, and renote references with the referenced author's safe profile fields
plus the custom emoji and attachment metadata required for in-card rendering.
Raw federation documents
and internal attachment URIs are never returned.
Renote references include one visibility-checked `quote` reference when their
target is itself a quote Note, allowing Salvia to retain the quoted card without
an unbounded recursive projection.

Actor projections include `emojis`, an array of
`{ "name": string, "url": string, "media_type"?: string }` references resolved
for that Actor's display name and biography. Note `text` and Actor `summary`
are sanitized MFM source strings; Salvia parses them with the shared MFM
renderer and never inserts them as HTML. Actor display names use the separate
custom-emoji text renderer, which expands `:name:` codes from `emojis` but
intentionally leaves MFM syntax literal.

Each Note reaction summary contains `reaction`, `count`, `reacted`, and an
optional `emoji` reference with the same shape. Reaction notifications contain
the concrete `reaction` value and an optional `reaction_emoji` reference.
Rosmarinus stores the emoji metadata received with a federated reaction so the
notification and Note remain renderable even when the reaction code contains
a remote host suffix. Legacy reaction rows are resolved against the validated
emoji catalog when possible and otherwise retain their literal reaction code.

Profile projections include viewer-specific `follow_status` and
`blocked_by_viewer`, plus `pinned_notes` in the Actor's featured order. Pinned
Notes pass the same visibility, deletion, active-author, and bilateral-block
filters as other Note reads and use the normal safe Note projection. A profile
blocked by the viewer remains readable so the viewer can reverse their own
block; a profile that has blocked the viewer is still hidden.

Actor `profile_fields` (including timeline authors and referenced Notes) is
an array of `{ "name": string, "value": string }`, empty when unset. Salvia
also normalizes the legacy `{ "Name": string, "Value": string }` response
at its API boundary so remote authors with profile fields do not prevent an
entire timeline page from loading during upgrades.

Account settings expose `theme`, `reduce_motion`, `compact_mode`, and
`selected_actor_id`. Supported themes are `yellow`, `light`, `dark`, and
`system`; yellow is the default. Actor settings expose `default_visibility`,
`show_content_warning`, `display_order`, `color`, and `pinned`. A selected
Actor and every Actor settings request are ownership-checked server-side.

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

## Authenticated SSE

Rosmarinus exposes `GET /api/v1/events` as a same-origin,
session-authenticated Server-Sent Events (SSE) stream for server-to-browser
updates. Browser requests and mutations use REST; WebSocket is not part of
this integration contract. The stream does not provide durable replay and
ignores `Last-Event-ID`; reconnecting clients reload canonical REST views.

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

- Authenticate the session before opening SSE and re-check account
  status during long-lived connections.
- Publish only to the authenticated account. Include an Actor ID where it
  narrows invalidation; never trust a client-supplied subscription account.
- Emit `note.created` after both local Note creation and a newly accepted
  ActivityPub `Create(Note)` or `Announce`. Public remote Notes invalidate each
  account with no `actor_id`; non-public remote Notes are sent only to accounts
  whose active local Actor can receive them, with that local Actor ID. The
  `data.note_id` value is the stored Rosmarinus Note ID.
- Keep payloads minimal and browser-safe. Prefer resource IDs and projection
  invalidation over copying federation documents into Pub/Sub.
- Use per-account Redis channel derivation exclusively inside Rosmarinus. Do
  not document internal channel names as a browser contract.
- Treat Redis delivery as at-most-best-effort. Slow clients are disconnected or
  coalesced rather than allowed to block domain processing.
- Duplicate or missed events are valid. The SPA re-reads REST state on reconnect
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
- Return MFM as source text plus explicit custom-emoji references. Salvia must
  parse that source into React elements, validate link protocols and style
  arguments, respect reduced-motion preferences, and keep unknown syntax as
  visible text rather than using a raw-HTML rendering path.
- Rosmarinus does not decode, resize, crop, transcode, optimize, or generate
  thumbnails for images and remains buildable with `CGO_ENABLED=0`. Salvia
  generates upload previews, thumbnails, and other required derivatives in the
  browser with Canvas APIs.
- Sanitize or convert ActivityPub HTML server-side. The SPA must not introduce
  a raw-HTML escape hatch for remote content.

## Runtime Configuration

The integrated backend has no `ABLY_*` or legacy Connector channel variables.
Authentication and API behavior use these environment-backed values:

- `SESSION_COOKIE_NAME`, `SESSION_TTL`, and `SESSION_COOKIE_SECURE`;
- `WEBAUTHN_RP_ID`, `WEBAUTHN_RP_NAME`,
  `WEBAUTHN_ALLOWED_ORIGINS`, and `WEBAUTHN_CEREMONY_TTL`;
- `AUTH_RATE_LIMIT` and `AUTH_RATE_WINDOW`; and
- `API_IDEMPOTENCY_TTL`.

`REDIS_ADDR`, `REDIS_PASSWORD`, and `REDIS_DB` configure queues, locks, rate
limits, caches, and internal Pub/Sub. Pub/Sub channel derivation and subscriber
buffer sizes are backend implementation details, not deployment or browser
contracts. Defaults must be safe, secrets must fail closed when missing, and
no secret may enter the SPA bundle.

## Migration And Verification

1. Rosmarinus-owned account/passkey/session/settings/idempotency schemas and
   indexes are implemented for fresh deployments.
2. The versioned REST API, passkey/session endpoints, authenticated SSE, and
   Redis Pub/Sub fan-out are implemented; Ably code, SDKs, configuration, and
   cross-service MongoDB roles have been removed.
3. Deployments with legacy Rosmarinus data must run `go run ./cmd/migrateids`
   as a dry run and then, with a current backup and writers stopped,
   `go run ./cmd/migrateids --apply`. The migration converts persisted entity
   IDs and internal references to ObjectID strings while retaining public
   protocol URIs and route aliases.
4. Move the React SPA to the HTTP/event contracts and verify feature parity for
   every user-facing workflow.
5. Remove the old Next.js deployment only after the static SPA and integrated
   backend pass end-to-end, security, and federation tests.

Required negative tests include concurrent initial setup, expired/replayed
WebAuthn challenges, invalid origin/RP ID, stolen/expired sessions, CSRF,
cross-account Actor IDs, suspended/deleted accounts, idempotency conflicts,
SSE account isolation, slow clients, duplicate/missed Pub/Sub events,
and SPA history fallback over API/federation paths.
