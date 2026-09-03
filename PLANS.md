# Rosmarinus Implementation Plan

Rosmarinus is the integrated Go backend successor to Concorde. It owns
ActivityPub federation, local accounts, passkey authentication, sessions,
local Actor management, and the REST API consumed by the Salvia React
SPA. It should not copy Misskey's public API surface, but it should preserve the
federation behavior that other ActivityPub servers depend on.

Do not edit `./misskey` or `./concorde`. Treat the current `./misskey` backend
and its tests as the primary federation reference. Treat `./concorde` as a
secondary historical reference for predecessor behavior and regression
investigation only; current Misskey wins when their federation semantics
differ unless this plan records an intentional Rosmarinus exception.

## Goals

- Implement ActivityPub federation in Go.
- Use MongoDB as the primary database.
- Use Redis for job queues, delayed retries, rate limiting, distributed AP
  locks, and local Pub/Sub fan-out between Rosmarinus processes.
- Own local accounts, passkey credentials and challenges, sessions, Actor
  ownership, application data, and federation state in Rosmarinus.
- Serve an authenticated REST API and SSE stream to the React SPA. REST handles
  browser requests and mutations; SSE carries server-to-browser updates. Redis
  is an internal transport and is never a browser-facing API.
- Build Salvia from `./salvia` as a static React SPA without Next.js or a
  separate backend. Rosmarinus may serve the built assets or deploy them behind
  the same-origin reverse proxy.
- Preserve the old Salvia product direction from `./salvia/salvia-old`:
  passkey-only authentication, one account managing multiple Actors, a
  simplified Misskey-inspired interface, yellow default theme, custom emoji
  reactions, and Tabler Icons.
- Keep dependencies injectable through interfaces.
- Use Go's `log` package through injected `*log.Logger` values.
- Add focused tests for parsing, signing, resolving, rendering, and queue behavior.
- At each federation checkpoint, inspect the current `./misskey` implementation
  and relevant tests and record the Misskey commit used as the behavior
  baseline in the checkpoint or commit notes.
- Use `./concorde` to explain historical differences and mine regression
  fixtures, not as the default source for new federation semantics.
- Extend the latest-Misskey federation test whenever an implementation
  checkpoint adds or materially changes externally observable federation
  behavior. Unit tests remain required for edge cases; the Docker federation
  test verifies the corresponding end-to-end interoperability path when the
  behavior can be exercised through Misskey's public API.
- Review every implementation checkpoint for Salvia integration impact. When a
  change affects REST API/SSE contracts, passkey or session behavior,
  ownership or authorization rules, or federation state consumed by Salvia,
  update the applicable `docs/salvia-integration.md`, `docs/salvia/AGENTS.md`,
  and `docs/salvia/PLANS.md` documents in the same checkpoint.

## Implementation Checkpoint Definition Of Done

- [ ] Current Misskey source and relevant unit/federation tests were reviewed
      for the changed federation behavior, and intentional deviations are
      documented in tests or handoff notes.
- [ ] Focused unit/integration tests cover the changed behavior.
- [ ] If the behavior is observable through the existing real-Misskey fixture,
      `test/federation/misskey_test.go` includes or updates a clearly commented
      phase and the federation workflow documentation remains accurate.
- [ ] If the Salvia integration contract changes, the applicable Salvia
      handoff documents are updated; otherwise the change is confirmed to be
      internal-only.
- [ ] Formatting, tests, and relevant static checks pass before a signed
      commit is created.

## Federation Reference Baseline

The reference-policy audit on 2026-08-23 found that Concorde and current
Misskey still share the same broad ActivityPub model, but they are no longer
close enough for Concorde to be the primary implementation guide:

- `./concorde` at `a49b1d8c` (`12.25Q4.1`) and `./misskey` at `33f53280`
  (`2026.8.0-alpha.0`) share Misskey `12.119.1` commit `fccd9c32` from
  2022-12-03 as their merge base.
- After that base, the inspected federation paths contain 54 Concorde commits
  and 256 Misskey commits. The current Misskey side changed 124 files with
  roughly 9,256 insertions and 3,757 deletions in those paths.
- Both still recognize the same core Create/Delete/Update/Follow/Accept/Reject/
  Add/Remove/Announce/Like/Undo/Block/Flag activities, post and Actor types,
  `_misskey_reaction || content || name` reaction precedence, and much of the
  Actor/Note validation shape.
- Current Misskey additionally implements `Move`, bounded and host-checked
  collection ingestion, stricter resolver and URL checks, LD-signature fallback,
  newer retry behavior, and additional Actor/Note validation. It also removed
  `Accept` from signed GET headers and changed several older validation details.

Therefore, similarity may reduce porting work, but it must be established per
behavior rather than assumed.

Use these current Misskey files as the initial code map, following their
dependencies when a checkpoint needs more detail:

- `packages/backend/src/core/activitypub/ApInboxService.ts` for activity
  dispatch and per-activity behavior.
- `packages/backend/src/server/ActivityPubServerService.ts`,
  `queue/processors/InboxProcessorService.ts`, and
  `core/activitypub/ApRequestService.ts` for inbox validation and HTTP
  signatures.
- `core/activitypub/ApResolverService.ts` and `misc/check-against-url.ts` for
  resolution, recursion, redirects, and URL trust boundaries.
- `core/activitypub/models/ApPersonService.ts`, `ApNoteService.ts`, and
  `ApQuestionService.ts` for Actor, Note, emoji, and poll ingestion.
- `core/activitypub/ApRendererService.ts`, `ApDeliverManagerService.ts`, and
  `queue/QueueProcessorService.ts` for rendering, delivery, and retry behavior.
- `packages/backend/test/unit` and `packages/backend/test-federation/test` for
  executable behavior examples.

### Intentional Rosmarinus Differences

- Rosmarinus requires explicit local approval for every inbound follow. Current
  Misskey's per-user follow policy must not reintroduce auto-acceptance.
- Rosmarinus uses MongoDB and exposes ActivityPub plus a purpose-built,
  authenticated REST API for Salvia. Misskey's PostgreSQL entities,
  public API compatibility, and unrelated side effects are out of scope.
- Rosmarinus uses `github.com/go-fed/httpsig`. Match current Misskey's wire
  behavior without copying its Node signature implementation.
- Rosmarinus authenticates federation with HTTP Signatures only. It does not
  implement JSON-LD `RsaSignature2017` fallback or relay flows that require
  Linked Data signatures.
- Rosmarinus keeps completed inbound Activity IDs in MongoDB for seven days by
  default. Current Misskey retains completed and failed BullMQ inbox jobs for
  up to seven days, but does not use the Activity URI as a durable domain
  receipt. The stricter Rosmarinus receipt prevents sequential peer retries or
  queue replays from repeating federation side effects.
- Salvia is a static client of Rosmarinus's same-origin REST API and SSE stream.
  It never accesses MongoDB or Redis directly.

### Current-Misskey Baseline Reconciliation

Complete this audit before treating already-implemented Concorde-derived paths
as final:

- [ ] Compare all completed inbox, resolver, renderer, and delivery behavior
      against the pinned current Misskey commit and add focused regression
      tests for every material difference.
- [x] Align signed GET with current Misskey's
      `(request-target) date host` header list; retain `Accept` as an unsigned
      request header.
- [x] Require a string activity ID for `Accept` and `Reject`, matching current
      Misskey instead of retaining the historical ID-less allowance.
- [x] Replace blanket Collection/OrderedCollection refusal with current
      Misskey's bounded, signer-host-checked ingestion behavior.
- [x] Implement and test `Move` with alias validation and following migration
      semantics appropriate to Rosmarinus ownership boundaries.
- [x] Port current Misskey's remote-fetch protections: blocked-host
      enforcement (including subdomains and redirects), strict request/final/
      object-ID consistency, ActivityStreams context checks, HTTPS downgrade
      refusal, federation-loop fragment restrictions, and post-DNS private,
      loopback, link-local, documentation, and reserved-address rejection.
- [x] Add per-Collection URL history and nesting-depth limits, and resolve
      local Actors from MongoDB without issuing HTTP requests back to
      Rosmarinus.
- [x] Reconcile Actor validation differences, including optional outbox values
      and ignoring an invalid shared inbox while retaining the individual
      inbox, rather than rejecting an otherwise usable Actor.
- [x] Reconcile Note validation differences, including published timestamp
      safety, sender/attribution equality, HTTPS note URLs, and raw AP mention
      limits.
- [x] Align inbox/delivery retry defaults and exponential backoff with current
      Misskey, accounting explicitly for Asynq's attempt numbering.
- [ ] Mine current Misskey's `test/unit/activitypub.ts`, `test/unit/ap-request.ts`,
      and `test-federation/test` cases as the primary compatibility fixtures.
      Keep Concorde fixtures only as supplemental historical regressions.

## Federation Surface To Implement

Current Misskey does more than just receive inbox activities. The relevant
federation surface is spread across ActivityPub routes, resolver and renderer
services, queue processors, note/follow services, instance metadata services,
and federation tests.

Rosmarinus intentionally differs from current Misskey in follow approval
policy: Rosmarinus requires local user approval for every inbound follow.
Inbound `Follow` activities must be stored as pending requests and must not
deliver `Accept(Follow)` until an explicit local approval action occurs.

### Public Discovery Endpoints

- [x] `GET /.well-known/host-meta`
- [x] `GET /.well-known/host-meta.json`
- [x] `GET /.well-known/webfinger?resource=...`
- [x] `GET /.well-known/nodeinfo`
- [x] `GET /nodeinfo/2.0`
- [x] `GET /nodeinfo/2.1` if we decide to expose it

Current Misskey uses WebFinger to map `acct:user@example.com` and local Actor URLs to
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
- [x] `GET /users/{user}/followers?page=true` backed by stored follows.
- [x] `GET /users/{user}/following?page=true` backed by stored follows.
- [x] `GET /notes/{note}`
- [x] `GET /notes/{note}/activity`
- [x] `GET /emojis/{emoji}`
- [x] `GET /likes/{like}`
- [x] `GET /follows/{follower}/{followee}`

Responses should negotiate `application/activity+json` and
`application/ld+json; profile="https://www.w3.org/ns/activitystreams"` in the
the same style as current Misskey.

### Inbox Validation

- [x] Read raw request body and limit it to current Misskey's 64 KiB.
- [x] Parse JSON after preserving the raw body.
- [x] Require and verify `Digest: SHA-256=...`.
- [x] Parse HTTP Signature.
- [x] Use `github.com/go-fed/httpsig` for HTTP Signature RSA sign/verify
      behavior instead of hand-rolled crypto or Node-side
      `@peertube/http-signature` semantics.
- [x] Accept only supported algorithms such as `rsa-sha256` and compatible
      hs2019 forms.
- [x] Require signed `(request-target)`, `digest`, `host`, and `date` for POST.
- [x] Verify `Host` against the configured local host.
- [x] Resolve signer by `keyId`; fall back to resolving `activity.actor`.
- [x] Reject old `acct:` key IDs.
- [x] Verify signature using the actor public key.
- [x] Require signer actor URI to match `activity.actor`.
- [x] Require every `activity.id` to be a string and its host to match the
      verified signer host, including for `Accept` and `Reject`.
- [x] Update instance communication stats after authenticated requests.
- [x] Enqueue the activity and return `202 Accepted`.

### Activity Processing

- [x] `Create`: merge activity/object audiences, fill missing `attributedTo`,
      resolve object, and create basic notes.
- [x] `Create`: create questions/polls and durable note side effects.
- [x] `Announce`: resolve target note and create a basic renote record.
- [x] `Announce`: reject specified Notes, other Actors' followers-only Notes,
      pure Announce targets, blocks, and blocked hosts using current Misskey's
      sharing rules.
- [x] `Announce`: expose normalized, indexed renote records for exact counts.
- [x] `Like`, `EmojiReaction`, `EmojiReact`: resolve target note and create
      or replace the actor's reaction using current Misskey-compatible
      `_misskey_reaction || content || name` precedence.
- [x] `Like`, `EmojiReaction`, `EmojiReact`: extract emoji tags and persist
      notification side effects.
- [x] `Like`, `EmojiReaction`, `EmojiReact`: expose normalized, indexed
      reaction records for exact aggregate counts.
- [x] `Follow`: store every remote follower -> local followee request as
      pending and do not enqueue `Accept(Follow)` automatically.
- [x] `Follow`: basic internal approval path transitions a pending request into
      an accepted relationship and enqueues `Accept(Follow)`.
- [x] `Follow`: expose the domain operation needed for user-facing approval.
      Replace the legacy Connector adapter with an authenticated application
      API endpoint during the backend-integration phase.
- [x] `Follow`: reject blocked pairs, persist pending requests, and prevent
      approval after either direction becomes blocked.
- [x] `Accept`: accept local outgoing follow requests.
- [x] `Reject`: reject local outgoing follow requests.
- [x] `Undo`: support remote `Undo(Follow)` for remote follower -> local
      followee relationships.
- [x] `Undo`: support remote `Undo(Like)`, `Undo(EmojiReaction)`, and
      `Undo(EmojiReact)` by deleting the actor's stored note reaction.
- [x] `Undo`: support remote `Undo(Announce)` by deleting the stored renote.
- [x] `Undo`: support remote `Undo(Block)` by deleting stored local block state.
- [x] `Undo`: support undo accept while verifying the remote acceptor and the
      embedded Follow endpoints before deleting a local-to-remote relationship.
- [x] `Delete`: delete remote notes when the deleting actor is the stored note
      author.
- [x] `Delete`: tombstone remote actors and enqueue account cleanup tasks.
- [x] `Delete`: implement full cascaded account cleanup side effects.
- [x] `Delete`: close an account-owned local Actor through `actor.delete`, hide
      it from normal lookup immediately, deliver a signed Actor deletion to
      known remote peers, and retain the tombstone/key needed for retries.
- [x] `Update`: authenticate remote Actor updates, require the updated Actor ID
      to match the signer, and refresh the stored remote Actor.
- [x] `Update`: patch account-owned local Actor profiles through an authorized
      `actor.update` command and fan out a full current Person/Service object to
      accepted, active, unblocked remote followers with shared-inbox dedupe.
- [x] `Update`: refresh Question vote counts; generic `Update(Note)` remains
      intentionally unsupported like current Misskey.
- [x] `Block`: create local block state for remote actor against local actor.
- [x] `Flag`: store abuse reports for local users mentioned in the object list.
- [x] `Add` and `Remove`: authenticate remote featured-collection changes,
      require the Note to belong to the sending Actor, and update the Actor's
      bounded `featuredNoteIds` state atomically.
- [x] Ingest `Collection` / `OrderedCollection` activities only when they stay
      below the current Misskey recursion limit, resolving and processing each
      signer-hosted activity independently.
- [x] `Move`: validate reciprocal Actor aliases and migrate eligible local-to-
      remote following
      relationships without violating local Actor ownership.

### Actor Resolution

- [x] Resolve local actors from MongoDB.
- [x] Resolve remote actors using signed GET when possible.
- [x] Validate actor type: `Person`, `Service`, `Group`, `Organization`,
      `Application`.
- [x] Require Actor `id` and `inbox` to belong to the expected host; validate
      an optional `outbox` when present.
- [x] Validate the public key host and ignore an invalid `sharedInbox` so
      delivery safely falls back to the individual inbox.
- [x] Validate `followers` and `following` host.
- [x] Validate `preferredUsername` against current Misskey-compatible rules.
- [x] Truncate display name to current Misskey-compatible limits.
- [x] Convert, truncate, and store summary to current Misskey-compatible
      limits.
- [x] Store remote public keys by `keyId`.
- [x] Store `inbox`, `sharedInbox`, `followersUri`, `followingUri`, and
      `featured`.
- [x] Store `movedToUri`, `alsoKnownAs`, and `movedAt`, and expose them as
      additive fields in Salvia's read-only Actor projection.
- [x] Store bot/cat/locked/discoverability flags, profile fields, birthday,
      location, HTTPS profile/avatar/banner URLs, hashtags, and custom emoji
      names where available.
- [x] Refresh remote actors when stale after 24 hours, retaining the last
      validated Actor if a refresh temporarily fails.
- [x] Update up to five resolved featured Note IDs from remote `featured`
      collections without failing an otherwise valid Actor refresh.

### Note Resolution And Creation

For MFM behavior, use the current `./misskey` backend and the matching
`./mfm.js` checkout (currently MFM.js 0.26.0) as the compatibility references.
Concorde's older MFM behavior must not constrain Salvia-authored notes.

- [x] Resolve by AP URI with remote signed GET for Create object references.
- [x] Resolve local/cached notes by AP URI from MongoDB and fetch/store missing
      remote notes through the shared strict resolver.
- [x] Use Redis AP locks to deduplicate concurrent note and Announce target
      resolution by URI.
- [x] Validate post types: `Note`, `Question`, `Article`, `Audio`, `Document`,
      `Image`, `Page`, `Video`, `Event`.
- [x] Require `note.id` and `attributedTo` hosts to match actor host.
- [x] Reject unexpected non-HTTPS note IDs.
- [x] Parse audience from `to` and `cc` into `public`, `home`, `followers`,
      or `specified`.
- [x] Extract AP mentions and hashtags.
- [x] Preserve remote attachment metadata on notes.
- [x] Preserve validated remote attachment, avatar, banner, and emoji source
      URLs directly for frontend presentation.
- [x] Store and render basic reply and quote URIs.
- [x] Resolve replies and quotes recursively with URL history/depth protection.
- [x] Preserve basic Misskey compatibility fields: `_misskey_content` and
      `source.mediaType = text/x.misskeymarkdown`.
- [x] Preserve current Misskey compatibility fields:
      `source.mediaType = text/x.misskeymarkdown`, `_misskey_quote`, and
      `quoteUrl`, including fallback across distinct quote candidates. The
      historical Concorde-only `_misskey_talk` extension is intentionally not
      emitted because it is absent from current Misskey.
- [x] Convert remote HTML to MFM-compatible text matching the observed behavior of
      current Misskey's `MfmService.fromHtml` (MFM.js 0.26 compatibility),
      including links, mentions, hashtags, formatting, code, quotes, and ruby.
- [x] Parse Salvia-authored MFM with current MFM.js-compatible syntax and render
      its AST to safe ActivityPub HTML matching current Misskey behavior.
- [x] Omit `_misskey_content` and `source` for simple MFM ASTs, and include them
      for advanced MFM, matching current Misskey `ApMfmService.getNoteHtml`.
- [x] Port focused MFM.js 0.26 parser fixtures for functions, nesting limits,
      mentions, URLs, emoji codes, code, math, plain blocks, and malformed input.
- [x] Store basic URI, attributedTo, author, text, content warning, sensitive,
      reply URI, quote URI, visibility, mentions, hashtags, emojis, raw AP
      object, createdAt, and publishedAt for remote notes.
- [x] Store basic renote/Announce target references.
- [x] Store basic remote attachment metadata.
- [x] Soft-delete remote notes on inbound `Delete(Note/Tombstone)`.
- [x] Store resolved `replyId` and `quoteId` alongside their AP URIs.
- [x] Keep remote media out of the backend cache and expose validated direct
      source URLs to Salvia.
- [ ] Store denormalized author fields where justified by measured query cost.
- [x] Store remote poll state separately from Question Notes.
- [x] Store the resolved ActivityPub audience URIs for specified Notes.
- [x] Keep reply, renote, and reaction state normalized and expose indexed
      aggregation fields to Salvia instead of crash-prone cross-document
      counters; store hashtags directly on Notes.
- [x] Persist local notifications for inbound federation activities.

### Custom Emoji

- [x] Resolve `post.create` local emoji names from Rosmarinus-owned records and
      render them as ActivityPub `Emoji` tags.
- [x] Resolve local custom emoji reactions, normalize them to Misskey's
      `:name@.:` form, and include their `Emoji` tag in `Like` and `Undo(Like)`.
- [x] Extract remote `Emoji` tags from notes.
- [x] Extract remote `Emoji` names and direct icon URLs from Actor tags.
- [x] Normalize `:name:` to `name`.
- [x] Upsert Note, Actor, and reaction emoji tags by `(host, name)`.
- [x] Update existing emoji if AP URI, remote updated timestamp, or original
      URL changes.

### Polls

- [x] Render local polls as `Question` with `oneOf` or `anyOf`.
- [x] Extract remote `Question` choices, vote counts, multiplicity, and expiry.
- [x] Accept authenticated `Update(Question)` vote-count refreshes without
      allowing the remote Actor to replace stored choice identity/order.
- [x] Support vote activities represented as notes replying to poll notes.
- [x] Enqueue delayed poll-ended work in Redis and notify the local owner and
      local voters idempotently when it runs.
- [x] Deliver question updates to remote followers when votes change.

### Delivery

- [x] Sign POST requests with `rsa-sha256`.
- [x] Include `Date`, `Host`, `Content-Type`, `Digest`, `Signature`, and
      `User-Agent`.
- [x] Sign GET requests with `Accept`, `Date`, `Host`, and `Signature`.
- [x] Delegate HTTP Signature cryptographic signing and verification to
      `github.com/go-fed/httpsig`, while preserving current Misskey-compatible header
      lists and signing string shape.
- [x] Use `(request-target) date host digest` for POST signing strings.
- [x] Use current Misskey's `(request-target) date host` list for GET signing
      while continuing to send the unsigned `Accept` header.
- [x] Build paginated delivery inbox lists from followers and direct
      recipients.
- [x] Prefer `sharedInbox` for follower fan-out and deduplicate destinations;
      retain individual inboxes for direct activities.
- [x] Skip configured blocked hosts and Actor block relationships.
- [x] Skip suspended Actors retained in existing relationships and prevent
      resolver refresh from resurrecting tombstoned Actors.
- [x] Update instance send stats on success and failure.
- [x] Suspend delivery to instances that have failed continuously for seven
      days, matching current Misskey's automatic suspension threshold.
- [x] Treat non-retryable HTTP failures as terminal and mark a host gone when
      its shared inbox returns `410`, matching current Misskey delivery policy.

### Instance Metadata

- [x] Register instances by host on first authenticated contact or delivery.
- [x] Track NodeInfo users/notes and accepted directional follow counts.
- [x] Track latest sent/received timestamps and status codes.
- [x] Fetch remote `.well-known/nodeinfo` with 2.1, 2.0, then 1.0 preference.
- [x] Fetch remote root HTML and linked or conventional `manifest.json`.
- [x] Store software name/version, open registrations, maintainer, name,
      description, icon, favicon, and theme color.
- [x] Refresh metadata at most daily unless forced.

## Redis Queue Design

Current Misskey uses BullMQ with Redis. Rosmarinus should likewise use Redis as
the queue backend, not an in-memory worker. The default implementation should
use Asynq, wrapped by `internal/queue` interfaces so ActivityPub services do not
depend on Asynq directly.

Rosmarinus should remain a single application binary by default. The HTTP server,
Asynq client, and Asynq worker server should run in the same process, sharing the
same DI container and graceful shutdown path. Redis is still required as external
state, but no separate worker service should be required for normal operation.

The queue layer should still allow future split-worker operation through config
flags, but that is an operational option, not the default architecture.

### Queue Names

- [x] `inbox`: accepted inbound ActivityPub activities.
- [x] `deliver`: outbound ActivityPub delivery jobs.
- [ ] `system`: scheduled maintenance.
- [x] `poll-ended`: delayed poll expiration work.
- [x] `metadata`: remote instance metadata refresh.
- [x] `account-delete`: remote actor delete cleanup task payload/enqueue path.
- [x] `account-delete`: validate the tombstoned remote Actor and idempotently
      clean its Notes, reactions, relationships, polls, and notifications.

### Single Binary Runtime

- [x] Start HTTP routes and queue workers from the same `cmd/rosmarinus`
      process by default.
- [x] Create one Asynq client for enqueueing jobs from HTTP handlers and domain
      services.
- [x] Create one Asynq server in the same process for `inbox`, `deliver`,
      `system`, `poll-ended`, `metadata`, and `account-delete` jobs.
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
- [x] Idempotent inbox processing keyed by the verified Activity URI, with an
      expiring processing lease and completed receipts retained for seven
      days by default. Handler-level unique indexes remain the final guard for
      retries after ambiguous partial failures.
- [x] Worker concurrency per queue.
- [x] Redis-backed per-second rate limits matching current Misskey:
      `deliver` defaults to 128/sec and `inbox` defaults to 32/sec, with
      independent concurrency defaults of 128 and 16.
- [x] Default to 11 `deliver` retries and 7 `inbox` retries in Asynq, equivalent
      to current Misskey's 12 and 8 total attempts including the initial run.
- [x] Use current Misskey's HTTP-related backoff:
      `(2^attempts - 1) * 1m`, capped at 8 hours, plus up to 20% jitter; verify
      the equivalent Asynq attempt index in focused tests.
- [x] Job timeout:
      `deliver` defaults to 1 minute, `inbox` defaults to 5 minutes.
- [x] Dead-letter or failed-job inspection through the queue operations CLI.
- [x] Ability to promote delayed, retrying, or archived `deliver` and `inbox`
      jobs for operations.
- [x] Structured payload versioning for safe migrations.

### Redis Locks And Cache

- [x] Acquire the Redis AP lock for each verified inbound activity ID before
      applying federation side effects, and release it with an independent
      bounded context.
- [x] AP object lock by URI for note and Announce target resolution.
- [x] Extend AP object locking to missing/stale Actor refresh paths.
- [x] Instance metadata lock by host.
- [x] Redis public key cache keyed by `keyId`, using current Misskey's 12-hour
      TTL and write-path invalidation.
- [x] Redis Actor URI cache with current Misskey's five-minute TTL and
      write-path invalidation.
- [x] Redis instance/suspended-host cache with a one-hour TTL and immediate
      health/suspension update propagation.
- [x] Redis WebFinger cache with a short five-minute TTL.

## Integrated REST And SSE Backend

The target architecture replaces the Ably Connector and the separate Next.js
backend. Rosmarinus authenticates the user, authorizes the selected Actor,
executes REST operations, reads MongoDB, and returns browser-safe
projections. Salvia is an untrusted static client.

```text
Salvia React SPA
  | same-origin HTTPS: passkey ceremonies, REST API, SSE
  v
Rosmarinus Go backend ----> MongoDB (all durable application/federation state)
  |
  +-----------------------> Redis (queues, locks, rate limits, local Pub/Sub)
```

- [x] Remove Ably dependencies, credentials, channels, subscribers, publishers,
      and deployment configuration.
- [x] Replace Connector commands with versioned authenticated REST endpoints.
      Preserve the existing domain services and idempotency guarantees where a
      retry can duplicate a federation side effect.
- [x] Add an authenticated server-to-browser SSE stream. Publish only opaque
      account/Actor-scoped invalidation events through Redis Pub/Sub; each
      Rosmarinus instance re-authorizes its connected clients and reads durable
      state from MongoDB.
- [x] Never expose Redis credentials, channel access, or raw MongoDB documents
      to the SPA. Redis Pub/Sub is best-effort and local to the deployment;
      reconnect always reconciles through the REST API.
- [x] Delete `connector_command_receipts` after equivalent HTTP idempotency has
      migrated to a transport-neutral receipt collection or endpoint-specific
      idempotency mechanism.

### Passkey Authentication And Sessions

- [x] Implement one-time initial administrator setup when no account exists;
      close public registration after the first account is created.
- [x] Support WebAuthn/passkeys only. Store credentials, short-lived single-use
      challenges, and revocable sessions in Rosmarinus-owned MongoDB
      collections.
- [x] Validate RP ID, expected origins, ceremony type, challenge expiry and
      consumption, user verification, signatures, and credential counters in
      the Go backend.
- [x] Use secure, HTTP-only, same-site session cookies; rotate sessions after
      authentication and reject suspended or deleted accounts on every
      state-changing REST request and SSE connection.
- [x] Protect cookie-authenticated mutations against CSRF and apply endpoint-
      appropriate rate limits through Redis.

### REST API And Multiple Actors

- [x] Keep accounts and ActivityPub Actors distinct. One active account may own
      any number of local Actors; each user-managed local Actor has exactly one
      `ownerAccountId`.
- [x] Retain `FindOwnedLocalByID(ctx, accountID, actorID)` as the authorization
      boundary for Actor-scoped domain operations. The SPA-selected Actor is
      input, never proof of ownership.
- [x] Provide browser-safe REST representations and stable pagination for
      Actors, timelines, notes, notifications, follows, reactions, polls,
      instances, and settings. Do not expose unrestricted collection-query
      endpoints.
- [x] Add REST endpoints for the already implemented Actor, post, follow,
      reaction, block, poll, and notification domain operations.
- [x] Return structured, versioned errors and use conventional HTTP status
      codes. Accept an idempotency key for mutations that may be retried after
      an ambiguous network result.
- [x] Scope SSE events and payloads to the authenticated account and,
      where applicable, include `actor_id` so one session supports multiple
      Actors.

### Completed Ably Connector Migration

The transport-independent operation DTOs and domain behavior remain in
`internal/connector` for compatibility with the federation worker. Ably
publishers/subscribers, channel code, SDKs, environment variables, legacy
command receipts, and the Salvia-owned account projection have been removed.
HTTP idempotency now uses `api_idempotency_receipts`.

## Legacy Shared MongoDB Boundary

This section records the former two-backend ownership model for migration.
Under the target architecture Rosmarinus is the only backend and owns every
runtime collection. Salvia has no MongoDB credentials. The current deployment
bootstrap no longer creates legacy roles or cross-service projections, so
existing `salvia_*` documents must be migrated offline before deploying it.

The former split ownership used `salvia_*` collections, separate MongoDB roles,
and a read-only cross-service account projection. The migration must preserve
account IDs, Actor ownership, lifecycle status, settings, and active session
cutover behavior while making Rosmarinus the owner of all surviving
collections and indexes. This is migration input only; no new split-ownership
collection should be added.

## MongoDB Collections

### Core Collections

- [x] `accounts` (passkey credentials are embedded)
- [x] `webauthn_challenges`
- [x] `sessions`
- [x] `ui_settings`
- [x] `actor_settings`
- [x] `actors`
- [ ] `actor_profiles`
- [ ] `actor_public_keys`
- [x] `notes`
- [x] `polls`
- [x] `poll_votes`
- [x] `reactions`
- [x] `follows`
- [ ] `follow_requests`
- [x] `blocks`
- [x] `emojis`
- [x] `instances`
- [x] `abuse_reports`
- [x] `notifications`
- [x] `api_idempotency_receipts`
- [x] No account-reconciliation checkpoint collection is required by the
      current periodic full-scan implementation.

### Required Indexes

- [x] `accounts`: unique normalized username, WebAuthn ID, bootstrap slot, and
      embedded credential ID indexes
- [x] `webauthn_challenges`: TTL `expiresAt`
- [x] `sessions`: unique opaque session-token digest, account lookup, and TTL
      `expiresAt`
- [x] `actor_settings`: unique `{ accountId, actorId }`
- [x] `actors`: unique `uri`
- [x] `actors`: unique sparse `{ usernameLower, host }`
- [x] `actors`: basic `{ ownerAccountId, isSuspended }` for listing and
      reconciling account-owned local Actors.
- [ ] `actor_public_keys`: unique `keyId`
- [x] `notes`: unique sparse `uri`
- [x] `notes`: basic `{ authorId, createdAt }`
- [x] `notes`: basic `{ renoteId, createdAt }`
- [x] `notes`: active reply/renote aggregation indexes by target ID
- [ ] `notes`: tag and mention indexes suitable for MongoDB
- [x] `follows`: unique `{ followerId, followeeId }`
- [x] `follows`: unique partial `remoteActivityId` for non-empty values
- [x] `follows`: basic `{ followerId, createdAt }` and
      `{ followeeId, createdAt }`
- [ ] `follow_requests`: unique `{ followerId, followeeId }`
- [x] `polls`: `{ authorId, expiresAt }` and sparse `expiresAt`
- [x] `poll_votes`: `{ noteId, choice }` and `{ actorId, createdAt }`
- [x] `blocks`: unique `{ blockerId, blockeeId }`
- [x] `reactions`: unique `{ noteId, actorId }`
- [x] `reactions`: basic `{ noteId, createdAt }` and
      `{ actorId, createdAt }`
- [x] `reactions`: active per-Note reaction aggregation index
- [x] `abuse_reports`: unique sparse `remoteActivityId`
- [x] `abuse_reports`: basic `{ targetUserId, createdAt }` and
      `{ reporterId, createdAt }`
- [x] `notifications`: `{ recipientActorId, createdAt, _id }` and
      `{ recipientAccountId, isRead, createdAt, _id }`
- [x] `notifications`: unique `{ recipientActorId, kind, remoteActivityId }`
- [x] `emojis`: unique `{ host, name }` and sparse `uri`
- [x] `instances`: unique `host`, suspension/update lookup, and sparse metadata
      refresh timestamp
- [x] `api_idempotency_receipts`: unique `{ accountId, requestId }`
- [x] `api_idempotency_receipts`: TTL `expiresAt`

## Go Package Plan

- [ ] `cmd/rosmarinus`: process entrypoint.
- [ ] `internal/config`: config loading and validation.
- [ ] `internal/app`: dependency wiring.
- [ ] `internal/http`: server setup, middleware, route registration.
- [x] `internal/auth`: passkey ceremonies, session lifecycle, CSRF protection,
      and authenticated-account middleware.
- [x] `internal/api`: versioned Salvia REST endpoints, browser-safe
      projections, pagination, and structured errors.
- [x] `internal/realtime`: authenticated SSE streams plus injectable Redis
      Pub/Sub fan-out.
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
- [x] `internal/account`: Rosmarinus-owned account model.
- [x] `internal/connector`: transport-neutral operation/event DTOs retained by
      the API and federation worker; all Ably adapters are removed.
- [x] `internal/queue`: Redis queue interfaces and implementation.
- [ ] `internal/cache`: Redis-backed caches and locks.
- [x] `internal/mfm`: current-Misskey-compatible HTML-to-MFM conversion layer.

## Development Workflow Notes

- [x] HTTP Signature implementation should use `github.com/go-fed/httpsig` for
      signing and verification. Keep compatibility wrappers only for
      ActivityPub draft-era quirks and current Misskey-compatible behavior.
- [x] Make git commits at coherent implementation checkpoints after tests pass,
      instead of leaving unrelated completed changes uncommitted.

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

- [x] Implement AP type helpers for `getApId`, `getApType`, actor/post/activity
      predicates, and array normalization.
- [ ] Reconcile those helpers with current Misskey's nullable type handling,
      `Move`, and URL/href normalization.
- [x] Implement digest verification tests.
- [x] Implement HTTP Signature parse/verify tests with fixture requests.
- [x] Implement signed GET/POST tests with stable signing strings.
- [x] Implement Redis queues, workers, retry/backoff, and timeout handling.
- [x] Implement Redis AP locks and tests for duplicate lock behavior.

### Phase 2: Discovery And Actor Resolution

- [x] Implement WebFinger local responses.
- [x] Implement NodeInfo responses.
- [x] Implement remote WebFinger client.
- [x] Implement actor renderer and `/users/{id}`.
- [x] Implement `/users/{id}/publickey`.
- [x] Implement Misskey-shaped local Actor `outbox`, `followers`,
      `following`, and `featured` AP collections with empty contents until
      social graph and local note ownership are implemented.
- [x] Back local actor `followers` and `following` collections with stored
      `follows` records.
- [x] Implement remote actor create/update baseline.
- [x] Implement signed remote AP GET.
- [x] Add Actor validation tests based on historical Concorde edge cases.
- [x] Add current Misskey Actor validation and resolver edge cases, treating
      any intentionally retained historical behavior as an explicit exception.

### Phase 2A: Legacy Salvia Connector And Multi-Actor Authorization

This completed phase is retained as migration history. Its multi-Actor domain
model remains valid, but its shared-database and Ably boundaries are superseded
by Phase 8.

- [x] Add `ownerAccountId`, explicit system-Actor semantics, ownership-aware
      repository queries, and tests for multiple Actors and cross-account
      denial. Preserve these in the integrated backend.
- [x] Implement the user-facing domain operations, durable notifications, and
      receipt-backed idempotency behind transport adapters. Reuse these through
      the REST API rather than rewriting federation behavior.
- [x] Implement the former account projection and Ably adapters. Treat them as
      migration sources scheduled for removal; do not complete unfinished
      legacy transport work.

### Phase 3: Inbox MVP

- [x] Implement `/inbox` and `/users/{id}/inbox`.
- [x] Validate digest, signature, host, signer, and activity host.
- [x] Enqueue `inbox` jobs.
- [x] Implement basic `Create Note` handler.
- [x] Implement a minimum Note object parser using historical Concorde tests.
- [ ] Reconcile it with current Misskey's ActivityPub Note tests and validation.
- [x] Implement basic note storage.
- [ ] Implement full note resolver.
- [x] Implement initial audience parser.
- [x] Extract AP note mentions, hashtags, and emoji tags.
- [x] Store and render note CW, sensitive, inReplyTo, and quote URLs.
- [x] Store and render basic note attachments.
- [x] Implement HTML to MFM conversion against current Misskey fixtures.
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

- [x] Replace the current basic inbound `Follow` auto-accept behavior with
      pending follow request storage for every local actor.
- [x] Implement basic explicit local approval for pending follow requests, then
      persist the accepted relationship and enqueue `Accept(Follow)`.
- [x] Implement local outgoing `Follow` plus inbound remote `Accept(Follow)` and
      `Reject(Follow)` state transitions so followed servers can begin
      delivering notes to the local Actor inbox.
- [x] Expose follow approval through the legacy Connector command path.
- [x] Implement inbound remote `Undo(Follow)` for local followees.
- [x] Implement `Follow`, `Accept`, and `Reject`, including current Misskey's
      referenced outbound Follow resolution.
- [x] Implement follow request storage without unlocked-account auto-accept.
- [x] Implement basic inbound `Like`, `EmojiReaction`, and undo reaction
      persistence.
- [x] Implement reaction emoji extraction into the shared remote emoji store.
- [x] Derive reaction counts from active indexed reaction records.
- [x] Implement durable reaction notifications and local reaction delivery.
- [x] Implement basic inbound `Announce` and undo announce persistence.
- [x] Implement current-Misskey-compatible Announce visibility checks.
- [x] Derive Announce counts from active indexed renote records.
- [x] Implement durable Announce notifications for local Note authors.
- [x] Implement basic inbound block and unblock.
- [x] Apply stored blocks bidirectionally to follow approval/creation,
      reactions, announces, specified recipients, and follower fan-out; remove
      both follow directions when a Block is accepted.
- [x] Add tests for local/remote Follow, Accept, Reject, Undo, approval, and
      rejection direction combinations.

### Phase 6: Updates, Deletes, Polls, And Media

- [x] Implement authenticated remote Actor update.
- [x] Match current Misskey by implementing `Update(Person)` and
      `Update(Question)`; generic `Update(Note)` remains intentionally
      unsupported.
- [x] Implement inbound remote note delete.
- [x] Implement retryable local note delete delivery.
- [x] Resolve local reply/quote targets before insertion, enforce target
      visibility/shareability, persist canonical IDs, and directly deliver
      Create(Note) to remote target authors even when they are not followers.
- [x] Implement idempotent Note-delete cleanup for reactions, polls, poll
      votes, and related notifications.
- [x] Implement inbound remote actor delete and account cleanup queue enqueue.
- [x] Implement full remote account cleanup worker behavior.
- [x] Implement poll extraction and authenticated poll count updates.
- [x] Implement local/remote vote ingestion and poll update delivery.
- [x] Implement delayed poll-ended jobs and durable local notifications.
- [x] Preserve validated direct media URLs for note attachments, avatars,
      banners, emoji, and instance metadata without backend image processing.
- [x] Keep the Rosmarinus build free of CGO and native image-processing
      dependencies; image decoding, resizing, cropping, transcoding, and
      thumbnail generation are outside the backend boundary.
- [x] Add tests for quote, reply, poll, and sensitive media behavior.

### Phase 7: Federation Hardening

- [x] Apply configured blocked-host checks (including subdomains) at inbox
      authentication, resolution, redirects, Announce targets, direct
      recipients, follower fan-out, and delivery; reject suspended Actors and
      skip delivery to manually/automatically/gone-suspended instances while
      restoring auto-suspended instances after authenticated inbound traffic.
- [x] Implement daily deduplicated instance metadata refresh through NodeInfo,
      persist software/registration/usage metadata, and verify it against the
      real-Misskey fixture.
- [x] Provide queue operations commands to inspect archived tasks as structured
      JSON and promote only `inbox`/`deliver` tasks by ID without starting the
      application server.
- [x] Add a race-tested 32-way duplicate inbox delivery fixture proving the
      atomic Activity receipt permits exactly one federation side effect and
      one completion.
- [ ] Add compatibility fixtures from current Misskey for ActivityPub
      render/parse, then retain useful Concorde fixtures as historical
      regressions.
- [x] Document operational Redis and MongoDB configuration, durability,
      startup index ownership, receipt retention, queue inspection, and safe
      task promotion.

### Phase 8: Integrated Backend And React SPA Boundary

- [x] Add Rosmarinus-owned account, WebAuthn ceremony, and session collections
      with unique/TTL indexes. Fresh initial setup now creates its account
      directly in Rosmarinus; migration of legacy account rows remains open.
- [ ] Move account ownership into Rosmarinus-owned MongoDB collections and
      migrate existing account IDs without changing Actor `ownerAccountId`
      references.
- [x] Implement passkey-only initial bootstrap, discoverable login, logout,
      credential counter updates, and revocable cookie sessions in Go. Further
      authenticated passkey credential management remains a later endpoint.
- [x] Add the initial versioned authenticated REST endpoints for owned Actor
      listing and the existing Actor/post/follow/reaction/block/poll/
      notification mutations, with CSRF, idempotency, ownership checks, and
      browser-safe Actor projections.
- [x] Add versioned authenticated REST endpoints for queries and all
      existing user-facing domain mutations, with ownership checks and focused
      authorization tests.
- [x] Add authenticated SSE and Redis Pub/Sub fan-out;
      verify account isolation, reconnect reconciliation, and multi-instance
      delivery without treating Pub/Sub as durable storage.
- [x] Remove Ably and the Connector transport after endpoint parity is tested.
      The obsolete service-specific MongoDB role and configuration are removed;
      any legacy Salvia data requires an offline migration before cutover.
- [ ] Build `./salvia` as a React SPA, carrying forward the passkey-only,
      multi-Actor, simplified Misskey-inspired product design from
      `./salvia/salvia-old` without carrying forward Next.js or server code.
      Generate upload previews, thumbnails, and other required image
      derivatives in the browser with Canvas APIs.
- [ ] Define a same-origin production deployment for the SPA, REST API, SSE,
      and ActivityPub routes, including SPA history fallback that
      cannot shadow protocol or API endpoints.
- [ ] Add integration tests for initial setup, passkey login, session expiry,
      cross-account Actor denial, multi-Actor switching, mutation idempotency,
      event isolation, and state recovery after a missed Pub/Sub message.
- [x] Update operations, container, environment-variable, and CI documentation;
      ensure no Ably configuration or frontend secret remains.

## Federation Compatibility Test Adoption Status

Current Misskey unit tests and `packages/backend/test-federation` are the
primary fixture source for all new work. The following historical tests from
`./concorde/packages/backend/test` were already reviewed and remain useful as
supplemental regressions. Frontend, Misskey API, streaming, drive, and timeline
tests are out of scope unless they expose ActivityPub persistence semantics
Rosmarinus must write to MongoDB.

### Ported

- [x] `ap-request.ts`
  - [x] `createSignedPost with verify`
  - [x] `createSignedGet with verify`
  - [x] strict request/final/object-ID origin and canonical URL validation,
        including `www.` normalization, redirect downgrade rejection, and
        Mastodon-compatible fragment removal for fetched activity IDs.
- [x] `activitypub.ts`
  - [x] `Parse minimum object / Minimum Actor`
  - [x] `Parse minimum object / Minimum Note` as AP note parser and basic
        DB-backed note creation behavior.
  - [x] `Truncate long name / Actor`
  - [x] `Normalize empty name / Actor`
  - [x] scalar and array `alsoKnownAs`, including Update(Person) persistence.
  - [x] same-origin embedded featured Notes without redundant fetches, while
        cross-origin featured items are fetched canonically to reject embedded
        content substitution.
  - [x] `Images / Render image document with dimensions` and `Link is not an
        attachment files`, adapted to Rosmarinus's validated direct-HTTPS media
        model and verified through the real-Misskey Note fixture.
- [x] `fetch-resource.ts`
  - [x] AP `GET /@:username` and `GET /users/:id` return
        `application/activity+json` when AP is requested.
  - [x] AP `GET /users/:id/outbox`, `followers`, `following`, and
        `collections/featured` return Misskey-shaped `OrderedCollection`
        / `OrderedCollectionPage` responses.
  - [x] AP `GET /notes/:id` returns `application/activity+json` for stored
        notes.
  - [x] AP `GET /notes/:id/activity` returns the corresponding public
        `Create` activity.
  - [x] inbox `Accepted`.
  - [x] inbox `Invalid Host`.
  - [x] inbox `Payload Too Large`.
  - [x] inbox `Signature Header Required`.
  - [x] inbox `Digest Header Required`.
  - [x] inbox `Invalid Digest Header`.
  - [x] inbox `Unsupported Digest Algorithm`.
  - [x] inbox `Digest Mismatch`.

### Not Ported Yet

- [ ] Reassess the historical `activitypub.ts / Minimum Note` side effects
      against current Misskey before porting any missing behavior.
- [x] `fetch-resource.ts / /notes/:id` local-vs-remote
      redirect and public/home visibility behavior.
- [ ] `fetch-resource.ts / HTML`, root/docs/assets, RSS/ATOM/JSON feeds:
      out of scope for Rosmarinus unless a future federation requirement needs
      them.
- [ ] API visibility, note API, block/mute/thread-mute/streaming tests:
      out of scope for Rosmarinus API/frontend, but should be mined later for
      MongoDB state side effects that ActivityPub handlers must preserve.

## Real Federation Test Plan

Use this once actor resolution, signed GET, Create Note ingestion, and delivery
workers are implemented enough to exchange basic activities.

Implementation compatibility must be checked against the current `./misskey`
backend, unit tests, and official Docker federation suite. `./concorde` may add
historical regression coverage but does not define new behavior.

Treat this suite as incremental acceptance coverage, not a one-time smoke test.
Each completed federation capability should add the smallest stable Misskey
scenario that proves it when Misskey exposes a suitable public API. If a
capability cannot yet be exercised end to end, record that gap here and add
focused unit/integration coverage until the fixture can cover it.

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
- [x] Add Rosmarinus-specific Misskey federation tests that drive follow,
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
- [ ] Run a separate current Misskey test instance with its own PostgreSQL and
      Redis using `./misskey/packages/backend/test-federation`.
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

- [ ] Inspect `./misskey/packages/backend/test-federation` for both its Docker
      topology and its expected federation behavior; port the smallest relevant
      scenario at each checkpoint.
- [x] Add `docker/federation/misskey-rosmarinus.compose.yml` to attach
      Rosmarinus, MongoDB, Redis, and `nginx` TLS endpoint `rosmarinus.test` to
      Misskey's federation network.
- [x] Add `docker/federation/nginx/rosmarinus.test.conf` to proxy
      `https://rosmarinus.test` to the Rosmarinus HTTP server.
- [ ] Prepare a disposable current Misskey config with:
      `url: http(s)://misskey.example.test`,
      Postgres pointing to the test DB,
      Redis pointing to the test Redis,
      and federation enabled.
- [ ] Use the available JS package manager (`yarn`, `pnpm`, or `npm`) only in
      the Misskey checkout or a disposable test checkout. Do not modify
      `./misskey` or `./concorde` source files.
- [ ] Start Misskey web and queue worker locally, then create a local Misskey
      test account through its setup flow or seed script.
- [ ] Start Rosmarinus with `RUN_HTTP=true` and `RUN_WORKERS=true` so inbox and
      deliver queues run inside the same binary.
- [ ] Use Misskey search/follow UI or API to follow
      `@relay@rosmarinus.example.test`.
- [ ] Confirm Rosmarinus enqueues and processes:
      `inbox Follow -> signature verify -> pending follow request`.
- [ ] Trigger the Rosmarinus local approval path for the pending follow request.
- [ ] Confirm Rosmarinus enqueues and processes `deliver Accept` only after
      approval.
- [ ] Confirm Misskey marks the follow as accepted after approval.

### Rosmarinus Visibility Checks From Misskey

- [ ] From Misskey, search `@relay@rosmarinus.example.test`.
- [ ] Confirm Misskey performs WebFinger and receives a `self` link.
- [ ] Confirm Misskey fetches `GET /users/{id}` and accepts the actor object.
- [ ] Confirm Misskey fetches `GET /users/{id}/publickey` or reads `publicKey`
      from the actor object.
- [ ] Confirm Rosmarinus access logs show the expected WebFinger and actor fetch
      sequence.

### Inbound Federation Checks

- [x] From Misskey, follow the Rosmarinus actor.
- [x] Confirm Rosmarinus receives `Follow` at `/inbox` or `/users/{id}/inbox`.
- [x] Confirm digest, HTTP Signature parsing, host validation, and queue enqueue
      succeed.
- [x] After signer resolution is implemented, confirm signature verification
      succeeds against the Misskey actor public key.
- [x] Post a public Misskey note from an actor followed by Rosmarinus.
- [x] Confirm Rosmarinus receives `Create` and stores the remote actor/note.

### Outbound Federation Checks

- [x] Approve a pending Misskey follow request in Rosmarinus.
- [x] Confirm Rosmarinus sends `Accept(Follow)` back to Misskey only after
      approval and Misskey marks the follow request accepted.
- [x] Have Rosmarinus send a public `Create(Note)` to an accepted Misskey
      follower through the delivery queue.
- [x] Confirm through Misskey's public API that Misskey stores the delivered
      remote note.
- [x] Have Rosmarinus send `Like` and `Announce` for a Misskey note.
- [x] Confirm Misskey accepts both activities and their Undo forms without a
      retry loop remaining in Asynq.
- [x] Delete a local Note and confirm `Delete(Tombstone)` reaches both ordinary
      followers and a non-following current-Misskey Actor that replied to the
      Note, matching Misskey's concerned-user delivery behavior.

### Operational Checks

- [ ] Stop Rosmarinus during queued delivery, restart it, and confirm Asynq
      resumes pending jobs from Redis.
- [ ] Force a temporary Misskey outage and confirm AP backoff behavior matches
      current Misskey's delayed retry profile after accounting for queue-library
      attempt numbering.
- [ ] Inspect failed jobs and logs for enough information to identify target
      inbox, actor, activity ID, and failure reason.
- [ ] Keep packet captures or structured logs for WebFinger, signed GET, inbox,
      and delivery requests as compatibility fixtures.

## Open Decisions

- [x] Choose the Redis queue implementation: use Asynq by default, hidden behind
      `internal/queue` interfaces.
- [x] Decide browser integration: Rosmarinus owns the authenticated REST API and
      SSE stream; Salvia is a React SPA with no backend or direct
      database access.
- [x] Decide realtime transport: use Redis Pub/Sub only for local best-effort
      fan-out between Rosmarinus processes. Browsers receive authenticated
      account-scoped events from Rosmarinus and recover through HTTP reads.
- [x] Decide data ownership: Rosmarinus owns all account, authentication, UI,
      Actor, and federation collections and their indexes. Retire the former
      cross-service MongoDB roles after migration.
- [x] Decide authentication: WebAuthn/passkeys are the only login method;
      Rosmarinus owns credential verification and server-side sessions.
- [x] Decide media ownership: Rosmarinus stores validated direct remote URLs;
      Salvia owns browser presentation and any separately secured image proxy.
- [ ] Decide how much of current Misskey's antenna/word-mute/timeline side effects
      belong in the integrated Rosmarinus product.
- [x] Decide local Actor provisioning roles: keep the environment-provisioned
      Actor as a stable system Actor for service-level federation work. Create
      user-managed Actors through authenticated REST API calls, store them
      in MongoDB with `ownerAccountId`, and allow one Salvia account to own
      multiple Actors.
- [ ] Decide whether object IDs follow current Misskey's externally observable
      ID properties or use MongoDB ObjectIDs/ULIDs behind stable AP URIs.
