# Salvia Integration Implementation Plan

This plan belongs in the separate Salvia repository. It describes Salvia work
against the Rosmarinus integration contract; it is not a plan to add a Next.js
application to the Rosmarinus repository.

## Goal

Build Salvia as the authentication and browser/UI boundary while Rosmarinus
remains the sole ActivityPub and federation service. An authenticated browser
obtains a short-lived Ably credential from Salvia, publishes commands directly
to Rosmarinus through Ably, receives account-scoped events through Ably, and
reads durable federation state from the shared MongoDB database.

```text
Browser
  | session / Ably token
  v
Salvia Next.js backend ---- writes ----> Salvia-owned MongoDB collections
  ^                                           |
  | reads Rosmarinus state                    | Rosmarinus reads account authz
  |                                           v
Shared MongoDB <-------------------------- Rosmarinus
  ^                                           ^
  | account events                            | authenticated commands
  +------------------------ Ably -------------+

No Salvia-to-Rosmarinus HTTP, RPC, or gRPC API
```

## Fixed Architecture Decisions

- MongoDB and Ably Pub/Sub are the only Salvia/Rosmarinus integration paths.
- Each MongoDB collection has one writer.
- Salvia writes only its own collections and reads documented Rosmarinus
  collections.
- Rosmarinus writes all federation state; Salvia never mutates it directly.
- Salvia authenticates local users and issues browser Ably credentials.
- `clientId` maps to a local account, not to an Actor.
- One account may own multiple local Actors through Actor `ownerAccountId`.
- Rosmarinus authorizes each command using the Ably `clientId` plus Actor
  ownership; payload identity fields are not trusted.
- MongoDB is canonical. Ably commands and events are asynchronous and may be
  duplicated, delayed, or missed by a disconnected browser.
- Account removal is soft deletion. Salvia does not cascade deletion into
  Rosmarinus collections.

## Phase 0: Establish the Salvia Repository Baseline

- [ ] Copy `AGENTS.md` and this `PLANS.md` into the Salvia repository root.
- [ ] Inspect Salvia's current Next.js version, routing mode, authentication
      library, MongoDB access layer, validation library, logging conventions,
      and test framework.
- [ ] Confirm `pnpm format`, `pnpm lint`, and `pnpm build` exist and are the
      repository's pre-commit verification commands.
- [ ] Document which existing authentication record maps to the stable local
      account `_id`; never derive it from email, display name, or another
      mutable attribute.
- [ ] Add a startup-validated, server-only configuration module for MongoDB and
      Ably settings.
- [ ] Add typed integration constants so channel names, protocol version, and
      message names are not duplicated across routes and components.

Exit criteria: the existing auth and data architecture is documented, required
scripts run, and no Rosmarinus source code has been introduced into Salvia.

## Phase 1: Add the Salvia-Owned Account Authorization Projection

- [ ] Implement a `salvia_accounts` repository owned exclusively by Salvia.
- [ ] Store this required cross-service projection:

  ```text
  _id             stable local account ID
  ablyClientId    stable opaque URL-safe identity
  status          active | suspended | deleted
  authzRevision   monotonically increasing integer
  createdAt       timestamp
  updatedAt       timestamp
  deletedAt       optional timestamp
  ```

- [ ] Generate `ablyClientId` once on the server with sufficient entropy. Do
      not use an Actor ID, email address, username, session ID, or tab ID.
- [ ] Create unique indexes on `_id` and `ablyClientId` from Salvia's migration
      or initialization path.
- [ ] Link the projection to the existing Salvia authentication user without
      making provider-specific fields part of the Rosmarinus contract.
- [ ] Implement atomic lifecycle updates that increment `authzRevision` on
      every authorization-relevant change.
- [ ] Prefer `status: deleted` plus `deletedAt` to hard deletion.
- [ ] Add Salvia-owned UI collections such as `salvia_ui_settings` and
      `salvia_actor_settings`; key Actor preferences by both account ID and
      Actor ID.
- [ ] Configure a Salvia MongoDB role with write access only to Salvia-owned
      collections and read access only to required Rosmarinus collections.
- [ ] Add repository tests for uniqueness, lifecycle transitions, and revision
      increments.

Exit criteria: every authenticated local user has one stable account projection
and `clientId`; Salvia cannot write Rosmarinus-owned collections with its normal
runtime credentials.

## Phase 2: Implement the Browser Ably Token Endpoint

- [ ] Define server-only environment variables, with names equivalent to:

  ```text
  ABLY_BROWSER_TOKEN_API_KEY
  ABLY_CONTROL_API_KEY
  ABLY_COMMAND_CHANNEL=rosmarinus:commands
  ABLY_ACCOUNT_EVENT_NAMESPACE=rosmarinus:accounts
  ABLY_ACCOUNT_CONTROL_CHANNEL=rosmarinus:control:accounts
  ABLY_TOKEN_TTL=15m
  ```

- [ ] Keep the browser token key and the account-control publisher key
      separate and give each the minimum Ably capability it needs.
- [ ] Add a Next.js server route that authenticates the existing session and
      loads its `salvia_accounts` projection.
- [ ] Return `401` for no session and deny a missing, suspended, or deleted
      projection.
- [ ] Issue a short-lived token/JWT whose `clientId` is fixed to the stored
      `ablyClientId`.
- [ ] Grant exactly:

  ```json
  {
    "rosmarinus:commands": ["publish"],
    "rosmarinus:accounts:{accountId}:events": ["subscribe"]
  }
  ```

- [ ] Derive the account ID, `clientId`, and channels server-side. Ignore or
      reject client-supplied identity and capability parameters.
- [ ] Return `Cache-Control: no-store`; ensure tokens and key material cannot
      enter application logs, analytics, or client bundles.
- [ ] Add rate limiting appropriate for token refresh without breaking the
      Ably SDK renewal flow.
- [ ] Test successful claims and capabilities, unauthenticated access,
      suspended/deleted accounts, cross-account channel denial, and expiry.

Exit criteria: an active session can connect only as its stored `clientId`,
publish commands, and subscribe only to its own account event channel.

## Phase 3: Build the Browser Realtime Layer

- [ ] Create one lifecycle-managed Ably client/provider per authenticated
      account using `authUrl` or `authCallback` so the SDK renews credentials.
- [ ] Close and discard the client on logout or account change.
- [ ] Subscribe to `rosmarinus:accounts:{accountId}:events` before enabling
      command actions that wait for a result.
- [ ] Add a cryptographically strong request ID generator and a pending-command
      store keyed by `request_id`.
- [ ] Reuse a request ID when retrying the same logical operation. Generate a
      new request ID for a new user intent.
- [ ] Treat publish acknowledgement as transport acknowledgement, not business
      success; resolve operations only from result events or canonical DB
      state.
- [ ] Handle reconnects by re-reading the relevant MongoDB-backed view. Do not
      assume Ably replay restores all missed state.
- [ ] Make event consumption safe across multiple tabs. Deduplicate UI effects
      by event identity/request ID while allowing every tab to refresh state.
- [ ] Add connection-state, timeout, cancellation, and retry UX without
      interpreting a timeout as proof of failure.

Exit criteria: browser connection and renewal work without exposing secrets,
and ambiguous network outcomes recover through idempotent retry and DB refresh.

## Phase 4: Implement Multi-Actor Account UX

- [ ] Add a read-only Rosmarinus Actor repository that lists local Actors by
      `ownerAccountId` for the authenticated account.
- [ ] Scope the query from the server session; do not accept an arbitrary
      account ID from the browser.
- [ ] Show an Actor switcher for zero, one, or many owned Actors.
- [ ] Store only UI selection/preferences in Salvia-owned collections. The
      selected Actor is convenience state, not authorization evidence.
- [ ] Publish `actor.create` without top-level `actor_id`:

  ```json
  {
    "version": 1,
    "request_id": "...",
    "data": {
      "username": "alice",
      "name": "Alice",
      "type": "Person"
    }
  }
  ```

- [ ] Handle `actor.created` and the associated `command.succeeded` event,
      then re-query the Actor collection before selecting the new Actor.
- [ ] Display authorization errors if an Actor was deleted, moved, or no
      longer belongs to the account; do not attempt to repair ownership from
      Salvia.
- [ ] Test one account owning multiple Actors and two accounts being unable to
      act through each other's Actors.

Exit criteria: one local account can create, list, select, and operate multiple
Actors while Rosmarinus remains the authority for Actor lifecycle and ownership.

## Phase 5: Implement Versioned Command Publishers

- [ ] Centralize the version 1 command envelope:

  ```json
  {
    "version": 1,
    "request_id": "stable unique request ID",
    "actor_id": "local Actor ID when required",
    "data": {}
  }
  ```

- [ ] Validate command data before publish and preserve the same JSON field
      names as the Rosmarinus contract.
- [ ] Implement `post.create` with required `note_id` and `text`, plus optional
      `visibility`, `content_warning`, `sensitive`, `in_reply_to_uri`,
      `quote_uri`, `mention_uris`, and `hashtags`.
- [ ] Implement `follow.create` with a Fediverse handle or absolute Actor URL
      in `data.target`.
- [ ] Implement `follow.delete` with the owned local `actor_id` and the same
      handle or absolute Actor URL in `data.target`.
- [ ] Implement `reaction.create` with the owned local `actor_id`,
      `data.note_id`, and `data.reaction`.
- [ ] Implement `reaction.delete` with the owned local `actor_id` and
      `data.note_id`.
- [ ] Implement `follow.approve` with top-level local followee `actor_id` and
      `data.follower_id`.
- [ ] Implement `follow.reject` with the same addressing model.
- [ ] Never include account/user identity in `data` as authorization proof.
- [ ] Preserve request IDs across retries so Rosmarinus command receipts can
      prevent duplicate effects.
- [ ] Handle current Rosmarinus `command.failed` codes
      `command_in_progress` and `command_failed` without silently republishing
      under a new ID.
- [ ] Treat a timeout as ambiguous. The current Rosmarinus handler may reject
      malformed, unauthenticated, or unauthorized commands before publishing a
      result event; refresh canonical state and retry the same logical request
      with the same request ID when appropriate.
- [ ] If typed validation or authorization failures are required by the UI,
      plan them as a versioned Rosmarinus contract change before Salvia depends
      on them.
- [ ] Add tests for envelope validation, Actor selection, stable retry IDs, and
      unsupported protocol versions.

Exit criteria: each supported UI mutation becomes a typed, validated,
idempotently retryable Ably command and never a direct database write.

## Phase 6: Implement Versioned Event Consumers

- [ ] Validate this version 1 event envelope at runtime:

  ```json
  {
    "version": 1,
    "type": "event type",
    "request_id": "optional correlated request ID",
    "actor_id": "optional local Actor ID",
    "occurred_at": "RFC 3339 timestamp",
    "data": {}
  }
  ```

- [ ] Implement `command.succeeded` and `command.failed` correlation.
- [ ] Implement refresh/invalidation handlers for `actor.created`,
      `post.created`, `notification.created`, `follow.approval.requested`,
      `follow.approval.completed`, and `follow.approval.rejected`.
- [ ] Treat domain events as refresh hints and render durable state from
      MongoDB wherever possible.
- [ ] Ignore and log unknown future event types at an appropriate level instead
      of disconnecting the client.
- [ ] Reject unsupported envelope versions explicitly and expose enough
      telemetry to detect a contract mismatch.
- [ ] Deduplicate notifications and command completion effects across repeated
      deliveries and multiple tabs.
- [ ] Add tests for malformed events, duplicates, out-of-order result/domain
      events, unknown types, and missed-event DB rebuild.

Exit criteria: supported events update the UI promptly, while reconnect and
duplicate scenarios converge on the MongoDB source of truth.

## Phase 7: Implement Account Authorization Invalidation

- [ ] Add a server-only Ably REST publisher for
      `rosmarinus:control:accounts` using `ABLY_CONTROL_API_KEY`.
- [ ] For suspend/delete/authorization change, atomically update status and
      increment `authzRevision` before publishing any control message.
- [ ] Publish message name `account.authorization.changed` with:

  ```json
  {
    "account_id": "account ID",
    "authz_revision": 2
  }
  ```

- [ ] Stop issuing new browser credentials as soon as the database state is no
      longer active.
- [ ] Use short browser-token TTLs so already-issued capability expires within
      the documented window.
- [ ] Retry a failed control publish with the same revision. Do not roll back
      the database change because the message is an invalidation hint and
      Rosmarinus also reconciles periodically.
- [ ] Do not write, suspend, or delete Rosmarinus Actor documents from this
      lifecycle path.
- [ ] Test ordering, repeated revisions, publish failure, token denial after
      suspension, and eventual Rosmarinus rejection of existing clients.

Exit criteria: the durable account state changes first, credential issuance
stops, and Rosmarinus receives low-latency invalidation without an HTTP call.

## Phase 8: Build Read-Only Federation Views

- [ ] Enumerate the Rosmarinus collections and fields each Salvia screen needs;
      use explicit projections rather than passing raw documents to the
      browser.
- [ ] Confirm required query indexes with the Rosmarinus team. Index changes on
      Rosmarinus-owned collections must be implemented and committed in the
      Rosmarinus repository, not Salvia.
- [ ] Scope all local-Actor queries through authenticated account ownership.
- [ ] Add stable pagination and deterministic sorting for notes, followers,
      following, reactions, and notifications where applicable.
- [ ] Build reply and quote projections from Note `replyId` and `quoteId`;
      retain `inReplyToUri` and `quoteUri` only as federation source metadata.
- [ ] Project remote Actor `movedToUri`, `alsoKnownAs`, and `movedAt` where the
      UI needs moved-account labels or navigation; treat them as read-only and
      trust `movedAt`, not an unverified URI alone, as the accepted-move signal.
- [ ] Project the explicit remote Actor profile fields documented in the
      integration contract; render converted summary/property text safely and
      distinguish remote avatar/banner source URLs from cached media.
- [ ] Render remote featured posts by joining Actor `featuredNoteIds` to
      `notes._id`; preserve the stored order and tolerate missing/deleted Notes.
- [ ] Model eventual consistency in loading and post-command UI states.
- [ ] Keep presentation fields and preferences in Salvia-owned collections.
- [ ] Verify the Salvia runtime MongoDB role cannot mutate a Rosmarinus
      collection even if application code attempts it.

Exit criteria: UI queries are account-scoped, indexed, projection-based, and
enforced as read-only at the database permission boundary.

## Phase 9: End-to-End Contract Verification

- [ ] Test authenticated token issuance through a real Salvia session.
- [ ] Test browser command -> Ably -> Rosmarinus -> MongoDB -> account event ->
      UI for Actor creation, post creation, and follow approval/rejection.
- [ ] Test one account with at least two Actors and cross-account Actor denial.
- [ ] Test retrying the same request ID produces no duplicate federation side
      effect.
- [ ] Test browser disconnect during command processing and recovery from DB
      without relying on a retained event.
- [ ] Test multiple tabs receiving the same event.
- [ ] Test suspension and deletion while a browser is connected.
- [ ] Test Ably control-message loss and periodic Rosmarinus reconciliation.
- [ ] Test MongoDB permissions for both services and prove that no collection
      has two writers.
- [ ] Add an architectural test or CI check that prevents introducing a direct
      Salvia-to-Rosmarinus HTTP/RPC client.

Exit criteria: identity, authorization, idempotency, ownership, reconnect, and
collection-writer boundaries are verified in realistic flows.

## Phase 10: Operations and Rollout

- [ ] Add structured logs and metrics keyed by safe account/Actor identifiers
      and `request_id`; never record tokens, API keys, session cookies, or full
      sensitive content.
- [ ] Monitor token issuance failures, Ably connection failures, command
      latency, command failures by code, pending-command timeouts, event schema
      failures, and account invalidation publish failures.
- [ ] Document rotation for browser-token and control-publisher Ably keys.
- [ ] Document the browser token TTL and maximum suspension propagation delay.
- [ ] Document channel/version migration procedures that allow Salvia and
      Rosmarinus to deploy independently.
- [ ] Roll out behind feature flags where existing Salvia behavior must remain
      available during migration.
- [ ] Provide an operator runbook for Ably outage, MongoDB read degradation,
      command backlog, and contract-version mismatch.

Exit criteria: the integration can be observed, keys can be rotated, failures
degrade predictably, and the two repositories can release independently.

## Rosmarinus Dependencies Already Available

The current Rosmarinus contract provides:

- lookup of `salvia_accounts` by Ably `clientId`;
- account status and authorization revision checks;
- one-account-to-many-Actor ownership through `ownerAccountId`;
- `actor.create`, `post.create`, `follow.create`, `follow.delete`,
  `reaction.create`, `reaction.delete`, `follow.approve`, and `follow.reject`
  commands;
- command receipt persistence for request ID idempotency;
- account-scoped result and domain event publication;
- `notification.created` account event publication;
- `account.authorization.changed` invalidation; and
- periodic account authorization reconciliation.

If Salvia needs an additional federation mutation, event, field, or query index,
treat it as a versioned cross-repository contract change. Plan and commit the
Rosmarinus change in its own repository before depending on it from Salvia.

## Definition of Done for Every Salvia Checkpoint

- [ ] The change respects the single-writer collection boundary.
- [ ] No direct Salvia-to-Rosmarinus HTTP/RPC dependency was added.
- [ ] Runtime configuration comes from validated environment variables.
- [ ] Focused tests cover the changed identity, authorization, or event path.
- [ ] No secrets or tokens are present in client bundles or logs.
- [ ] `pnpm format` passes.
- [ ] `pnpm lint` passes.
- [ ] `pnpm build` passes.
- [ ] The commit contains only the coherent completed checkpoint.
- [ ] The git commit is signed; an unsigned fallback is forbidden.
