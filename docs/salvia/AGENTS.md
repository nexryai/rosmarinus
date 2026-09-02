# Salvia React SPA

These instructions apply to the current frontend in `./salvia`. The preserved
`./salvia/salvia-old` checkout is a product and design reference only. Do not
edit it or carry its Next.js and Ably architecture into the current app.

## Engineering Rules

- Build Salvia as a React and TypeScript single-page application. Do not add
  Next.js, server components, server routes, API keys, or a frontend backend.
- Use `pnpm` for dependencies and commit the lockfile with dependency changes.
- Keep runtime configuration environment-backed at build or deployment time.
  No value shipped to the browser may be a secret.
- Write focused tests for components, client API behavior, accessibility, and
  security-sensitive browser flows whenever practical.
- Keep LF line endings and avoid comments that narrate self-evident code.

## Product Direction

Carry these decisions forward from old Salvia:

- Passkeys are the only authentication method. Do not add passwords, password
  reset, recovery passwords, or TOTP.
- One authenticated account may create, own, select, and manage multiple local
  ActivityPub Actors. Account identity and the active Actor must remain visibly
  distinct.
- Use a substantially simplified, Misskey-inspired experience. Widgets, Deck,
  and unrelated Misskey compatibility are out of scope.
- Use a yellow-based default theme and allow supported theme selection.
- Render Misskey-style custom emoji reactions rather than reducing reactions to
  a generic like or favorite.
- Use Tabler Icons when an appropriate icon exists.
- Put reusable UI primitives in `src/components/ui`, reusable domain
  components in `src/components`, and name component files in PascalCase.

## Backend Boundary

Rosmarinus is the only backend. It owns:

- local accounts, passkey credentials and WebAuthn challenges;
- application sessions and authorization;
- local Actor lifecycle, ownership, and key material;
- all MongoDB reads, writes, indexes, and migrations;
- ActivityPub processing, delivery, queues, and federation state; and
- the application HTTP API and authenticated browser event stream.

The SPA must never connect directly to MongoDB, Redis, or ActivityPub peers.
It must not contain database credentials, Redis credentials, private Actor
keys, WebAuthn server verification, or authorization policy.

Use same-origin HTTPS for passkey ceremonies, session APIs, JSON APIs, and the
event stream. The production deployment may serve built SPA assets from
Rosmarinus or route them through the same origin. Client-side history fallback
must not intercept `/api`, ActivityPub, WebFinger, NodeInfo, inbox, Actor, Note,
or media routes.

## Authentication And Sessions

- The SPA initiates passkey registration or authentication, but Rosmarinus
  creates challenges and verifies every WebAuthn response.
- Show one-time administrator setup only when the backend reports an empty
  installation. Server responses, not a hidden client route, determine setup
  eligibility.
- Once an account exists, show login only unless Rosmarinus later exposes an
  authenticated administrator invitation flow.
- Use secure HTTP-only session cookies. Browser code must not persist a bearer
  token or passkey secret in local storage.
- Send the backend-issued CSRF proof on cookie-authenticated mutations as
  required by the API contract.
- Treat `401` as an expired or absent session and `403` as an authenticated but
  unauthorized operation. Clear account-scoped client state on logout or
  session loss.

## Account And Actor Identity

An account is the authenticated administrative identity. An Actor is a local
federated identity owned by that account.

```text
Account
  -> Actor A
  -> Actor B
  -> Actor C
```

- Load the Actor list from Rosmarinus for the current session; never accept or
  construct an arbitrary account ID in the SPA.
- Treat selected Actor state as a UI preference only. Include the Actor ID in
  Actor-scoped requests, and rely on Rosmarinus to verify ownership in the same
  query that loads the Actor.
- Support zero, one, and multiple Actor states. Make creation, switching,
  profile editing, suspension/deletion status, and command context clear.
- Do not infer authorization from data already rendered in the browser.

## API And Realtime Rules

- Use the versioned Rosmarinus application API for all reads and mutations.
  Do not write federation or UI state directly to MongoDB.
- Validate API responses at the client boundary and render structured error
  codes without exposing internal backend details.
- Supply an idempotency key for retryable mutations when the endpoint requires
  it. A network timeout is ambiguous; reconcile canonical state before creating
  a new logical operation.
- Connect only to the authenticated Rosmarinus event stream. Events are scoped
  to the current account and may name an Actor for efficient invalidation.
- Treat realtime events as refresh hints. They may be duplicated or missed;
  reconnect by re-reading the relevant API views while preserving useful UI
  state such as timeline position.
- Redis Pub/Sub is internal to Rosmarinus. Do not install a Redis client in the
  SPA or encode Redis channel names into browser code.

## Rendering Untrusted Federation Data

- Treat remote text, HTML-derived content, profile fields, URLs, attachment
  metadata, emoji, avatars, banners, and instance metadata as untrusted.
- Use the sanitized projections returned by Rosmarinus. Do not add raw HTML
  rendering paths or infer visibility from mentions.
- Preserve Actor/account and Note visibility boundaries in caches and query
  keys. Clear private projections when the session or active Actor changes.
- Remote media URLs remain untrusted even after backend validation. Apply the
  documented browser presentation policy and never forward session secrets to
  remote origins.

## Verification

Every frontend checkpoint should run formatting, linting, focused tests, and a
production SPA build. Changes to API shapes, session behavior, Actor ownership,
or event payloads must update `docs/salvia-integration.md` and the frontend and
backend plans in the same checkpoint.
