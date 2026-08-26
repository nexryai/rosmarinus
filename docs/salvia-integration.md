# Salvia Integration Contract

Salvia and Rosmarinus share MongoDB and Ably Pub/Sub. They do not call each
other over HTTP. MongoDB is authoritative; Ably carries browser commands,
command results, account events, and low-latency account invalidations.

## Collection ownership

Each collection has one writer and one migration/index owner.

| Collection | Writer | Other service access |
| --- | --- | --- |
| `salvia_accounts` | Salvia | Rosmarinus reads the authorization projection |
| Salvia session and UI collections | Salvia | No Rosmarinus access |
| `actors`, `notes`, `polls`, `poll_votes`, `follows`, `reactions`, `blocks`, `emojis`, `instances`, `abuse_reports`, `notifications` | Rosmarinus | Salvia reads UI-facing state |
| `connector_command_receipts` | Rosmarinus | Salvia does not need write access |

Use separate MongoDB users with collection-scoped custom roles. The Salvia role
must have read/write and index privileges only on Salvia-owned collections and
`find` on the Rosmarinus collections it renders. The Rosmarinus role must have
read/write and index privileges only on Rosmarinus-owned collections and
`find` on `salvia_accounts`. It must not have access to Salvia sessions.

Rosmarinus never creates an index on `salvia_accounts`. Salvia must provide a
unique index on `ablyClientId`.

Remote documents in `actors` are refreshed by Rosmarinus after it verifies a
signed ActivityPub `Update(Person)`. Salvia should render the current shared
document and must not maintain a separately writable remote-profile copy.

The read-only remote profile projection includes `summary`, `url`,
`profileFields`, `birthday`, `location`, `avatarUrl`, `bannerUrl`, `tags`,
`emojiNames`, `isBot`, `isCat`, `isLocked`, `isDiscoverable`, and
`lastFetchedAt`. `featuredNoteIds` contains up to five resolved `notes._id`
values from the Actor's current ActivityPub featured collection. Rosmarinus
converts ActivityPub HTML summaries and property values to its MFM-compatible
text representation before storing them. The avatar and banner values are
validated HTTPS source URLs supplied directly to Salvia. They are untrusted
remote resources, not Rosmarinus-hosted cache URLs.

Remote Actor documents may contain additive account-migration fields owned by
Rosmarinus:

```text
movedToUri    destination ActivityPub Actor URI claimed by the remote Actor
alsoKnownAs   Actor URIs claimed as aliases by the remote Actor
movedAt       timestamp when Rosmarinus accepted the verified Move
```

Salvia may use these fields to redirect profile navigation or label a remote
account as moved. It must not infer a valid migration from `movedToUri` alone:
Rosmarinus writes `movedAt` only after fetching both Actors, confirming the
source claim, and finding the source URI in the destination's `alsoKnownAs`.

After an authenticated remote `Delete(Actor)`, Rosmarinus suspends the Actor
immediately and runs an idempotent `account-delete` job. The job soft-deletes
the Actor's Notes, its reactions, reactions on its Notes, and Follow/Block
relationships involving it; it removes the Actor's polls and notifications it
originated or whose related Note was removed. Salvia must tolerate those
records disappearing asynchronously after the Actor becomes suspended.

Remote Note documents retain ActivityPub relationship URIs and resolved local
references together:

```text
inReplyToUri   ActivityPub URI claimed by the remote Note
replyId        Rosmarinus Note `_id` after successful recursive resolution
quoteUri       ActivityPub quote URI
quoteId        Rosmarinus Note `_id` after successful recursive resolution
visibleUserUris ActivityPub Actor URIs allowed to read a `specified` Note
```

Salvia should join thread/quote views by `replyId` and `quoteId`. The URI fields
are federation source data and remain useful for diagnostics and outbound
rendering; they are not MongoDB foreign keys.

For `specified` Notes, Salvia must apply `visibleUserUris` in addition to
Actor/account ownership before returning content to a browser. `mentionUris`
is a notification/rendering projection and is not itself the authorization
list.

Remote custom emoji metadata is keyed uniquely by `{ host, name }` in the
Rosmarinus-owned `emojis` collection. `uri`, `originalUrl`, `publicUrl`,
`mediaType`, `remoteUpdatedAt`, and `updatedAt` are read-only federation state.
The emoji document's `originalUrl` and `publicUrl` are direct remote URLs.
Remote Note attachments likewise retain their ActivityPub `url` values.
Rosmarinus does not download, transform, proxy, or cache these resources.
Salvia owns the browser presentation policy: it may render direct URLs or use a
separately secured frontend image service. Any server-side frontend fetcher
must independently enforce DNS rebinding/SSRF protection, byte and time limits,
MIME validation, and active-content rejection.

Rosmarinus owns the `instances` collection and registers a normalized remote
host after authenticated inbound contact or outbound delivery. Salvia may use
this read-only projection for federation administration and remote-server UI:

```text
_id                      stable opaque instance ID
host                     normalized ASCII host; unique
usersCount               remote NodeInfo user count
notesCount               remote NodeInfo local post plus comment count
followingCount           accepted remote-host users following local Actors
followersCount           accepted local Actors following remote-host users
latestRequestReceivedAt  latest authenticated inbound Activity timestamp
latestRequestSentAt      latest attempted outbound delivery timestamp
latestStatus             latest outbound HTTP status, or 0 for transport errors
isNotResponding          whether the latest delivery path is failing
notRespondingSince       start of the current delivery-failure period
suspensionState          none | manuallySuspended | goneSuspended | autoSuspendedForNotResponding
softwareName             normalized NodeInfo software name
softwareVersion          NodeInfo software version
openRegistrations        NodeInfo registration flag when supplied
name, description        remote server presentation metadata
maintainerName, maintainerEmail
iconUrl, faviconUrl      validated remote HTTPS source URLs
themeColor               remote theme-color metadata
firstRetrievedAt         first contact timestamp
infoUpdatedAt            latest metadata refresh timestamp
updatedAt                latest operational update timestamp
```

Rosmarinus refreshes metadata no more than daily unless explicitly forced and
keeps relationship counts current when accepted follows change. `iconUrl` and
`faviconUrl` are validated direct remote URLs subject to the same frontend
media policy.
Treat any non-`none` suspension state as an outbound-delivery stop. A host that
fails continuously for seven days is automatically suspended; authenticated
inbound traffic revives only that automatic failure suspension, never a manual
or gone suspension.

ActivityPub `Question` state is stored in `polls`, keyed by the related
`notes._id`. The read projection contains `authorId`, `authorHost`, ordered
`choices`, positionally matching `votes`, `multiple`, optional `expiresAt`, and
timestamps. `Update(Question)` may change vote counts but not the stored choice
identity or ordering. Salvia must zip `choices` and `votes` by array index and
treat both arrays as Rosmarinus-owned.

`poll_votes` is Rosmarinus-owned and contains deterministic vote documents
with `noteId`, `actorId`, zero-based `choice`, and `createdAt`. For a
single-choice poll, one Actor can have only one vote; for a multiple-choice
poll, one Actor can vote once per choice. Salvia may use this collection to
project the authenticated Actor's selected choices but must not mutate it.

Deleting a local or remote Note first soft-deletes the Note and then
idempotently soft-deletes its reactions and removes its Poll, Poll votes, and
related notifications. Salvia must treat `notes.deletedAt` as authoritative
immediately and tolerate the dependent collections converging during a retry.

Rosmarinus intentionally keeps reply, renote, and reaction records normalized
instead of maintaining crash-prone cross-document counters. Salvia derives
counts for a batch of Note IDs with MongoDB aggregation over active documents:
`notes.replyId`, `notes.renoteId`, and `reactions.noteId`, always filtering
`deletedAt: null`. Group reactions by `reaction` when per-emoji counts are
needed. Rosmarinus owns the supporting indexes; Salvia must not write cached
counts back into Note documents.

Local user-facing federation notifications are durable documents in the
Rosmarinus-owned `notifications` collection:

```text
_id                 stable opaque notification ID
recipientAccountId  owning Salvia account ID
recipientActorId    local Actor receiving the notification
kind                followRequest | reaction | renote | reply | mention | pollEnded
sourceActorId        Actor that caused the notification
noteId               related Note ID when applicable
remoteActivityId     source ActivityPub object/activity ID used for deduplication
createdAt            notification creation timestamp
isRead               authoritative read state
readAt               timestamp set when marked read
```

Salvia scopes notification queries by the authenticated account and, for an
Actor-specific view, `recipientActorId`. Sort by `createdAt` descending with
`_id` as the stable pagination tie-breaker. Salvia reads these documents
directly but changes read state only through the Connector command below.

When Rosmarinus accepts an ActivityPub `Block`, it stores the block and
soft-deletes active/pending `follows` in both directions for that Actor pair.
Salvia must derive the resulting relationship state from the current `blocks`
and `follows` documents rather than preserving a UI-only follow flag.

### MongoDB role bootstrap

[`docker/mongo/init-users.js`](../docker/mongo/init-users.js) creates two custom
roles and service users on a new MongoDB deployment:

- `rosmarinusService` can write and manage indexes only on Rosmarinus-owned
  collections, and can only `find`
  documents in `salvia_accounts`.
- `salviaService` can write and manage indexes only on the four documented
  `salvia_*` collections, and can only `find` documents in Rosmarinus-owned
  federation collections.

The script requires `ROSMARINUS_MONGO_USERNAME`,
`ROSMARINUS_MONGO_PASSWORD`, `SALVIA_MONGO_USERNAME`, and
`SALVIA_MONGO_PASSWORD`. `ROSMARINUS_MONGO_DATABASE` defaults to
`rosmarinus_federation`. Mount it into `/docker-entrypoint-initdb.d` for the
official MongoDB image. MongoDB runs these scripts only when initializing an
empty data directory; apply equivalent role changes explicitly to an existing
deployment.

Each service should use its own authenticated URI with the application database
as `authSource`, for example:

```text
mongodb://<service-user>:<percent-encoded-password>@mongo:27017/rosmarinus_federation?authSource=rosmarinus_federation
```

Generate independent, high-entropy root and service passwords through the
deployment secret manager. The literal credentials in the Docker federation
compose files are disposable fixture values and must not be reused outside
those fixtures.

## Salvia account projection

Salvia writes one document per local account:

```json
{
  "_id": "account-01J...",
  "ablyClientId": "client-01J...",
  "status": "active",
  "authzRevision": 4,
  "deletedAt": null
}
```

Supported status values are `active`, `suspended`, and `deleted`. Accounts
referenced by Actors are soft-deleted. Rosmarinus accepts a state-changing
command only when the account is `active` and `deletedAt` is absent or null.

`ablyClientId` is an opaque, stable, URL-safe account identifier. It is not an
Actor ID, username, or email address. Changing it immediately prevents the old
client ID from resolving in Rosmarinus, independently of Ably token expiry.

## Actor ownership

Rosmarinus writes `ownerAccountId` on every user-managed local Actor. One
account can own multiple Actors; one Actor has one owner. Remote Actors have no
owner. The environment-provisioned Actor has `isSystemActor: true` and no
account owner.

Actor-bound commands are authorized with one MongoDB query over `_id`,
`ownerAccountId`, `host: null`, and `isSuspended: false`. The command payload
cannot override account ownership.

System Actor events are not published to an account event channel because no
account owns them. A future operator workflow must use a separately authorized
system-event channel rather than assigning a fake account owner.

## Ably channels and keys

Default channels are:

| Channel | Publisher | Subscriber |
| --- | --- | --- |
| `rosmarinus:commands` | Authenticated browser | Rosmarinus |
| `rosmarinus:accounts:{accountId}:events` | Rosmarinus | That account's browser clients |
| `rosmarinus:control:accounts` | Salvia backend | Rosmarinus |

Use five least-privilege Ably keys:

1. Salvia browser-token issuer key: tokens may publish to
   `rosmarinus:commands` and subscribe only to the authenticated account's
   exact event channel.
2. Salvia control key: publish only to `rosmarinus:control:accounts`.
3. Rosmarinus command key: subscribe only to `rosmarinus:commands`.
4. Rosmarinus account-event key: publish only to
   `rosmarinus:accounts:*:events`.
5. Rosmarinus account-control key: subscribe only to
   `rosmarinus:control:accounts`.

Salvia sets a non-wildcard `x-ably-clientId` and explicit capabilities in every
short-lived browser JWT. The browser never receives an API key, wildcard
capability, command-channel subscribe permission, event-channel publish
permission, or control-channel access.

## Browser command envelope

The browser subscribes to its account event channel before publishing a
command. Every command has an Ably message name and this versioned payload:

```json
{
  "version": 1,
  "request_id": "request-01J...",
  "actor_id": "actor-01J...",
  "data": {}
}
```

`request_id` is stable across retries. Rosmarinus claims a unique
`{accountId, requestId}` receipt before mutation. A duplicate does not repeat
the mutation; it republishes the stored result or reports that the original is
still in progress.

To follow a remote Actor, publish a `follow.create` command. `actor_id` is the
owned local Actor that will follow the target. `target` accepts either a
Fediverse handle or an absolute ActivityPub Actor URL:

```json
{
  "version": 1,
  "request_id": "request-01J...",
  "actor_id": "actor-01J...",
  "data": {
    "target": "alice@remote.example"
  }
}
```

A `command.succeeded` result means the remote Actor was resolved, the outgoing
request was stored as `pending`, and its signed `Follow` delivery was enqueued.
It does not mean the remote server accepted it. Salvia reads the
Rosmarinus-owned `follows` collection for authoritative status: the relationship
changes to `accepted` only after Rosmarinus verifies and processes the remote
`Accept(Follow)`. A remote `Reject(Follow)` removes the pending relationship.

To stop following the same remote Actor, publish `follow.delete` with the owned
local Actor in `actor_id` and the handle or absolute Actor URL in `data.target`.
Rosmarinus resolves the target, soft-deletes the active relationship from its
`follows` collection, and enqueues `Undo(Follow)` to the remote Actor. A
successful `command.succeeded.data.result` contains `follower_id`,
`followee_id`, and the Undo activity `uri`. Retrying the same logical removal
must reuse its original `request_id`; a new request after the relationship is
already absent fails as `command_failed`.

To react to a Rosmarinus-stored remote Note, publish `reaction.create` with the
owned local Actor in `actor_id`:

```json
{
  "version": 1,
  "request_id": "request-01J...",
  "actor_id": "actor-01J...",
  "data": {
    "note_id": "remote-note-id",
    "reaction": "👍"
  }
}
```

Rosmarinus verifies Actor ownership and Note visibility, stores the reaction,
and enqueues a Misskey-compatible `Like` to the remote author's individual
inbox. `command.succeeded.data.result` contains `reaction_id`, `note_id`,
`reaction`, and the dereferenceable local `uri`. MongoDB remains canonical.

To remove that Actor's reaction, publish `reaction.delete` with the same
top-level `actor_id` and `data.note_id`. Rosmarinus verifies ownership, removes
the matching stored reaction, and delivers `Undo(Like)` to the remote author.
Its successful result contains `reaction_id`, `note_id`, and the Undo activity
`uri`. Retrying the same logical removal must reuse its original `request_id`;
a new request after the reaction is already absent fails as `command_failed`.

`actor.create` omits `actor_id` because the Actor does not exist yet:

```json
{
  "version": 1,
  "request_id": "request-01J...",
  "data": {
    "username": "alice-work",
    "name": "Alice Work",
    "type": "Person"
  }
}
```

Rosmarinus generates the Actor ID, URI, and key pair and derives
`ownerAccountId` from Ably `message.clientId` through `salvia_accounts`.

For `post.create` with `visibility: "specified"`, `mention_uris` is the
recipient list and must contain at least one Actor URI. Rosmarinus resolves
every recipient before storing the note, places the deduplicated Actor URIs in
the ActivityPub `to` audience, and delivers remote recipients to their
individual inboxes rather than a shared inbox.

`post.create.data.text` is current Misskey-compatible MFM, not HTML. Salvia
must pass the user's MFM source unchanged and must not pre-render or inject
HTML. Rosmarinus parses it at the federation boundary, emits escaped safe HTML,
and includes `_misskey_content` plus `source.mediaType =
text/x.misskeymarkdown` only when advanced MFM syntax or an inline quote needs
the original source for lossless Misskey rendering.

`post.create.data.emoji_names` may contain up to 100 local custom emoji names
without surrounding colons. Names use Misskey-compatible ASCII letters,
digits, and underscores. Rosmarinus resolves only locally provisioned records
from `emojis` and embeds their owned URI/public URL as ActivityPub `Emoji`
tags; unknown names are ignored. Salvia must not supply or override emoji URLs.

To delete a local Note, publish `post.delete` with the owned local Actor in
`actor_id` and `note_id` in `data`. Rosmarinus verifies Note ownership,
soft-deletes the Note, and enqueues a Misskey-compatible `Delete(Tombstone)` to
remote followers and direct recipients. A successful result contains
`actor_id`, `note_id`, and the deleted Note `uri`. Reuse the same `request_id`
when retrying the same logical deletion.

`post.create` may include `data.poll` with `choices` (2–10 unique strings, at
most 50 characters each), optional `multiple`, and optional RFC 3339
`expires_at`. Text may be empty when a Poll is present. To vote, publish
`poll.vote` with the owned local Actor in `actor_id`, plus `note_id` and a
zero-based `choice` in `data`. Rosmarinus enforces visibility, blocks,
expiration, and single/multiple-choice uniqueness. A successful result contains
`vote_id`, `note_id`, `choice`, and, for a remote Poll, the delivered vote
activity `uri`.

For a local Poll with `expires_at`, Rosmarinus schedules durable delayed work.
At expiration it creates a stable `pollEnded` notification for the local Poll
owner and each local Actor that voted. Repeated delayed-job execution does not
create duplicate notification documents.

To mark a notification read, publish `notification.mark_read` with the owned
recipient Actor in `actor_id` and `notification_id` in `data`. A successful
result contains `notification_id` and `is_read: true`. Rosmarinus updates a
notification only when both `recipientAccountId` and `recipientActorId` match
the authenticated command; unknown or cross-Actor IDs fail without revealing
the notification.

Rosmarinus publishes `notification.created` to the recipient account event
channel after persisting the notification. Its `data` contains
`recipient_actor_id`, `notification_id`, `kind`, and optional
`source_actor_id` and `note_id`. Activity retries can republish the same event,
so Salvia deduplicates by `notification_id` and refreshes the durable MongoDB
document rather than treating the event as canonical state.

## Account invalidation and recovery

After updating `salvia_accounts`, Salvia publishes:

```json
{
  "account_id": "account-01J...",
  "authz_revision": 5
}
```

with the message name `account.authorization.changed` on the account control
channel. The event is only an invalidation. Rosmarinus reads the current Salvia
document and never trusts status supplied in the event payload.

Inactive or missing accounts are rejected on every browser mutation. A control
event immediately suspends user-managed Actors belonging to an inactive
account. Rosmarinus also periodically lists account owners in its Actor
collection and re-reads `salvia_accounts`, so a missed Ably control event is
eventually repaired from MongoDB.

## Rosmarinus environment

| Variable | Default |
| --- | --- |
| `ABLY_COMMAND_SUBSCRIBE_API_KEY` | empty; command subscription disabled |
| `ABLY_ACCOUNT_EVENT_PUBLISH_API_KEY` | empty; account-event publishing disabled |
| `ABLY_ACCOUNT_CONTROL_SUBSCRIBE_API_KEY` | empty; account-control subscription disabled |
| `ABLY_ROSMARINUS_API_KEY` | empty; deprecated fallback for each unset role-specific key |
| `CONNECTOR_COMMAND_CHANNEL` | `rosmarinus:commands` |
| `CONNECTOR_ACCOUNT_EVENT_NAMESPACE` | `rosmarinus:accounts` |
| `CONNECTOR_ACCOUNT_CONTROL_CHANNEL` | `rosmarinus:control:accounts` |
| `SALVIA_ACCOUNT_COLLECTION` | `salvia_accounts` |
| `CONNECTOR_RECEIPT_TTL` | `168h` |
| `CONNECTOR_ACCOUNT_RECONCILE_INTERVAL` | `5m` |
