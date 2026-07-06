# Rosmarinus Implementation Plan

Rosmarinus is the ActivityPub microservice successor to Concorde. It should not
copy Concorde's frontend or Misskey API surface, but it should preserve the
federation behavior that other ActivityPub servers depend on.

Do not edit `./concorde`. Treat it as the behavioral reference. `./misskey`
is used only as a Docker federation test fixture/reference and must not drive
Rosmarinus implementation semantics.

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
- [x] `GET /users/{user}/outbox`
- [x] `GET /users/{user}/followers`
- [x] `GET /users/{user}/following`
- [x] `GET /users/{user}/collections/featured`
- [x] `GET /notes/{note}`
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

- [x] `Create`: merge activity/object audiences, fill missing `attributedTo`,
      resolve object, and create basic notes.
- [ ] `Create`: create questions/polls and full note side effects.
- [ ] `Announce`: resolve target note, check visibility, create renote.
- [ ] `Like`, `EmojiReaction`, `EmojiReact`: resolve target note, extract emoji
      tags, create reaction.
- [x] `Follow`: basic remote actor follows local actor path, enqueueing
      `Accept(Follow)` for unlocked local actors.
- [x] `Follow`: persist remote follower -> local followee relationships
      idempotently in MongoDB before enqueueing `Accept(Follow)`.
- [ ] `Follow`: block/lock/request persistence logic.
- [ ] `Accept`: accept local outgoing follow requests.
- [ ] `Reject`: reject local outgoing follow requests.
- [x] `Undo`: support remote `Undo(Follow)` for remote follower -> local
      followee relationships.
- [ ] `Undo`: support undo block, like, announce, and accept.
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
- [x] Validate `followers` and `following` host.
- [x] Validate `preferredUsername` against Concorde-compatible rules.
- [x] Truncate display name to Concorde-compatible limits.
- [ ] Truncate and store summary to Concorde-compatible limits.
- [x] Store remote public keys by `keyId`.
- [x] Store `inbox`, `sharedInbox`, `followersUri`, `followingUri`, and
      `featured`.
- [ ] Store bot/cat flags, discoverability, profile fields, birthday, location,
      avatar, banner, and custom emoji names where available.
- [ ] Refresh remote actors when stale, initially after 24 hours.
- [ ] Update featured notes from remote `featured` collections.

### Note Resolution And Creation

- [x] Resolve by AP URI with remote signed GET for Create object references.
- [ ] Resolve by AP URI with local URI parsing and full note resolver cache.
- [ ] Use Redis AP locks to deduplicate concurrent resolution by URI.
- [x] Validate post types: `Note`, `Question`, `Article`, `Audio`, `Document`,
      `Image`, `Page`, `Video`, `Event`.
- [x] Require `note.id` and `attributedTo` hosts to match actor host.
- [x] Reject unexpected non-HTTPS note IDs.
- [x] Parse audience from `to` and `cc` into `public`, `home`, `followers`,
      or `specified`.
- [x] Extract AP mentions and hashtags.
- [x] Preserve remote attachment metadata on notes.
- [ ] Resolve attachments as media records.
- [x] Store and render basic reply and quote URIs.
- [ ] Resolve replies and quotes.
- [x] Preserve basic Misskey compatibility fields: `_misskey_content` and
      `source.mediaType = text/x.misskeymarkdown`.
- [ ] Preserve full Misskey compatibility fields:
      `source.mediaType = text/x.misskeymarkdown`, `_misskey_quote`,
      `quoteUrl`, and `_misskey_talk`.
- [ ] Convert remote HTML to MFM-compatible text.
- [x] Store basic URI, attributedTo, author, text, content warning, sensitive,
      reply URI, quote URI, visibility, mentions, hashtags, emojis, raw AP
      object, createdAt, and publishedAt for remote notes.
- [x] Store basic remote attachment metadata.
- [ ] Store cached files, polls, URL, visible users, resolved reply/renote, and
      denormalized author fields.
- [ ] Update reply counts, renote counts, hashtags, and local notifications
      where Rosmarinus owns those writes.

### Custom Emoji

- [ ] Render local custom emojis as ActivityPub `Emoji` tags.
- [x] Extract remote `Emoji` tags from notes.
- [ ] Extract remote `Emoji` tags from actors.
- [x] Normalize `:name:` to `name`.
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
- [x] Idempotent inbound `Follow` persistence keyed by
      `{ followerId, followeeId }`.
- [ ] Idempotent handlers keyed by AP URI for notes, reactions, announces,
      and updates where possible.
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
- [x] `notes`
- [ ] `polls`
- [ ] `reactions`
- [x] `follows`
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
- [x] `notes`: unique sparse `uri`
- [x] `notes`: basic `{ authorId, createdAt }`
- [ ] `notes`: `userId`, `userHost`, `replyId`, `renoteId`, `createdAt`
- [ ] `notes`: tag and mention indexes suitable for MongoDB
- [x] `follows`: unique `{ followerId, followeeId }`
- [x] `follows`: basic `{ followerId, createdAt }` and
      `{ followeeId, createdAt }`
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
- [x] `internal/activitypub/notes`: minimum AP note validation, text/tag
      extraction, rendering, and audience compatibility helpers.
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
- [x] Add a distroless Dockerfile that runs Rosmarinus as `nonroot`.
- [x] Add Docker compose fixtures for Rosmarinus smoke testing and for attaching
      Rosmarinus to Misskey's local federation topology.
- [x] Add GitHub Actions workflow showing the Docker smoke test and the
      workflow-dispatch Misskey federation fixture shape.

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
- [x] Implement Concorde-shaped local actor `outbox`, `followers`,
      `following`, and `featured` AP collections with empty contents until
      social graph and local note ownership are implemented.
- [x] Implement remote actor create/update baseline.
- [x] Implement signed remote AP GET.
- [x] Add actor validation tests based on Concorde edge cases.

### Phase 3: Inbox MVP

- [x] Implement `/inbox` and `/users/{id}/inbox`.
- [x] Validate digest, signature, host, signer, and activity host.
- [x] Enqueue `inbox` jobs.
- [x] Implement basic `Create Note` handler.
- [x] Implement minimum note object parser compatible with Concorde tests.
- [x] Implement basic note storage.
- [ ] Implement full note resolver.
- [x] Implement initial audience parser.
- [x] Extract AP note mentions, hashtags, and emoji tags.
- [x] Store and render note CW, sensitive, inReplyTo, and quote URLs.
- [x] Store and render basic note attachments.
- [ ] Implement HTML to MFM conversion.
- [ ] Add golden tests for incoming Mastodon/Misskey-style notes.

### Phase 4: Outbound Delivery MVP

- [x] Implement basic note renderer.
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
- [x] Persist inbound remote `Follow` relationships in MongoDB.
- [x] Implement inbound remote `Undo(Follow)` for local followees.
- [ ] Implement full `Follow`, `Accept`, and `Reject`.
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

## Concorde Backend Test Port Status

The following tests from `./concorde/packages/backend/test` have been reviewed.
Only ActivityPub/federation behavior that belongs to Rosmarinus is ported.
Frontend, Misskey API, streaming, drive, and timeline tests are intentionally
out of scope unless they expose ActivityPub persistence semantics Rosmarinus
must write to MongoDB.

### Ported

- [x] `ap-request.ts`
  - [x] `createSignedPost with verify`
  - [x] `createSignedGet with verify`
- [x] `activitypub.ts`
  - [x] `Parse minimum object / Minimum Actor`
  - [x] `Parse minimum object / Minimum Note` as AP note parser and basic
        DB-backed note creation behavior.
  - [x] `Truncate long name / Actor`
- [x] `fetch-resource.ts`
  - [x] AP `GET /@:username` and `GET /users/:id` return
        `application/activity+json` when AP is requested.
  - [x] AP `GET /users/:id/outbox`, `followers`, `following`, and
        `collections/featured` return Concorde-shaped `OrderedCollection`
        / `OrderedCollectionPage` responses.
  - [x] AP `GET /notes/:id` returns `application/activity+json` for stored
        notes.
  - [x] inbox `Accepted`.
  - [x] inbox `Invalid Host`.
  - [x] inbox `Payload Too Large`.
  - [x] inbox `Signature Header Required`.
  - [x] inbox `Digest Header Required`.
  - [x] inbox `Invalid Digest Header`.
  - [x] inbox `Unsupported Digest Algorithm`.
  - [x] inbox `Digest Mismatch`.

### Not Ported Yet

- [ ] `activitypub.ts / Minimum Note` full equivalent with all Concorde note
      side effects.
- [ ] `fetch-resource.ts / /notes/:id` exact Concorde local-vs-remote redirect
      semantics once local note ownership exists.
- [ ] `fetch-resource.ts / HTML`, root/docs/assets, RSS/ATOM/JSON feeds:
      out of scope for Rosmarinus unless a future federation requirement needs
      them.
- [ ] API visibility, note API, block/mute/thread-mute/streaming tests:
      out of scope for Rosmarinus API/frontend, but should be mined later for
      MongoDB state side effects that ActivityPub handlers must preserve.

## Real Federation Test Plan

Use this once actor resolution, signed GET, Create Note ingestion, and delivery
workers are implemented enough to exchange basic activities.

Implementation compatibility should be checked against `./concorde`. The
latest Misskey checkout in `./misskey` is used only to run a real Docker-based
federation peer and to inspect its official local federation test topology.

### CI Shape

- [x] Build Rosmarinus with a distroless runtime image:
      `gcr.io/distroless/static-debian12:nonroot`.
- [x] Set the image `USER` to `nonroot:nonroot`.
- [x] Exclude `./concorde` and `./misskey` from the Docker build context.
- [x] In GitHub Actions, run `go test ./...`.
- [x] In GitHub Actions, build `rosmarinus:test` and assert the image user is
      `nonroot:nonroot`.
- [x] In GitHub Actions, start Rosmarinus with MongoDB and Redis through
      `docker/federation/rosmarinus.compose.yml` and check `/healthz`.
- [x] Add a workflow-dispatch-only Misskey fixture job that expects `./misskey`
      to be present, builds Misskey, starts its official federation harness, and
      adds Rosmarinus as `https://rosmarinus.test` through an overlay compose.
- [ ] Add Rosmarinus-specific Misskey federation tests that drive follow,
      Accept(Follow), signed GET, and Create(Note) once the missing behavior is
      implemented.
- [ ] Run `docker compose config` and the workflow on a Docker-enabled machine;
      this local workspace currently lacks the `docker` CLI.

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

- [ ] Inspect `./misskey/packages/backend/test-federation` only for Docker
      topology, service names, local DNS, and test command conventions.
- [x] Add `docker/federation/misskey-rosmarinus.compose.yml` to attach
      Rosmarinus, MongoDB, Redis, and `nginx` TLS endpoint `rosmarinus.test` to
      Misskey's federation network.
- [x] Add `docker/federation/nginx/rosmarinus.test.conf` to proxy
      `https://rosmarinus.test` to the Rosmarinus HTTP server.
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
