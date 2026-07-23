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

## Concorde Compatibility

- Treat `./concorde` as the behavioral reference for ActivityPub federation.
- Do not edit `./concorde`.
- For ActivityPub parsing, HTTP signatures, MFM handling, custom emoji handling, and federation edge cases, read Concorde and implement Rosmarinus as closely as practical.
- Treat `./misskey` only as a Docker federation test fixture/reference. Do not use it as the primary implementation guide.

## HTTP Signatures

- ActivityPub still commonly uses draft-era HTTP Signatures, so compatibility matters more than strict modern spec interpretation.
- Use `github.com/go-fed/httpsig` for HTTP Signature signing and verification.
- Preserve Concorde-compatible behavior where it matches real-world `@peertube/http-signature` federation behavior.

## Federation Tests

- Keep `test/federation/misskey_test.go` organized into clearly labeled phase
  comments. Each phase comment must state the federation behavior being
  exercised and the outcome it verifies.
- Update the Misskey federation workflow when adding behavior that can be
  verified through the existing real-server fixture.

## Git Workflow

- Make git commits at coherent implementation checkpoints after tests pass.
- Do not mix unrelated or unfinished work into the same commit.
- Always create signed git commits.
- If signing fails, do not create an unsigned commit. Stop and notify the user
  that the commit could not be signed.
