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

## HTTP Signatures

- ActivityPub still commonly uses draft-era HTTP Signatures, so compatibility matters more than strict modern spec interpretation.
- Use `github.com/go-fed/httpsig` for HTTP Signature signing and verification.
- Match current Misskey's signed header sets and validation behavior where
  practical, while preserving explicitly tested compatibility with real-world
  `@peertube/http-signature` peers.

## Federation Tests

- Keep `test/federation/misskey_test.go` organized into clearly labeled phase
  comments. Each phase comment must state the federation behavior being
  exercised and the outcome it verifies.
- For every implementation checkpoint, assess whether the behavior can be
  verified through the existing real-Misskey fixture. When it can, update
  `test/federation/misskey_test.go` and its workflow documentation in the same
  checkpoint.

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
