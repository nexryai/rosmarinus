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

- [x] `GET /.well-known/host-meta`
- [x] `GET /.well-known/host-meta.json`
- [x] `GET /.well-known/webfinger?resource=...`
- [x] `GET /.well-known/nodeinfo`
- [x] `GET /nodeinfo/2.0`
- [x] `GET /nodeinfo/2.1` if we decide to expose it

Concorde uses WebFinger to map `acct:user@example.com` and local actor URLs to
ActivityPub actor URLs. Rosmarinus needs this even without a frontend API.

### ActivityPub Public Endpoints

- [x] `POST /inbox`
- [x] `POST /users/{user}/inbox`
- [x] `GET /users/{user}`
- [x] `GET /@{user}`
- [x] `GET /users/{user}/publickey`
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

- [x] Read raw request body and limit it, initially to Concorde's 64 KiB.
- [x] Parse JSON after preserving the raw body.
- [x] Require and verify `Digest: SHA-256=...`.
- [x] Parse HTTP Signature.
- [x] Accept only supported algorithms such as `rsa-sha256` and compatible
      hs2019 forms.
- [x] Require signed `(request-target)`, `digest`, `host`, and `date` for POST.
- [x] Verify `Host` against the configured local host.
- [x] Resolve signer by `keyId`; fall back to resolving `activity.actor`.
- [x] Reject old `acct:` key IDs.
- [x] Verify signature using the actor public key.
- [x] Require signer actor URI to match `activity.actor`.
- [x] Require `activity.id` host to match signer host.
- [ ] Update instance communication stats after accepted requests.
- [x] Enqueue the activity and return `202 Accepted`.

### Activity Processing

- [ ] `Create`: merge activity/object audiences, fill missing `attributedTo`,
      resolve object, and create notes/questions.
- [ ] `Announce`: resolve target note, check visibility, create renote.
- [ ] `Like`, `EmojiReaction`, `EmojiReact`: resolve target note, extract emoji
      tags, create reaction.
- [x] `Follow`: basic remote actor follows local actor path, enqueueing
      `Accept(Follow)` for unlocked local actors.
- [ ] `Follow`: block/lock/request persistence logic.
- [ ] `Accept`: accept local outgoing follow requests.
- [ ] `Reject`: reject local outgoing follow requests.
- [ ] `Undo`: support undo follow, block, like, announce, and accept.
- [ ] `Delete`: delete notes, tombstone actors, and enqueue account cleanup.
- [ ] `Update`: update actors, notes, and questions/polls.
- [ ] `Block`: create local block state for remote actor against local actor.
- [ ] `Flag`: store abuse reports for local users mentioned in the object list.
- [ ] `Add` and `Remove`: inspect Concorde handling before deciding exact scope.
- [x] Refuse to ingest `Collection` / `OrderedCollection` as a single activity.

### Actor Resolution

- [x] Resolve local actors from MongoDB.
- [x] Resolve remote actors using signed GET when possible.
- [x] Validate actor type: `Person`, `Service`, `Group`, `Organization`,
      `Application`.
- [x] Require actor `id`, `inbox`, and `outbox` to belong to the expected host.
- [x] Validate `sharedInbox` and public key host.
- [ ] Validate `followers` and `following` host.
- [x] Validate `preferredUsername` against Concorde-compatible rules.
- [x] Truncate display name to Concorde-compatible limits.
- [ ] Truncate and store summary to Concorde-compatible limits.
- [x] Store remote public keys by `keyId`.
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

- [x] Sign POST requests with `rsa-sha256`.
- [x] Include `Date`, `Host`, `Content-Type`, `Digest`, `Signature`, and
      `User-Agent`.
- [x] Sign GET requests with `Accept`, `Date`, `Host`, and `Signature`.
- [x] Use `(request-target) date host digest` for POST signing strings.
- [x] Use `(request-target) date host accept` for GET signing strings.
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

- [x] Start HTTP routes and queue workers from the same `cmd/rosmarinus`
      process by default.
- [x] Create one Asynq client for enqueueing jobs from HTTP handlers and domain
      services.
- [x] Create one Asynq server in the same process for `inbox`, `deliver`,
      `system`, `poll-ended`, `media`, `metadata`, and `account-delete` jobs.
- [x] Register handlers through DI so job handlers use the same repositories,
      resolver, signer, renderer, and logger wiring as HTTP handlers.
- [x] Support graceful shutdown that stops accepting HTTP requests, stops
      enqueueing new jobs, drains or stops Asynq workers, then closes Redis and
      MongoDB clients.
- [x] Add config flags for advanced deployments:
      `run_http`, `run_workers`, and `worker_queues`, all enabled by default.
- [x] Keep the default documented deployment as one Rosmarinus process plus
      MongoDB and Redis.

### Queue Requirements

- [x] Redis-backed persistence.
- [x] At-least-once processing.
- [ ] Idempotent handlers keyed by AP URI where possible.
- [x] Worker concurrency per queue.
- [ ] Per-second rate limits:
      `deliver` defaults to 128/sec, `inbox` defaults to 16/sec.
- [x] Retry attempts:
      `deliver` defaults to 17, `inbox` defaults to 10.
- [x] AP backoff compatible with Concorde:
      `(attempts^4 + 15) * 1s`, capped at 8 hours, plus up to 20% jitter.
- [x] Job timeout:
      `deliver` defaults to 1 minute, `inbox` defaults to 5 minutes.
- [ ] Dead-letter or failed-job inspection.
- [ ] Ability to promote delayed `deliver` and `inbox` jobs for operations.
- [x] Structured payload versioning for safe migrations.

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

- [x] `actors`: unique `uri`
- [x] `actors`: unique sparse `{ usernameLower, host }`
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
- [x] `internal/activitypub/signature`: HTTP signatures, digest, signed GET/POST.
- [x] `internal/activitypub/client`: signed AP GET/POST HTTP client.
- [x] `internal/activitypub/resolver`: AP resolver and local URI resolver.
- [ ] `internal/activitypub/renderer`: actor, note, collection, emoji, follow,
      like, delete, update, accept, reject, undo renderers.
- [ ] `internal/activitypub/performer`: activity dispatch and handlers.
- [ ] `internal/activitypub/audience`: `to`/`cc` visibility parser.
- [ ] `internal/domain`: actor, note, follow, reaction, emoji, media, instance
      domain services.
- [ ] `internal/store/mongo`: MongoDB repositories and index setup.
- [x] `internal/queue`: Redis queue interfaces and implementation.
- [ ] `internal/cache`: Redis-backed caches and locks.
- [ ] `internal/mfm`: MFM/HTML conversion compatibility layer.

## Implementation Phases

### Phase 0: Project Foundation

- [x] Add config model for host, URL, MongoDB, Redis, HTTP, queue limits, and
      user agent.
- [x] Create app wiring with explicit DI.
- [x] Add graceful shutdown for HTTP, MongoDB, Redis, and workers.
- [x] Add MongoDB connection and index bootstrap.
- [x] Add Redis connection and health check.
- [x] Add logger injection.
- [x] Add basic tests for config validation.

### Phase 1: ActivityPub Types, Signatures, And Queues

- [x] Implement AP type helpers equivalent to Concorde `getApId`, `getApType`,
      actor/post/activity predicates, and array normalization.
- [x] Implement digest verification tests.
- [x] Implement HTTP Signature parse/verify tests with fixture requests.
- [x] Implement signed GET/POST tests with stable signing strings.
- [x] Implement Redis queues, workers, retry/backoff, and timeout handling.
- [x] Implement Redis AP locks and tests for duplicate lock behavior.

### Phase 2: Discovery And Actor Resolution

- [x] Implement WebFinger local responses.
- [x] Implement NodeInfo responses.
- [ ] Implement remote WebFinger client.
- [x] Implement actor renderer and `/users/{id}`.
- [x] Implement `/users/{id}/publickey`.
- [x] Implement remote actor create/update baseline.
- [x] Implement signed remote AP GET.
- [x] Add actor validation tests based on Concorde edge cases.

### Phase 3: Inbox MVP

- [x] Implement `/inbox` and `/users/{id}/inbox`.
- [x] Validate digest, signature, host, signer, and activity host.
- [x] Enqueue `inbox` jobs.
- [ ] Implement `Create Note` handler.
- [ ] Implement note resolver and note storage.
- [ ] Implement audience parser.
- [ ] Implement HTML to MFM conversion.
- [ ] Add golden tests for incoming Mastodon/Misskey-style notes.

### Phase 4: Outbound Delivery MVP

- [ ] Implement note renderer.
- [ ] Implement create/announce/like/follow renderers.
- [x] Implement basic `Accept(Follow)` delivery to remote `sharedInbox` or
      `inbox`.
- [x] Implement `deliver` worker.
- [ ] Implement delivery manager with sharedInbox dedupe for followers and
      direct recipients.
- [ ] Update instance send stats.
- [ ] Add delivery signing and retry tests.

### Phase 5: Social Graph And Reactions

- [x] Implement basic inbound `Follow` -> outbound `Accept`.
- [ ] Implement full `Follow`, `Accept`, `Reject`, and `Undo Follow`.
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

## Real Federation Test Plan

Use this once actor resolution, signed GET, Create Note ingestion, and delivery
workers are implemented enough to exchange basic activities.

### Local Test Topology

- [ ] Run MongoDB and Redis for Rosmarinus.
- [ ] Run Rosmarinus as a single binary with:
      `HOST=rosmarinus.example.test`,
      `PUBLIC_URL=https://rosmarinus.example.test`,
      `LOCAL_ACTOR_USERNAME=relay`,
      `LOCAL_ACTOR_TYPE=Service`.
- [ ] Run a separate Misskey/Concorde test instance with its own PostgreSQL and
      Redis. The archived `./concorde/docker-compose.yml` and
      `./concorde/packages/backend/test/docker-compose.yml` are useful
      references, but should not be edited.
- [ ] Put both services behind local DNS names and a reverse proxy. A practical
      local-only topology is:
      `rosmarinus.example.test -> 127.0.0.1:<rosmarinus-proxy-port>` and
      `misskey.example.test -> 127.0.0.1:<misskey-proxy-port>`.
- [ ] Prefer HTTPS even for local tests. If HTTPS is too costly for the first
      smoke test, run an HTTP-only test with explicit notes that it may miss
      implementations that require HTTPS in actor/object IDs.
- [ ] Do not use `localhost` in ActivityPub object IDs during federation tests;
      many implementations reject or mishandle it.

### Local Misskey Test Execution Draft

- [ ] Prepare a disposable Misskey/Concorde config with:
      `url: http(s)://misskey.example.test`,
      Postgres pointing to the test DB,
      Redis pointing to the test Redis,
      and federation enabled.
- [ ] Use the available JS package manager (`yarn`, `pnpm`, or `npm`) only in
      the Misskey/Concorde checkout or a disposable test checkout. Do not modify
      `./concorde` source files.
- [ ] Start Misskey web and queue worker locally, then create a local Misskey
      test account through its setup flow or seed script.
- [ ] Start Rosmarinus with `RUN_HTTP=true` and `RUN_WORKERS=true` so inbox and
      deliver queues run inside the same binary.
- [ ] Use Misskey search/follow UI or API to follow
      `@relay@rosmarinus.example.test`.
- [ ] Confirm Rosmarinus enqueues and processes:
      `inbox Follow -> signature verify -> deliver Accept`.
- [ ] Confirm Misskey marks the follow as accepted.

### Rosmarinus Visibility Checks From Misskey

- [ ] From Misskey, search `@relay@rosmarinus.example.test`.
- [ ] Confirm Misskey performs WebFinger and receives a `self` link.
- [ ] Confirm Misskey fetches `GET /users/{id}` and accepts the actor object.
- [ ] Confirm Misskey fetches `GET /users/{id}/publickey` or reads `publicKey`
      from the actor object.
- [ ] Confirm Rosmarinus access logs show the expected WebFinger and actor fetch
      sequence.

### Inbound Federation Checks

- [ ] From Misskey, follow the Rosmarinus actor.
- [ ] Confirm Rosmarinus receives `Follow` at `/inbox` or `/users/{id}/inbox`.
- [ ] Confirm digest, HTTP Signature parsing, host validation, and queue enqueue
      succeed.
- [ ] After signer resolution is implemented, confirm signature verification
      succeeds against the Misskey actor public key.
- [ ] Post a public Misskey note mentioning the Rosmarinus actor.
- [ ] Confirm Rosmarinus receives `Create` and stores the remote actor/note.

### Outbound Federation Checks

- [ ] Have Rosmarinus send an `Accept(Follow)` back to Misskey.
- [ ] Confirm Misskey marks the follow request accepted.
- [ ] Have Rosmarinus send a public `Create(Note)` to Misskey followers.
- [ ] Confirm Misskey displays or at least stores the note.
- [ ] Have Rosmarinus send `Like` or `Announce` for a Misskey note.
- [ ] Confirm Misskey accepts the activity and no retry loop remains in Asynq.

### Operational Checks

- [ ] Stop Rosmarinus during queued delivery, restart it, and confirm Asynq
      resumes pending jobs from Redis.
- [ ] Force a temporary Misskey outage and confirm AP backoff behavior resembles
      Concorde's delayed retry profile.
- [ ] Inspect failed jobs and logs for enough information to identify target
      inbox, actor, activity ID, and failure reason.
- [ ] Keep packet captures or structured logs for WebFinger, signed GET, inbox,
      and delivery requests as compatibility fixtures.

## Open Decisions

- [x] Choose the Redis queue implementation: use Asynq by default, hidden behind
      `internal/queue` interfaces.
- [ ] Decide whether Rosmarinus owns notifications/webhooks or only writes
      federation-visible state to MongoDB.
- [ ] Decide media ownership: store remote files, proxy URLs only, or delegate
      media storage to another service.
- [ ] Decide how much of Concorde's antenna/word-mute/timeline side effects
      belong in this microservice.
- [x] Decide local actor provisioning flow: bootstrap a local actor from
      environment variables such as `LOCAL_ACTOR_USERNAME`, store it in MongoDB,
      and keep it stable across restarts.
- [ ] Decide whether object IDs keep Concorde-compatible generated IDs or use
      MongoDB ObjectIDs/ULIDs.
