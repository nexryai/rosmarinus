# Salvia Next.js Application

This file is a handoff artifact for the separate Salvia repository. Copy it to
the Salvia repository root as `AGENTS.md` before implementing Salvia. Paths and
commands in this file refer to that repository unless explicitly stated
otherwise.

Do not add Salvia application code to the Rosmarinus repository. Do not modify
Rosmarinus as part of a Salvia task unless the task explicitly covers both
repositories and each repository receives its own coherent commit.

## Product Scope

Salvia is the browser application and Next.js backend for Rosmarinus. Salvia is
responsible for:

- authenticating local users and maintaining their application sessions;
- mapping each local account to one stable Ably `clientId`;
- issuing short-lived, narrowly scoped Ably credentials to authenticated
  browsers;
- presenting and updating browser UI state;
- reading federation state written by Rosmarinus;
- publishing authenticated commands to Rosmarinus over Ably; and
- consuming Rosmarinus result and domain events over Ably.

Salvia is not an ActivityPub server. It must not implement ActivityPub inboxes,
outboxes, HTTP Signatures, federation delivery, remote object resolution, or
local Actor key management.

## Rosmarinus Responsibilities

Rosmarinus is an ActivityPub microservice with no frontend and no public
Misskey-compatible API. It owns:

- ActivityPub protocol endpoints and federation compatibility;
- inbound ActivityPub parsing and HTTP Signature verification;
- outbound ActivityPub signing, delivery, retries, and queue processing;
- resolution and persistence of remote Actors and other remote objects;
- lifecycle and key material of local Actors;
- federation state such as notes, follows, reactions, blocks, and delivery
  records;
- authorization of every Salvia command against the authenticated Ably
  `clientId`, its local account, and the selected Actor; and
- command receipts, command results, and federation-related UI events.

Rosmarinus does not authenticate a browser user or create a Salvia session. It
accepts an Ably message identity only after resolving `clientId` through the
Salvia-owned account collection and checking Actor ownership itself.

## Integration Boundary

Salvia and Rosmarinus communicate only through:

1. their shared MongoDB database; and
2. Ably Pub/Sub.

Do not add direct HTTP, RPC, or gRPC calls between the two applications. Public
ActivityPub HTTP traffic terminates at Rosmarinus and is not a Salvia-to-
Rosmarinus application API.

MongoDB is the durable source of truth. Ably carries authenticated commands and
low-latency notifications; it is not the canonical store. After a reconnect,
missed event, or ambiguous command result, rebuild the UI from MongoDB.

## MongoDB Collection Ownership

Every collection has exactly one writer. Sharing a database does not grant
shared write ownership.

Salvia may:

- write only Salvia-owned collections, conventionally prefixed `salvia_`;
- create and manage indexes only on Salvia-owned collections; and
- read the documented Rosmarinus collections through narrow repository
  projections needed by the UI.

Salvia must never write, repair, migrate, or create indexes on Rosmarinus-owned
Actor, note, follow, reaction, block, federation, delivery, queue, or command
receipt collections. A requested UI mutation of federation state must be an
Ably command, not a MongoDB update.

At minimum, Salvia owns `salvia_accounts` with this cross-service projection:

```text
_id             stable local account ID
ablyClientId    stable, opaque, URL-safe Ably identity; unique
status          "active" | "suspended" | "deleted"
authzRevision   monotonically increasing integer
createdAt       creation time
updatedAt       last update time
deletedAt       deletion time, null or absent while not deleted
```

Additional authentication-provider fields may exist, but Rosmarinus must not
depend on them. Create unique indexes for `_id` and `ablyClientId` in Salvia.
Prefer soft deletion so historical ownership and federation records retain a
valid account reference.

Use separate MongoDB credentials for Salvia and Rosmarinus. Salvia's database
role should have write access only to Salvia-owned collections and read access
only to the Rosmarinus collections the UI needs.

Typical Salvia-only collections include `salvia_ui_settings` and
`salvia_actor_settings`. UI preferences for a Rosmarinus Actor belong there;
do not add UI-only fields to the Rosmarinus Actor document.

Rosmarinus-owned remote Actor documents can add `movedToUri`, `alsoKnownAs`,
and `movedAt` after validating an ActivityPub account migration. Treat these as
read-only federation state. A non-null `movedAt` is the signal that Rosmarinus
accepted the reciprocal alias proof; Salvia must not validate or write account
migrations itself.

Treat a suspended remote Actor as unavailable immediately. Its Notes,
reactions, Follow/Block relationships, polls, and related notifications may be
soft-deleted or removed asynchronously by Rosmarinus account cleanup; do not
retain a separate Salvia relationship or content-presence flag.

Treat remote Actor `summary`, `url`, `profileFields`, `birthday`, `location`,
`avatarUrl`, `bannerUrl`, `tags`, `emojiNames`, `isBot`, `isCat`, `isLocked`,
`isDiscoverable`, and `lastFetchedAt` as read-only federation profile state.
`featuredNoteIds` contains resolved `notes._id` values for pinned/featured
posts. `summary` and profile-field values are already converted to
Rosmarinus' MFM-compatible text. The avatar/banner fields are validated remote
HTTPS source URLs supplied directly to Salvia. Treat them as untrusted remote
resources, not Rosmarinus cache URLs.

Remote Note relationships expose both federation URIs (`inReplyToUri`,
`quoteUri`) and resolved Rosmarinus IDs (`replyId`, `quoteId`). Use the ID fields
for MongoDB joins and thread rendering. Treat URI fields as read-only
ActivityPub source data, not database foreign keys.

For a Note with `visibility: "specified"`, enforce the Rosmarinus-owned
`visibleUserUris` audience before returning content. Do not substitute
`mentionUris` as the authorization list.

Resolve remote custom emoji by the Rosmarinus-owned `{ host, name }` key. Treat
`originalUrl`, `publicUrl`, URI, media type, and update timestamps as read-only
direct remote metadata. Rosmarinus does not download, transform, proxy, or
cache Actor, Note, emoji, or instance media. Salvia owns browser presentation;
if it performs server-side media fetching, enforce DNS rebinding/SSRF
protection, byte/time limits, MIME validation, and active-content rejection.

Treat `instances` as read-only Rosmarinus federation state keyed by unique
normalized `host`. It contains NodeInfo counts and software metadata, server
name/description/maintainer/icon/favicon/theme metadata, latest authenticated
receive and delivery timestamps/status, `isNotResponding`,
`notRespondingSince`, and `suspensionState`. The relationship counters mean
remote-host users following local Actors (`followingCount`) and local Actors
following remote-host users (`followersCount`). Treat a suspension state other
than `none` as delivery-disabled. `iconUrl` and `faviconUrl` are validated
direct remote URLs and remain untrusted frontend resources.

Join ActivityPub Question notes to `polls` by `polls._id = notes._id`. Preserve
the stored choice order and pair each choice with the vote count at the same
array index. Never update poll arrays directly from Salvia.

Read an Actor's selected Poll choices from `poll_votes` by `noteId` and
`actorId`. Treat `choice` as a zero-based index and never insert or update vote
documents directly.

Treat a Note's non-null `deletedAt` as authoritative. Rosmarinus subsequently
soft-deletes reactions and removes the related Poll, Poll votes, and
notifications idempotently; tolerate those dependent records during retries.

Derive reply, renote, and reaction counts in batched MongoDB aggregations over
active `notes` and `reactions`; filter `deletedAt: null`. Do not add or update
denormalized counter fields in Rosmarinus Note documents.

An accepted ActivityPub Block removes pending/active follow relationships in
both directions. Recompute UI relationship state from Rosmarinus-owned
`blocks` and `follows`; do not preserve or repair a separate Salvia follow flag.

## Account and Actor Identity

An Ably `clientId` identifies one authenticated local account. It does not
identify an Actor, a browser tab, or an authentication-provider identity.

One local account may own any number of local Actors:

```text
Salvia account (_id, ablyClientId)
  -> Rosmarinus Actor A (ownerAccountId)
  -> Rosmarinus Actor B (ownerAccountId)
  -> Rosmarinus Actor C (ownerAccountId)
```

Actor selection is command data. Commands that act as an existing Actor carry
`actor_id`; Rosmarinus verifies that the Actor is local and that
`ownerAccountId` matches the account resolved from the Ably message
`clientId`. Never treat the browser's selected Actor or a payload account ID as
proof of authorization.

`actor.create` is account-scoped and therefore omits `actor_id`. Rosmarinus
assigns the authenticated account as the new Actor's owner.

## Ably Authentication and Authorization

Only the Salvia backend may issue browser Ably credentials. The token endpoint
must:

- authenticate the existing Salvia session;
- load the corresponding `salvia_accounts` row;
- reject suspended, deleted, or missing accounts;
- fix the token identity to the stored `ablyClientId`;
- issue a short-lived token or JWT;
- grant only the capabilities below; and
- return `Cache-Control: no-store`.

For account `{accountId}`, browser capability is limited to:

```json
{
  "rosmarinus:commands": ["publish"],
  "rosmarinus:accounts:{accountId}:events": ["subscribe"]
}
```

Do not accept `clientId`, account ID, Actor ID, or channel names from the token
request. Derive them from the authenticated session and database. Never expose
an Ably API key or signing secret to browser code. Keep browser-token signing
credentials separate from the server credential that publishes account
authorization control messages. Rosmarinus also uses separate least-privilege
credentials for command subscription, account-event publishing, and
account-control subscription.

Use the Ably SDK's `authUrl` or `authCallback` renewal flow. Subscribe to the
account event channel before publishing a command whenever the UI needs its
result.

## Command Contract

Publish commands to `rosmarinus:commands`. The Ably message `clientId` is the
authenticated account identity. The JSON payload envelope is:

```json
{
  "version": 1,
  "request_id": "stable unique request ID",
  "actor_id": "local Actor ID; omitted only where documented",
  "data": {}
}
```

Use these Ably message names and payloads:

- `actor.create`: omit `actor_id`; `data` contains `username`, and may contain
  `name` and `type`.
- `post.create`: `data` contains `note_id` and `text`, and may contain
  `visibility`, `content_warning`, `sensitive`, `in_reply_to_uri`, `quote_uri`,
  `mention_uris`, `hashtags`, and `emoji_names`. `emoji_names` contains at most
  100 local names without colons; Rosmarinus resolves the URLs. When
  `visibility` is `specified`,
  `mention_uris` is required, must contain at least one Actor URI, and defines
  the direct recipients. It may also contain `poll` with `choices`, optional
  `multiple`, and optional RFC 3339 `expires_at`; `text` may be empty when
  `poll` is present. `text` is current Misskey-compatible MFM source; send it
  unchanged and never send pre-rendered HTML. Rosmarinus owns safe ActivityPub
  HTML rendering and conditional Misskey source metadata.
- `post.delete`: `data` contains `note_id`; top-level `actor_id` must own the
  local Note. Treat success as a soft deletion plus queued federation delivery.
- `poll.vote`: `data` contains `note_id` and a zero-based non-negative
  `choice`; top-level `actor_id` is the owned local voting Actor.
- `follow.create`: `data` contains `target`, which is a Fediverse handle or an
  absolute ActivityPub Actor URL.
- `follow.delete`: `data` contains the same `target` forms as `follow.create`;
  top-level `actor_id` is the owned local Actor whose outgoing relationship is
  removed.
- `reaction.create`: `data` contains `note_id` and `reaction`; top-level
  `actor_id` is the owned local Actor applying the reaction.
- `reaction.delete`: `data` contains `note_id`; top-level `actor_id` is the
  owned local Actor whose reaction is removed.
- `follow.approve`: `data` contains `follower_id`; top-level `actor_id` is the
  local followee Actor.
- `follow.reject`: the same addressing rules as `follow.approve`.
- `notification.mark_read`: `data` contains `notification_id`; top-level
  `actor_id` must be the owned local recipient Actor.

Generate `request_id` in Salvia before the first publish. A retry of the same
logical operation must reuse it. Do not insert account IDs or user IDs into
command payloads as an authorization mechanism.

## Event Contract

Subscribe only to `rosmarinus:accounts:{accountId}:events`. Events use:

```json
{
  "version": 1,
  "type": "event type",
  "request_id": "present for correlated commands",
  "actor_id": "present when applicable",
  "occurred_at": "RFC 3339 timestamp",
  "data": {}
}
```

Handle at least `command.succeeded`, `command.failed`, `actor.created`,
`post.created`, `notification.created`, `follow.approval.requested`,
`follow.approval.completed`, and `follow.approval.rejected`.
`command.succeeded.data` contains `command` and optional `result`;
`command.failed.data` contains `command` and `code`. Multiple tabs for the same
account can receive the same event, so event handlers and UI notifications must
be idempotent. Use `request_id` to correlate a response, but do not assume that
receiving an event means the browser's local state is canonical.

Read notifications from the Rosmarinus-owned `notifications` collection,
scoped by the authenticated account and recipient Actor. Never update
`isRead` directly. Publish `notification.mark_read` with the owned Actor ID and
notification ID, and deduplicate repeated `notification.created` events by
`notification_id` before refreshing MongoDB state.
Support `pollEnded` in addition to the existing Follow, reaction, renote,
reply, and mention kinds; use its `noteId` to load the completed Poll.

The current Rosmarinus handler can reject malformed, unauthenticated, or
unauthorized commands before it creates a receipt and publishes a result. A
browser timeout is therefore an ambiguous outcome, not a machine-readable
authorization result. Refresh canonical state and retry the same logical
request with the same `request_id` when appropriate. If the UI requires typed
pre-execution failures, add that as a versioned Rosmarinus contract change.

## Account Lifecycle

When suspending, deleting, or otherwise changing account authorization:

1. update `salvia_accounts.status` and increment `authzRevision` atomically;
2. stop issuing browser tokens immediately; and
3. publish `account.authorization.changed` to
   `rosmarinus:control:accounts` with:

```json
{
  "account_id": "account ID",
  "authz_revision": 2
}
```

The database write must happen first. The control message is only an
invalidation hint; Rosmarinus re-reads the Salvia-owned row and periodically
reconciles it. Retrying the same revision is safe. Salvia must not modify or
delete the account's Actors as part of this flow.

## Engineering Rules

- Follow modern Next.js and TypeScript practices already established in the
  Salvia repository.
- Keep authentication, repositories, Ably clients, clocks, ID generators, and
  loggers behind focused interfaces where dependency injection improves tests.
- Validate environment configuration at startup and load all runtime
  configuration from environment variables.
- Keep credentials and token issuance code in server-only modules.
- Validate all database documents, command inputs, and event envelopes at
  runtime; TypeScript types alone are not a trust boundary.
- Log meaningful lifecycle and command-correlation events without logging
  sessions, JWTs, API keys, or sensitive command content.
- Write focused unit and integration tests whenever practical.
- Keep source files using LF line endings.

## Git Workflow

- Make commits at coherent implementation checkpoints after verification
  passes. Do not mix unrelated or unfinished changes into a commit.
- Before every Salvia commit, run these commands in this exact order:

  ```sh
  pnpm format
  pnpm lint
  pnpm build
  ```

- If any command fails, fix the failure and rerun the sequence before
  committing.
- Always create signed git commits.
- If commit signing fails, do not create an unsigned commit. Stop and notify
  the user that the commit could not be signed.
