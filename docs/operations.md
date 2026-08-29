# Rosmarinus Operations

## Queue capacity

Rosmarinus runs one Asynq server by default and applies Redis-backed global
per-second limits before federation handlers run. The defaults follow current
Misskey:

```text
INBOX_CONCURRENCY=16
INBOX_RATE_PER_SECOND=32
DELIVER_CONCURRENCY=128
DELIVER_RATE_PER_SECOND=128
```

`INBOX_MAX_RETRY`, `DELIVER_MAX_RETRY`, `INBOX_TIMEOUT`, and
`DELIVER_TIMEOUT` control retry and execution limits independently. All
concurrency and rate values must be positive. Redis rate buckets are shared by
split worker processes using the same Redis database, so adding processes does
not multiply the configured federation request rate.

## Inbox activity receipts

After signature verification, inbox workers claim the Activity URI in the
MongoDB `inbox_activity_receipts` collection. A processing lease prevents
concurrent execution, and a completed receipt prevents sequential peer retries
or promoted queue jobs from repeating side effects. `INBOX_ACTIVITY_RECEIPT_TTL`
controls retention and defaults to `168h`; it must exceed `INBOX_TIMEOUT` plus
the one-minute lease margin. MongoDB removes expired receipts through the
`expiresAt` TTL index.

Handler mutations still use their own unique keys because a crash can happen
after a mutation but before its receipt is completed. If processing fails, the
worker releases only its token-matched lease so an Asynq retry can safely
resume. This receipt collection is internal to Rosmarinus and does not change
the Salvia shared-data or Ably contracts.

## Outbound network safety

ActivityPub object fetches and deliveries resolve hosts locally, reject every
non-public DNS answer, and connect to the validated address without using an
environment proxy. The same private-address boundary applies to media and
instance metadata fetches. Redirect targets are revalidated before they are
followed.

`MEDIA_ALLOWED_PRIVATE_NETWORKS` is the historical name of the shared CIDR
allowlist for these remote HTTP clients. Leave it empty in production. Set it
only for controlled private federation topologies such as the Docker
compatibility fixture; allowing a network makes any service on that network a
possible federation target.

## Failed task inspection

Asynq archives a task after its configured retries are exhausted. Inspect the
latest archived tasks as JSON without starting MongoDB or the application
server:

```sh
rosmarinus queue failed deliver
rosmarinus queue failed inbox 100
```

The optional limit is between 1 and 100 and defaults to 30. Each record includes
the task ID, type, versioned payload, retry counts, last error, and failure
timestamp. Treat payloads as operational data: inbox payloads contain remote
HTTP Signature material and activities may contain private federation content.

## Promoting federation work

Move an archived, retry-delayed, or scheduled inbox/deliver task back to the
pending queue with its Asynq task ID:

```sh
rosmarinus queue promote deliver TASK_ID
rosmarinus queue promote inbox TASK_ID
```

Promotion preserves the existing task payload. Only the `inbox` and `deliver`
queues are accepted by this command. Inspect the failure first and avoid
re-running a task whose target or authorization boundary is no longer valid.
