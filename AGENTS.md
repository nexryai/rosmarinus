# Rosmarinus ActivityPub Server

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

## Project Scope

- Rosmarinus is the successor project to Concorde, a Misskey fork.
- Rosmarinus is an ActivityPub microservice. It does not include a frontend or a public Misskey-compatible API.
- Its main responsibility is communicating with other ActivityPub servers and writing federation state to MongoDB.
- Use MongoDB as the database and design collections/indexes with MongoDB best practices.

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
  microservice scope, MongoDB model, mandatory follow approval policy, Go
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

- For every implementation checkpoint, assess whether it changes an Ably
  command or event, a shared MongoDB read contract, account/Actor ownership,
  authorization behavior, or federation state consumed by Salvia.
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
