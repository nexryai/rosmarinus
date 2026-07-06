# Rosmarinus Implementation Plan

Rosmarinus is the ActivityPub microservice successor to Concorde. It should not
copy Concorde's frontend or Misskey API surface, but it should preserve the
federation behavior that other ActivityPub servers depend on.

Do not edit `./concorde`. Treat it as the behavioral reference.

## Goals

- Implement ActivityPub federation in Go.
- Use MongoDB as the primary database.
- Use Redis for job queues, delayed retries, rate limiting, and distributed AP locks.
- Keep dependencies injectable through interfaces.
- Use Go's `log` package through injected `*log.Logger` values.
- Add focused tests for parsing, signing, resolving, rendering, and queue behavior.

## Concorde Behavior To Preserve

Concorde does more than just receive inbox activities. The relevant federation
surface is spread across ActivityPub routes, well-known routes, resolver code,
queue processors, note/follow services, and instance metadata services.

### Public Discovery Endpoints

- [ ] `GET /.well-known/host-meta`
- [ ] `GET /.well-known/host-meta.json`
- [ ] `GET /.well-known/webfinger?resource=...`
- [ ] `GET /.well-known/nodeinfo`
- [ ] `GET /nodeinfo/2.0`
- [ ] `GET /nodeinfo/2.1` if we decide to expose it

Concorde uses WebFinger to map `acct:user@example.com` and local actor URLs to
ActivityPub actor URLs. Rosmarinus needs this even without a frontend API.

### ActivityPub Public Endpoints

- [ ] `POST /inbox`
- [ ] `POST /users/{user}/inbox`
- [ ] `GET /users/{user}`
- [ ] `GET /@{user}`
- [ ] `GET /users/{user}/publickey`
- [ ] `GET /users/{user}/outbox`
- [ ] `GET /users/{user}/followers`
- [ ] `GET /users/{user}/following`
- [ ] `GET /users/{user}/collections/featured`
- [ ] `GET /notes/{note}`
- [ ] `GET /notes/{note}/activity`
- [ ] `GET /emojis/{emoji}`
- [ ] `GET /likes/{like}`
- [ ] `GET /follows/{follower}/{followee}`

Responses should negotiate `application/activity+json` and
`application/ld+json; profile="https://www.w3.org/ns/activitystreams"` in the
same style as Concorde.

### Inbox Validation

- [ ] Read raw request body and limit it, initially to Concorde's 64 KiB.
- [ ] Parse JSON after preserving the raw body.
- [ ] Require and verify `Digest: SHA-256=...`.
- [ ] Parse HTTP Signature.
- [ ] Accept only supported algorithms such as `rsa-sha256` and compatible
      hs2019 forms.
- [ ] Require signed `(request-target)`, `digest`, `host`, and `date` for POST.
- [ ] Verify `Host` against the configured local host.
- [ ] Resolve signer by `keyId`; fall back to resolving `activity.actor`.
- [ ] Reject old `acct:` key IDs.
- [ ] Verify signature using the actor public key.
- [ ] Require signer actor URI to match `activity.actor`.
- [ ] Require `activity.id` host to match signer host.
- [ ] Update instance communication stats after accepted requests.
- [ ] Enqueue the activity and return `202 Accepted`.

### Activity Processing

- [ ] `Create`: merge activity/object audiences, fill missing `attributedTo`,
      resolve object, and create notes/questions.
- [ ] `Announce`: resolve target note, check visibility, create renote.
- [ ] `Like`, `EmojiReaction`, `EmojiReact`: resolve target note, extract emoji
      tags, create reaction.
- [ ] `Follow`: remote actor follows local actor, with block/lock/request logic.
- [ ] `Accept`: accept local outgoing follow requests.
- [ ] `Reject`: reject local outgoing follow requests.
- [ ] `Undo`: support undo follow, block, like, announce, and accept.
- [ ] `Delete`: delete notes, tombstone actors, and enqueue account cleanup.
- [ ] `Update`: update actors, notes, and questions/polls.
- [ ] `Block`: create local block state for remote actor against local actor.
- [ ] `Flag`: store abuse reports for local users mentioned in the object list.
- [ ] `Add` and `Remove`: inspect Concorde handling before deciding exact scope.
- [ ] Refuse to ingest `Collection` / `OrderedCollection` as a single activity.

### Actor Resolution

- [ ] Resolve local actors from MongoDB.
- [ ] Resolve remote actors using signed GET when possible.
- [ ] Validate actor type: `Person`, `Service`, `Group`, `Organization`,
      `Application`.
- [ ] Require actor `id`, `inbox`, and `outbox` to belong to the expected host.
- [ ] Validate `sharedInbox`, `followers`, `following`, and public key host.
- [ ] Validate `preferredUsername` against Concorde-compatible rules.
- [ ] Truncate display name and summary to Concorde-compatible limits.
- [ ] Store remote public keys by `keyId`.
- [ ] Store `inbox`, `sharedInbox`, `followersUri`, `featured`, bot/cat flags,
      discoverability, profile fields, birthday, location, avatar, banner, and
      custom emoji names where available.
- [ ] Refresh remote actors when stale, initially after 24 hours.
- [ ] Update featured notes from remote `featured` collections.

### Note Resolution And Creation

- [ ] Resolve by AP URI, with local URI parsing and remote signed GET.
- [ ] Use Redis AP locks to deduplicate concurrent resolution by URI.
- [ ] Validate post types: `Note`, `Question`, `Article`, `Audio`, `Document`,
      `Image`, `Page`, `Video`, `Event`.
- [ ] Require `note.id` and `attributedTo` hosts to match actor host.
- [ ] Reject unexpected non-HTTPS note IDs.
- [ ] Parse audience from `to` and `cc` into `public`, `home`, `followers`,
      or `specified`.
- [ ] Extract AP mentions and hashtags.
- [ ] Resolve attachments as media records.
- [ ] Resolve replies and quotes.
- [ ] Preserve Misskey compatibility fields: `_misskey_content`,
      `source.mediaType = text/x.misskeymarkdown`, `_misskey_quote`,
      `quoteUrl`, and `_misskey_talk`.
- [ ] Convert remote HTML to MFM-compatible text.
- [ ] Store content warning, sensitive flag, files, polls, emoji tags, URI, URL,
      visibility, visible users, reply, renote, and denormalized author fields.
- [ ] Update reply counts, renote counts, hashtags, and local notifications
      where Rosmarinus owns those writes.

### Custom Emoji

- [ ] Render local custom emojis as ActivityPub `Emoji` tags.
- [ ] Extract remote `Emoji` tags from actors and notes.
- [ ] Normalize `:name:` to `name`.
- [ ] Upsert by `(host, name)`.
- [ ] Update existing emoji if AP URI, updated timestamp, or original URL changes.

### Polls

- [ ] Render local polls as `Question` with `oneOf` or `anyOf`.
- [ ] Extract remote `Question` choices and expiry.
- [ ] Support vote activities represented as notes replying to poll notes.
- [ ] Enqueue delayed poll-ended work in Redis.
- [ ] Deliver question updates to remote followers when votes change.

### Delivery

- [ ] Sign POST requests with `rsa-sha256`.
- [ ] Include `Date`, `Host`, `Content-Type`, `Digest`, `Signature`, and
      `User-Agent`.
- [ ] Sign GET requests with `Accept`, `Date`, `Host`, and `Signature`.
- [ ] Use `(request-target) date host digest` for POST signing strings.
- [ ] Use `(request-target) date host accept` for GET signing strings.
- [ ] Build delivery inbox lists from followers and direct recipients.
- [ ] Prefer `sharedInbox` and deduplicate inboxes.
- [ ] Skip blocked and suspended hosts.
- [ ] Update instance send stats on success and failure.
- [ ] Suspend delivery to instances that have failed for a long period.

### Instance Metadata

- [ ] Register instances by host on first contact.
- [ ] Track counts for users, notes, following, and followers where relevant.
- [ ] Track latest sent/received timestamps and status codes.
- [ ] Fetch remote `.well-known/nodeinfo`.
- [ ] Fetch remote root HTML and `manifest.json` where useful.
- [ ] Store software name/version, open registrations, maintainer, name,
      description, icon, favicon, and theme color.
- [ ] Refresh metadata at most daily unless forced.

## Redis Queue Design

Concorde uses Bull with Redis. Rosmarinus should use Redis as the queue backend,
not an in-memory worker. The default implementation should use Asynq, wrapped by
`internal/queue` interfaces so ActivityPub services do not depend on Asynq
directly.

Rosmarinus should remain a single application binary by default. The HTTP server,
Asynq client, and Asynq worker server should run in the same process, sharing the
same DI container and graceful shutdown path. Redis is still required as external
state, but no separate worker service should be required for normal operation.

The queue layer should still allow future split-worker operation through config
flags, but that is an operational option, not the default architecture.

### Queue Names

- [ ] `inbox`: accepted inbound ActivityPub activities.
- [ ] `deliver`: outbound ActivityPub delivery jobs.
- [ ] `system`: scheduled maintenance.
- [ ] `poll-ended`: delayed poll expiration work.
- [ ] `media`: remote attachment/avatar/banner fetch and cleanup.
- [ ] `metadata`: remote instance metadata refresh.
- [ ] `account-delete`: remote actor delete cleanup.

### Single Binary Runtime

- [ ] Start HTTP routes and queue workers from the same `cmd/rosmarinus`
      process by default.
- [ ] Create one Asynq client for enqueueing jobs from HTTP handlers and domain
      services.
- [ ] Create one Asynq server in the same process for `inbox`, `deliver`,
      `system`, `poll-ended`, `media`, `metadata`, and `account-delete` jobs.
- [ ] Register handlers through DI so job handlers use the same repositories,
      resolver, signer, renderer, and logger wiring as HTTP handlers.
- [ ] Support graceful shutdown that stops accepting HTTP requests, stops
      enqueueing new jobs, drains or stops Asynq workers, then closes Redis and
      MongoDB clients.
- [ ] Add config flags for advanced deployments:
      `run_http`, `run_workers`, and `worker_queues`, all enabled by default.
- [ ] Keep the default documented deployment as one Rosmarinus process plus
      MongoDB and Redis.

### Queue Requirements

- [ ] Redis-backed persistence.
- [ ] At-least-once processing.
- [ ] Idempotent handlers keyed by AP URI where possible.
- [ ] Worker concurrency per queue.
- [ ] Per-second rate limits:
      `deliver` defaults to 128/sec, `inbox` defaults to 16/sec.
- [ ] Retry attempts:
      `deliver` defaults to 17, `inbox` defaults to 10.
- [ ] AP backoff compatible with Concorde:
      `(attempts^4 + 15) * 1s`, capped at 8 hours, plus up to 20% jitter.
- [ ] Job timeout:
      `deliver` defaults to 1 minute, `inbox` defaults to 5 minutes.
- [ ] Dead-letter or failed-job inspection.
- [ ] Ability to promote delayed `deliver` and `inbox` jobs for operations.
- [ ] Structured payload versioning for safe migrations.

### Redis Locks And Cache

- [ ] AP object lock by URI for note/actor/announce resolution.
- [ ] Instance metadata lock by host.
- [ ] Public key cache keyed by `keyId`.
- [ ] Actor URI cache.
- [ ] Suspended host cache.
- [ ] Optional WebFinger cache with short TTL.

## MongoDB Collections

### Core Collections

- [ ] `actors`
- [ ] `actor_profiles`
- [ ] `actor_public_keys`
- [ ] `notes`
- [ ] `polls`
- [ ] `reactions`
- [ ] `follows`
- [ ] `follow_requests`
- [ ] `blocks`
- [ ] `emojis`
- [ ] `media`
- [ ] `instances`
- [ ] `abuse_reports`

### Required Indexes

- [ ] `actors`: unique `uri`
- [ ] `actors`: unique sparse `{ usernameLower, host }`
- [ ] `actor_public_keys`: unique `keyId`
- [ ] `notes`: unique sparse `uri`
- [ ] `notes`: `userId`, `userHost`, `replyId`, `renoteId`, `createdAt`
- [ ] `notes`: tag and mention indexes suitable for MongoDB
- [ ] `follows`: unique `{ followerId, followeeId }`
- [ ] `follow_requests`: unique `{ followerId, followeeId }`
- [ ] `blocks`: unique `{ blockerId, blockeeId }`
- [ ] `emojis`: unique `{ name, host }`
- [ ] `instances`: unique `host`
- [ ] `media`: unique sparse `uri`, and content hash if available

## Go Package Plan

- [ ] `cmd/rosmarinus`: process entrypoint.
- [ ] `internal/config`: config loading and validation.
- [ ] `internal/app`: dependency wiring.
- [ ] `internal/http`: server setup, middleware, route registration.
- [ ] `internal/activitypub/types`: AP object structs and helpers.
- [ ] `internal/activitypub/signature`: HTTP signatures, digest, signed GET/POST.
- [ ] `internal/activitypub/resolver`: AP resolver and local URI resolver.
- [ ] `internal/activitypub/renderer`: actor, note, collection, emoji, follow,
      like, delete, update, accept, reject, undo renderers.
- [ ] `internal/activitypub/performer`: activity dispatch and handlers.
- [ ] `internal/activitypub/audience`: `to`/`cc` visibility parser.
- [ ] `internal/domain`: actor, note, follow, reaction, emoji, media, instance
      domain services.
- [ ] `internal/store/mongo`: MongoDB repositories and index setup.
- [ ] `internal/queue`: Redis queue interfaces and implementation.
- [ ] `internal/cache`: Redis-backed caches and locks.
- [ ] `internal/mfm`: MFM/HTML conversion compatibility layer.

## Implementation Phases

### Phase 0: Project Foundation

- [ ] Add config model for host, URL, MongoDB, Redis, HTTP, queue limits, and
      user agent.
- [ ] Create app wiring with explicit DI.
- [ ] Add graceful shutdown for HTTP, MongoDB, Redis, and workers.
- [ ] Add MongoDB connection and index bootstrap.
- [ ] Add Redis connection and health check.
- [ ] Add logger injection.
- [ ] Add basic tests for config validation.

### Phase 1: ActivityPub Types, Signatures, And Queues

- [ ] Implement AP type helpers equivalent to Concorde `getApId`, `getApType`,
      actor/post/activity predicates, and array normalization.
- [ ] Implement digest verification tests.
- [ ] Implement HTTP Signature parse/verify tests with fixture requests.
- [ ] Implement signed GET/POST tests with stable signing strings.
- [ ] Implement Redis queues, workers, retry/backoff, and timeout handling.
- [ ] Implement Redis AP locks and tests for duplicate lock behavior.

### Phase 2: Discovery And Actor Resolution

- [ ] Implement WebFinger local responses.
- [ ] Implement NodeInfo responses.
- [ ] Implement remote WebFinger client.
- [ ] Implement actor renderer and `/users/{id}`.
- [ ] Implement `/users/{id}/publickey`.
- [ ] Implement remote actor create/update.
- [ ] Implement signed remote AP GET.
- [ ] Add actor validation tests based on Concorde edge cases.

### Phase 3: Inbox MVP

- [ ] Implement `/inbox` and `/users/{id}/inbox`.
- [ ] Validate digest, signature, host, signer, and activity host.
- [ ] Enqueue `inbox` jobs.
- [ ] Implement `Create Note` handler.
- [ ] Implement note resolver and note storage.
- [ ] Implement audience parser.
- [ ] Implement HTML to MFM conversion.
- [ ] Add golden tests for incoming Mastodon/Misskey-style notes.

### Phase 4: Outbound Delivery MVP

- [ ] Implement note renderer.
- [ ] Implement create/announce/like/follow renderers.
- [ ] Implement delivery manager with sharedInbox dedupe.
- [ ] Implement `deliver` worker.
- [ ] Update instance send stats.
- [ ] Add delivery signing and retry tests.

### Phase 5: Social Graph And Reactions

- [ ] Implement `Follow`, `Accept`, `Reject`, and `Undo Follow`.
- [ ] Implement follow request storage and locked-account behavior.
- [ ] Implement `Like`, `EmojiReaction`, and undo reaction.
- [ ] Implement `Announce` and undo announce.
- [ ] Implement block and unblock.
- [ ] Add tests for local/remote follow direction combinations.

### Phase 6: Updates, Deletes, Polls, And Media

- [ ] Implement actor update.
- [ ] Implement note update.
- [ ] Implement note delete.
- [ ] Implement actor delete and account cleanup queue.
- [ ] Implement poll extraction, vote ingestion, delayed poll-ended jobs, and
      poll update delivery.
- [ ] Implement media fetch for note attachments, avatars, banners, and emoji.
- [ ] Add tests for quote, reply, poll, and sensitive media behavior.

### Phase 7: Federation Hardening

- [ ] Implement blocked/suspended host checks everywhere Concorde applies them.
- [ ] Implement instance metadata refresh.
- [ ] Implement queue inspection and delayed job promotion commands or admin
      hooks suitable for operations.
- [ ] Add load/race tests for duplicate inbox delivery.
- [ ] Add compatibility fixtures from Concorde for ActivityPub render/parse.
- [ ] Document operational Redis and MongoDB settings.

## Open Decisions

- [x] Choose the Redis queue implementation: use Asynq by default, hidden behind
      `internal/queue` interfaces.
- [ ] Decide whether Rosmarinus owns notifications/webhooks or only writes
      federation-visible state to MongoDB.
- [ ] Decide media ownership: store remote files, proxy URLs only, or delegate
      media storage to another service.
- [ ] Decide how much of Concorde's antenna/word-mute/timeline side effects
      belong in this microservice.
- [ ] Decide local actor provisioning flow, since Rosmarinus has no frontend API.
- [ ] Decide whether object IDs keep Concorde-compatible generated IDs or use
      MongoDB ObjectIDs/ULIDs.
