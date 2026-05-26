# frontend

This frontend now has a single live path:

- `bun run index.ts` builds the React client and starts the Bun/Elysia BFF in `server/bff.ts`
- the browser dashboard is built from `src/` through Vite and served from `dist/`

To install dependencies:

```bash
bun install
```

To run:

```bash
bun run index.ts
```

Optional hardening:

```bash
API_TOKEN=change-me \
BFF_BASIC_AUTH=viewer:panel \
bun run index.ts
```

`BFF_BASIC_AUTH` protects the browser-facing dashboard, proxied API routes, and BFF WebSocket with HTTP Basic auth. Non-loopback BFF binds now require either `BFF_BASIC_AUTH` or an explicit insecure override.

Useful scripts:

```bash
bun run build:client
bun run dev:client
bun run typecheck
```

The legacy inline `public/index.html` dashboard was removed. The only supported
frontend path is now the React client plus the Bun BFF.
