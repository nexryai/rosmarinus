# Salvia Integration Contract

Salvia and Rosmarinus share MongoDB and Ably Pub/Sub. They do not call each
other over HTTP. MongoDB is authoritative; Ably carries browser commands,
command results, account events, and low-latency account invalidations.

## Collection ownership

Each collection has one writer and one migration/index owner.

| Collection | Writer | Other service access |
| --- | --- | --- |
| `salvia_accounts` | Salvia | Rosmarinus reads the authorization projection |
| Salvia session and UI collections | Salvia | No Rosmarinus access |
| `actors`, `notes`, `follows`, `reactions`, `blocks`, `abuse_reports` | Rosmarinus | Salvia reads UI-facing state |
| `connector_command_receipts` | Rosmarinus | Salvia does not need write access |

Use separate MongoDB users with collection-scoped custom roles. The Salvia role
must have read/write and index privileges only on Salvia-owned collections and
`find` on the Rosmarinus collections it renders. The Rosmarinus role must have
read/write and index privileges only on Rosmarinus-owned collections and
`find` on `salvia_accounts`. It must not have access to Salvia sessions.

Rosmarinus never creates an index on `salvia_accounts`. Salvia must provide a
unique index on `ablyClientId`.

## Salvia account projection

Salvia writes one document per local account:

```json
{
  "_id": "account-01J...",
  "ablyClientId": "client-01J...",
  "status": "active",
  "authzRevision": 4,
  "deletedAt": null
}
```

Supported status values are `active`, `suspended`, and `deleted`. Accounts
referenced by Actors are soft-deleted. Rosmarinus accepts a state-changing
command only when the account is `active` and `deletedAt` is absent or null.

`ablyClientId` is an opaque, stable, URL-safe account identifier. It is not an
Actor ID, username, or email address. Changing it immediately prevents the old
client ID from resolving in Rosmarinus, independently of Ably token expiry.

## Actor ownership

Rosmarinus writes `ownerAccountId` on every user-managed local Actor. One
account can own multiple Actors; one Actor has one owner. Remote Actors have no
owner. The environment-provisioned Actor has `isSystemActor: true` and no
account owner.

Actor-bound commands are authorized with one MongoDB query over `_id`,
`ownerAccountId`, `host: null`, and `isSuspended: false`. The command payload
cannot override account ownership.

System Actor events are not published to an account event channel because no
account owns them. A future operator workflow must use a separately authorized
system-event channel rather than assigning a fake account owner.

## Ably channels and keys

Default channels are:

| Channel | Publisher | Subscriber |
| --- | --- | --- |
| `rosmarinus:commands` | Authenticated browser | Rosmarinus |
| `rosmarinus:accounts:{accountId}:events` | Rosmarinus | That account's browser clients |
| `rosmarinus:control:accounts` | Salvia backend | Rosmarinus |

Use three least-privilege Ably keys:

1. Salvia browser-token issuer key: tokens may publish to
   `rosmarinus:commands` and subscribe only to the authenticated account's
   exact event channel.
2. Salvia control key: publish only to `rosmarinus:control:accounts`.
3. Rosmarinus service key: subscribe to command/control channels and publish
   to account event channels.

Salvia sets a non-wildcard `x-ably-clientId` and explicit capabilities in every
short-lived browser JWT. The browser never receives an API key, wildcard
capability, command-channel subscribe permission, event-channel publish
permission, or control-channel access.

## Browser command envelope

The browser subscribes to its account event channel before publishing a
command. Every command has an Ably message name and this versioned payload:

```json
{
  "version": 1,
  "request_id": "request-01J...",
  "actor_id": "actor-01J...",
  "data": {}
}
```

`request_id` is stable across retries. Rosmarinus claims a unique
`{accountId, requestId}` receipt before mutation. A duplicate does not repeat
the mutation; it republishes the stored result or reports that the original is
still in progress.

`actor.create` omits `actor_id` because the Actor does not exist yet:

```json
{
  "version": 1,
  "request_id": "request-01J...",
  "data": {
    "username": "alice-work",
    "name": "Alice Work",
    "type": "Person"
  }
}
```

Rosmarinus generates the Actor ID, URI, and key pair and derives
`ownerAccountId` from Ably `message.clientId` through `salvia_accounts`.

## Account invalidation and recovery

After updating `salvia_accounts`, Salvia publishes:

```json
{
  "account_id": "account-01J...",
  "authz_revision": 5
}
```

with the message name `account.authorization.changed` on the account control
channel. The event is only an invalidation. Rosmarinus reads the current Salvia
document and never trusts status supplied in the event payload.

Inactive or missing accounts are rejected on every browser mutation. A control
event immediately suspends user-managed Actors belonging to an inactive
account. Rosmarinus also periodically lists account owners in its Actor
collection and re-reads `salvia_accounts`, so a missed Ably control event is
eventually repaired from MongoDB.

## Rosmarinus environment

| Variable | Default |
| --- | --- |
| `ABLY_ROSMARINUS_API_KEY` | empty; Connector disabled |
| `CONNECTOR_COMMAND_CHANNEL` | `rosmarinus:commands` |
| `CONNECTOR_ACCOUNT_EVENT_NAMESPACE` | `rosmarinus:accounts` |
| `CONNECTOR_ACCOUNT_CONTROL_CHANNEL` | `rosmarinus:control:accounts` |
| `SALVIA_ACCOUNT_COLLECTION` | `salvia_accounts` |
| `CONNECTOR_RECEIPT_TTL` | `168h` |
| `CONNECTOR_ACCOUNT_RECONCILE_INTERVAL` | `5m` |
