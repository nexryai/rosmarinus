# Salvia

Salvia is the static React SPA for Rosmarinus. It uses the Rosmarinus REST API
for commands and queries, authenticated SSE for live invalidation, and passkeys
for authentication. It has no Next.js server, Ably client, MongoDB client, or
Redis client.

## Development

Requirements: a current Node.js release and `pnpm`.

```sh
pnpm install --frozen-lockfile
pnpm dev
```

The Vite development server proxies `/api` to `http://127.0.0.1:3000`. Override
the public REST base with `VITE_API_BASE_URL` only when the SPA is not served
from the Rosmarinus origin. Never put credentials or secrets in Vite variables.

## Checks and production build

```sh
pnpm format
pnpm lint
pnpm test
pnpm build
```

The production assets are written to `dist/`. Serve `index.html` without a
long-lived cache and hashed assets with an immutable cache. Routes such as
`/settings` and `/profiles/*` need an `index.html` history fallback; `/api/*`
and ActivityPub routes must reach Rosmarinus instead of that fallback.

Tailwind CSS provides the styling utilities and theme tokens. Tabler Icons is
the icon set. The yellow visual direction, passkey-only flow, and multiple-Actor
workflow follow the read-only `salvia-old` product reference.

## Images

Rosmarinus intentionally performs no image transformation so it remains
buildable with `CGO_ENABLED=0`. When a media upload API is added, Salvia's
`createCanvasThumbnail` helper must create upload previews and thumbnails in
the browser with Canvas while retaining the original file required by the API.
