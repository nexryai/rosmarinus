# Salvia React SPA Implementation Plan

## Goal

Build `./salvia` as a static React SPA for the integrated Rosmarinus backend.
Rosmarinus owns authentication, sessions, accounts, Actor management,
REST APIs, MongoDB, Redis, and ActivityPub. Salvia owns browser
presentation and interaction only.

```text
Browser: Salvia React SPA
  | same-origin HTTPS
  |-- passkey and session endpoints
  |-- versioned REST API
  `-- authenticated SSE
             |
             v
       Rosmarinus backend
          |         |
       MongoDB    Redis queues/locks/PubSub
```

Redis Pub/Sub is local backend infrastructure. The browser never connects to
Redis, and Pub/Sub is not a durable source of truth. Ably and Next.js are not
part of the target architecture.

## Fixed Product And Architecture Decisions

- Completed: normalize legacy Actor profile field keys at the API boundary
  and verify home timeline rendering with nonempty remote profile fields.
  The backend now emits lowercase `name`/`value` keys consistently.

- Completed: resolve remote Actor handles and URLs through an authenticated,
  owned-Actor-scoped Rosmarinus endpoint, then open the safe Salvia profile so
  users can create or remove an outgoing ActivityPub follow.

- Completed: generate new local and remote Actor IDs as collision-checked
  MongoDB ObjectID hexadecimal strings while keeping IDs opaque to Salvia and
  using Actor `host` state, rather than an ID prefix, to determine locality.

- Use React, TypeScript, Vite, and `pnpm`; produce static assets suitable for
  same-origin deployment with Rosmarinus.
- Do not add Next.js, server rendering, frontend API routes, Ably, or direct
  MongoDB/Redis clients.
- Use passkeys exclusively. Do not implement passwords, password resets,
  recovery passwords, or TOTP.
- Keep accounts and Actors distinct. One account can manage multiple Actors;
  the selected Actor is UI state, not authorization proof.
- Preserve old Salvia's simplified Misskey-inspired design, yellow default
  theme, theme selection, custom emoji reactions, and Tabler Icons.
- Keep widgets, Deck, and public Misskey API compatibility out of scope.
- Treat SSE messages as invalidation hints and reconcile through the REST API
  after reconnects or ambiguous mutations.
- Load all secrets and server policy only in Rosmarinus. Browser-visible build
  configuration may contain public same-origin paths but no credentials.
- Generate upload previews, thumbnails, crops, and other required image
  derivatives with browser Canvas APIs. Rosmarinus stores or relays validated
  originals and metadata without server-side image processing.

## Phase 0: Establish The SPA Baseline

- [x] Inventory the Vite scaffold in `./salvia`, its scripts, linting,
      formatting, testing, and browser-support policy.
- [x] Inspect `./salvia/salvia-old` for product flows and visual choices only.
      Do not copy Next.js routes, server modules, Ably code, or secrets.
- [x] Confirm `pnpm format`, `pnpm lint`, `pnpm test`, and `pnpm build`, adding
      focused tooling only where missing.
- [x] Define typed public configuration for the same-origin REST and SSE
      paths, preferably using relative defaults.
- [x] Add routing with a documented production history fallback that cannot shadow API or
      ActivityPub routes.

Exit criteria: the static SPA builds reproducibly and contains no server or
secret-bearing dependency.

## Phase 1: Build The Design System And Shell

- [x] Define semantic design tokens with a yellow-based default palette and
      additional supported themes.
- [x] Keep all styling in TSX with CSS-in-JSX and no CSS framework dependency;
      retain Tabler Icons and the product-reference button ripple.
- [ ] Build accessible primitives in `src/components/ui` and reusable domain
      components in `src/components`.
- [x] Build responsive navigation, timeline layout, forms, menus, dialogs,
      avatars, loading states, empty states, and error boundaries.
- [x] Keep account identity and active Actor visibly separate in the shell.
- [x] Persist theme and non-authoritative display preferences through the
      Rosmarinus settings API; use local storage only as a non-sensitive render
      optimization.
- [ ] Add component, keyboard, contrast, and responsive behavior tests.

Exit criteria: the SPA has a reusable, accessible, simplified Misskey-inspired
shell without Next.js assumptions.

## Phase 2: Integrate Passkey-Only Authentication

- [x] Add an unauthenticated bootstrap-status call. Render first-administrator
      setup only when Rosmarinus reports an empty installation.
- [x] Implement the SPA WebAuthn registration and login UI using the now
      available Rosmarinus challenge and verification endpoints.
- [x] Do not complete setup or show an authenticated shell until the server has
      accepted the passkey ceremony and established its HTTP-only session.
- [ ] Implement login, logout, session refresh, expired-session handling, and
      account suspension/deletion states.
- [x] Send CSRF proof on cookie-authenticated mutations according to the API
      contract. Do not store session bearer tokens or credential material in
      local storage.
- [ ] Test unsupported browsers, canceled ceremonies, expired/replayed
      challenges, closed registration, login, logout, and session expiry.

Exit criteria: a fresh deployment creates exactly one initial administrator,
and subsequent anonymous users see passkey login only.

## Phase 3: Implement Multi-Actor Workflows

- [x] Load owned Actors from the implemented session-scoped API and handle
      zero, one, and multiple Actor states.
- [x] Build Actor creation, selection, profile editing, and deletion flows.
- [x] Include the selected Actor ID in Actor-scoped API paths or request bodies;
      never send an account ID as authorization evidence.
- [x] Recover cleanly when the selected Actor is deleted, suspended, moved, or
      no longer owned by the current account.
- [x] Store display order, color, pinning, and last-selected Actor through the
      settings API without duplicating federation-authoritative Actor fields.
- [ ] Test one account managing multiple Actors and cross-account access being
      rejected even when a foreign Actor ID is supplied manually.

Exit criteria: account and Actor identities remain distinct throughout the UI,
and every Actor mutation is authorized again by Rosmarinus.

## Phase 4: Add REST Client And SSE Reconciliation

Backend checkpoint: Rosmarinus now exposes the versioned passkey/session,
Actor, timeline, Note, social mutation, notification, profile, emoji, instance,
settings, and `GET /api/v1/events` contracts documented in
`docs/salvia-integration.md`. The unchecked work below is SPA implementation.

- [x] Centralize versioned REST calls, runtime response validation, structured
      error handling, cancellation, and session-loss handling.
- [x] Generate stable idempotency keys for retryable mutations. Reuse a key for
      the same logical intent; do not invent a new one after an ambiguous
      timeout until canonical state has been checked.
- [x] Connect to authenticated Rosmarinus SSE and close it on
      logout or account change.
- [x] Scope caches and query keys by account and Actor. Invalidate only affected
      projections when an event names an Actor.
- [x] Handle duplicates, reconnects, missed events, and multiple tabs by
      re-reading canonical state while preserving scroll and draft state where
      safe.
- [ ] Test SSE isolation, reconnect backoff, duplicate invalidations, missed
      message recovery, and REST/SSE ordering races.

Exit criteria: realtime improves freshness but correctness never depends on a
Pub/Sub message arriving.

## Phase 5: Build Core Social Features

- [x] Build home/public timelines with stable pagination and deterministic
      deduplication.
- [x] Render notes, replies, quotes, renotes, content warnings, visibility,
      polls, attachments, mentions, and custom emoji reactions from sanitized
      API projections.
- [x] Implement compose, delete, reaction, poll vote, follow/unfollow,
      block/unblock, and mandatory follow approve/reject flows.
- [x] Build account- and Actor-scoped notification views and mark-read actions.
- [x] Build local and remote Actor profiles, follower/following views, moved and
      suspended states, and safe external-link/media behavior.
- [x] Add remote-user lookup by handle or Actor URL and route successful
      resolution into the profile follow/unfollow flow.
- [x] Generate image upload previews and thumbnails with Canvas, test
      orientation and size handling, and keep the original file available when
      the upload contract requires it.
- [x] Add focused accessibility and interaction tests for every mutation and
      its loading, retry, empty, and error states.

Exit criteria: core federation workflows are usable without exposing MongoDB,
Redis, or ActivityPub implementation details to the browser.

## Phase 6: Operations And End-To-End Verification

- [x] Embed the production SPA in the Rosmarinus binary and serve immutable hashed SPA
      assets, `index.html`, and history fallback with correct cache policy.
- [x] Add Content Security Policy, frame, referrer, MIME-sniffing, and other
      browser security headers at the serving boundary.
- [ ] Verify passkey RP ID and allowed origins for the production same-origin
      topology.
- [ ] Remove all Ably and Next.js dependencies, variables, docs, and deployment
      resources outside explicitly preserved migration history.
- [ ] Add end-to-end tests covering bootstrap, passkey login, multiple Actors,
      cross-account denial, social mutations, realtime refresh, reconnect, and
      logout.
- [x] Run the production SPA build and Rosmarinus unit suites
      in CI.

Exit criteria: the integrated deployment needs only Rosmarinus, MongoDB, Redis,
and static Salvia assets, and the full user journey passes without Ably or a
Next.js server.

## Checkpoint Definition Of Done

- [ ] Formatting, linting, focused tests, and the production SPA build pass.
- [ ] No secret, backend credential, or unsafe raw federation data reaches the
      browser bundle.
- [ ] Account and Actor authorization boundaries have negative tests.
- [ ] New realtime behavior remains correct when events are duplicated or
      missed.
- [ ] REST, session, Actor ownership, or SSE-contract changes are reflected in
      `docs/salvia-integration.md`, this plan, and `docs/salvia/AGENTS.md`.
