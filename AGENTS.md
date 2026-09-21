# Rosmarinus Backend and Salvia SPA

## Engineering Rules

- Follow Go best practices.
- Use dependency injection for services, repositories, queues, clients, and loggers.
- Prefer Go's standard `log` package and log meaningful runtime events.
- Write focused tests whenever practical.
- Avoid comments that narrate self-evident code line by line. Add concise
  comments where the rationale is not apparent from the code, especially at
  security boundaries, across data-ownership boundaries, and for intentional
  compatibility exceptions.
- Keep source files using LF line endings.
- Load all runtime configuration from environment variables.
- Keep Rosmarinus buildable with `CGO_ENABLED=0`. Do not add CGO-only
  dependencies or require system image-processing libraries.

## Project Scope

- Rosmarinus is the successor project to Concorde, a Misskey fork.
- Rosmarinus is the integrated backend for ActivityPub federation, local
  accounts, passkey authentication, sessions, local Actor management, and the
  authenticated REST API. It does not expose a public Misskey-compatible
  API.
- Salvia is a React single-page application in `./salvia`. It is built as
  static assets, embedded into the Rosmarinus executable, and served by the
  Rosmarinus HTTP server. It contains no Next.js or other server-side backend.
- Do not introduce Ably. Browser commands and queries use the Rosmarinus REST
  API; live server-to-browser updates use authenticated Server-Sent Events
  (SSE).
  Redis Pub/Sub is an internal, local deployment transport for fan-out between
  Rosmarinus processes and is never exposed directly to browsers.
- Use MongoDB as the database and design collections/indexes with MongoDB best practices.
- Keep one account distinct from its Actors. An authenticated account may own
  multiple local Actors, and every Actor-scoped operation must verify ownership
  server-side.
- Authenticate local accounts exclusively with passkeys (WebAuthn). Do not add
  passwords, password reset, or TOTP authentication.
- Do not decode, resize, crop, transcode, optimize, or generate thumbnails for
  images in Rosmarinus. Preserve validated original media and metadata without
  backend image processing.

## Salvia Frontend

- Treat `./salvia/salvia-old` as the product and design reference for passkey
  authentication, multiple-Actor workflows, simplified Misskey-inspired UI,
  the yellow default theme, custom emoji reactions, and Tabler Icons.
- Do not edit `./salvia/salvia-old`; implement the current frontend in
  `./salvia` as a React SPA.
- Keep Salvia styling in TSX through CSS-in-JSX, with no CSS files or CSS
  framework dependency. Keep reusable theme tokens aligned with the
  yellow-first product direction.
- Keep authentication secrets, WebAuthn verification, sessions, authorization,
  MongoDB access, Redis access, and ActivityPub key material in Rosmarinus.
- The SPA must not connect directly to MongoDB or Redis and must not treat its
  selected Actor as authorization evidence.
- Generate upload previews, thumbnails, and other required image derivatives
  in the browser with Canvas APIs. Do not depend on Rosmarinus to transform
  uploaded images.

## Federation Compatibility

- Treat the current `./misskey` checkout as the primary behavioral and
  implementation reference for ActivityPub federation.
- Do not edit `./misskey` or `./concorde`.
- For ActivityPub parsing, HTTP signatures, MFM handling, custom emoji
  handling, delivery, retry behavior, and federation edge cases, inspect the
  current Misskey backend and its unit/federation tests before implementing or
  changing Rosmarinus behavior.
- Treat `./concorde` as a secondary historical reference. Use it to understand
  the predecessor's behavior and to identify compatibility regressions, but do
  not let it override current Misskey behavior.
- When Rosmarinus intentionally differs from current Misskey because of its
  focused backend scope, MongoDB model, mandatory follow approval policy, Go
  implementation, or real-world interoperability requirements, document and
  test the exception explicitly.
- Similarity between Concorde and current Misskey must be established per
  behavior, not assumed. Their shared merge base is historical context only.

### Current Misskey Reference Code Map

Follow these current Misskey files and their dependencies when a checkpoint
needs federation detail:

- `core/activitypub/ApInboxService.ts` for activity dispatch and per-activity
  behavior.
- `server/ActivityPubServerService.ts`,
  `queue/processors/InboxProcessorService.ts`, and
  `core/activitypub/ApRequestService.ts` for inbox validation and HTTP
  signatures.
- `core/activitypub/ApResolverService.ts` and `misc/check-against-url.ts` for
  resolution, recursion, redirects, and URL trust boundaries.
- `core/activitypub/models/ApPersonService.ts`, `ApNoteService.ts`, and
  `ApQuestionService.ts` for Actor, Note, emoji, and poll ingestion.
- `core/activitypub/ApRendererService.ts`,
  `core/activitypub/ApDeliverManagerService.ts`, and
  `queue/QueueProcessorService.ts` for rendering, delivery, and retry.
- `packages/backend/test/unit` and `packages/backend/test-federation/test` for
  executable behavior examples.

### Intentional Rosmarinus Differences

- Rosmarinus requires explicit local approval for every inbound follow and must
  never reintroduce a per-user auto-acceptance policy.
- Rosmarinus uses MongoDB and exposes ActivityPub plus a purpose-built,
  authenticated REST API for Salvia. Misskey's PostgreSQL entities, public API
  surface, and unrelated side effects are out of scope.
- Rosmarinus verifies federation with HTTP Signatures only. Do not implement a
  JSON-LD `RsaSignature2017` fallback or relay flows that require Linked Data
  signatures.
- Rosmarinus keeps completed inbound Activity IDs in MongoDB for seven days by
  default so peer retries and queue replays cannot repeat federation side
  effects.
- Salvia is a static, same-origin client of the REST API and SSE stream and
  never accesses MongoDB or Redis directly.

## Runtime Architecture

- Run the HTTP server and the queue workers in one `cmd/rosmarinus` process by
  default. `RUN_HTTP`, `RUN_WORKERS`, and `WORKER_QUEUES` allow split
  deployments but are not the default architecture.
- Use Redis for queues, delayed retries, rate limits, distributed AP locks, and
  local Pub/Sub fan-out. Wrap Asynq behind `internal/queue` interfaces so
  ActivityPub services never depend on Asynq directly.
- Queue names are `inbox`, `deliver`, `system`, `poll-ended`, `metadata`, and
  `account-delete`. Processing is at-least-once; handler-level unique indexes
  are the final guard against duplicates.
- Match current Misskey's queue limits: `deliver` and `inbox` default to
  128/sec and 32/sec with concurrency 128 and 16; 11 `deliver` and 7 `inbox`
  Asynq retries (12 and 8 total attempts); `(2^attempts - 1) * 1m` backoff
  capped at 8 hours with up to 20% jitter; 1-minute `deliver` and 5-minute
  `inbox` timeouts.
- Keep the inbound Activity processing lease short-lived and completed receipts
  for seven days by default.
- Rosmarinus is the only backend and owns every runtime MongoDB collection.
  Do not add `salvia_*` split-ownership collections; migrate legacy data
  offline before deploying a new ownership model.
- Use lowercase MongoDB ObjectID strings for stored entity IDs while keeping
  public ActivityPub URIs stable and independent of the internal ID.

## HTTP Signatures

- ActivityPub still commonly uses draft-era HTTP Signatures, so compatibility matters more than strict modern spec interpretation.
- Use `github.com/go-fed/httpsig` for HTTP Signature signing and verification.
- Match current Misskey's signed header sets and validation behavior where
  practical, while preserving explicitly tested compatibility with real-world
  `@peertube/http-signature` peers.

## External URL and Media Security

- Route every URL received from an external source through
  `internal/security` (`IsAllowedURL` / `ValidateURL`) before it is fetched,
  persisted, or rendered. Treat federated actor IDs, note IDs, `inReplyTo`,
  quote and mention targets, attachment, emoji and media URLs, WebFinger links,
  HTTP Signature `keyId` values, instance metadata links, and client-supplied
  remote-profile targets as untrusted.
- By default only absolute `https` URLs on ports 80 or 443 are accepted.
  Reject credentials, IPv6 literal hosts, loopback, private, link-local,
  multicast, unspecified and documented/bogon addresses, single-label and
  `localhost`/`.local`/`.internal` hosts, and ambiguous numeric host forms.
- The only exception is remote-authored note bodies, where plain `http` links
  are preserved because draft-era federation still emits them. Pass
  `allowUnsafeConnections=true` only for that note-body path; never for
  metadata, media, actor, or delivery URLs.
- Use `security` as the parsing and ingestion gate. Outbound requests must
  still go through the safe HTTP clients in `internal/media` that validate the
  resolved address, so DNS rebinding and allow-listed private federation
  networks remain controlled at the network boundary.
- When rendering stored or federated links, re-check the scheme and target with
  `security` and escape with `mfm.EscapeHTML`; do not emit an anchor for a URL
  that fails the policy.

## Federation Tests

- Keep `test/federation/misskey_test.go` organized into clearly labeled phase
  comments. Each phase comment must state the federation behavior being
  exercised and the outcome it verifies.
- For every implementation checkpoint, assess whether the behavior can be
  verified through the existing real-Misskey fixture. When it can, update
  `test/federation/misskey_test.go` and its workflow documentation in the same
  checkpoint.
- Treat the real-Misskey suite as incremental acceptance coverage, not a
  one-time smoke test. Add the smallest stable Misskey scenario that proves
  each new capability, and record the gap when a capability cannot yet be
  exercised through Misskey's public API.
- Current Misskey unit tests and `packages/backend/test-federation` are the
  primary fixture source. Keep Concorde fixtures only as supplemental
  historical regressions.
- Do not use `localhost` in ActivityPub IDs during federation tests; many
  implementations reject or mishandle it. Prefer HTTPS and non-loopback
  hostnames.

## Implementation Checkpoints

Before a checkpoint is complete:

- Review the current Misskey source and its relevant unit/federation tests for
  the changed federation behavior, and record intentional deviations in tests
  or handoff notes.
- Add focused unit/integration tests for the changed behavior.
- If the behavior is observable through the real-Misskey fixture, update
  `test/federation/misskey_test.go` with a clearly commented phase and keep the
  federation workflow documentation accurate.
- If the Salvia integration contract changes, update the applicable handoff
  documents; otherwise confirm the change is internal-only.
- Ensure formatting, tests, and relevant static checks pass before creating a
  signed commit.

## Open Work

- Reconcile all completed inbox, resolver, renderer, and delivery behavior
  against the pinned current Misskey commit and add focused regression tests for
  every material difference.
- Reconcile the ActivityPub type helpers with current Misskey's nullable type
  handling, `Move`, and URL/href normalization.
- Reconcile the Note parser with current Misskey's ActivityPub Note tests, and
  add golden tests for incoming Mastodon- and Misskey-style notes.
- Keep mining current Misskey's `test/unit/activitypub.ts`,
  `test/unit/ap-request.ts`, and `test-federation/test` as the primary
  compatibility fixtures, and add current-Misskey AP render/parse fixtures.
- Add integration tests for initial setup, passkey login, session expiry,
  cross-account Actor denial, multi-Actor switching, mutation idempotency,
  event isolation, and state recovery after a missed Pub/Sub message.

## Salvia Integration Documentation

- For every implementation checkpoint, assess whether it changes the
  Rosmarinus REST API or SSE contract, passkey/session behavior,
  account/Actor ownership, authorization behavior, or federation state
  consumed by Salvia.
- When it does, update the applicable handoff documents in the same checkpoint:
  `docs/salvia-integration.md`, `docs/salvia/AGENTS.md`, and/or
  `docs/salvia/PLANS.md`.
- Do not change Salvia documents for internal-only implementation details that
  leave the integration contract unchanged.

## Git Workflow

- Make git commits at coherent implementation checkpoints after tests pass.
- Do not mix unrelated or unfinished work into the same commit.
- Always create signed git commits.
- If signing fails, do not create an unsigned commit. Stop and notify the user
  that the commit could not be signed.
- After every signed commit, push it to the configured upstream branch and wait
  for the associated CI workflow to complete.
- If CI fails, inspect the workflow logs, fix the failure, and repeat the
  commit, push, and CI verification cycle until CI passes.
