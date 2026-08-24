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
