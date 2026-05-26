import { Elysia, t } from "elysia";
import { join, relative, resolve } from "path";

const HOST = Bun.env.HOST ?? "127.0.0.1";
const PORT = Number(Bun.env.PORT ?? "3001");
const BACKEND_HTTP = Bun.env.BACKEND_URL ?? "http://127.0.0.1:8080";
const BACKEND_WS = Bun.env.BACKEND_WS_URL ?? "ws://127.0.0.1:8080/ws";
const BACKEND_TOKEN = Bun.env.BACKEND_API_TOKEN ?? Bun.env.API_TOKEN ?? "";
const BACKEND_REQUEST_TIMEOUT_MS = Number(
  Bun.env.BACKEND_REQUEST_TIMEOUT_MS ?? "15000",
);
const BACKEND = `${BACKEND_HTTP.replace(/\/$/, "")}/api/v1`;
const CLIENT_DIST = join(import.meta.dir, "../dist");
const BFF_BASIC_AUTH = Bun.env.BFF_BASIC_AUTH?.trim() ?? "";
let backendReconnectDelayMs = 1000;
const requestMeta = new WeakMap<
  Request,
  { startedAt: number; requestId: string }
>();

type BasicAuthConfig = {
  username: string;
  password: string;
} | null;

function parseBasicAuthConfig(raw: string): BasicAuthConfig {
  if (!raw) {
    return null;
  }
  const separator = raw.indexOf(":");
  if (separator <= 0 || separator === raw.length - 1) {
    throw new Error("BFF_BASIC_AUTH must be in username:password format");
  }
  return {
    username: raw.slice(0, separator),
    password: raw.slice(separator + 1),
  };
}

const bffBasicAuth = parseBasicAuthConfig(BFF_BASIC_AUTH);

function isLoopbackHost(host: string) {
  const normalized = host
    .trim()
    .toLowerCase()
    .replace(/^\[(.*)\]$/, "$1");
  return (
    normalized === "127.0.0.1" ||
    normalized === "::1" ||
    normalized === "localhost"
  );
}

if (!isLoopbackHost(HOST) && Bun.env.ALLOW_REMOTE_BFF !== "1") {
  throw new Error(
    `Refusing to bind BFF to non-loopback host ${HOST} without ALLOW_REMOTE_BFF=1`,
  );
}
if (
  !isLoopbackHost(HOST) &&
  !bffBasicAuth &&
  Bun.env.ALLOW_INSECURE_REMOTE_BFF !== "1"
) {
  throw new Error(
    `Refusing remote BFF exposure on ${HOST} without BFF_BASIC_AUTH; set BFF_BASIC_AUTH or ALLOW_INSECURE_REMOTE_BFF=1`,
  );
}

// WebSocket clients for fan-out
const wsClients = new Set<{ send: (data: string) => void }>();

function secureHeaders(request: Request) {
  const headers = new Headers({
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
    "Referrer-Policy": "no-referrer",
    "Permissions-Policy": "camera=(), microphone=(), geolocation=()",
    "Cross-Origin-Opener-Policy": "same-origin",
    "Cross-Origin-Resource-Policy": "same-origin",
    "Content-Security-Policy": [
      "default-src 'self'",
      "base-uri 'none'",
      "frame-ancestors 'none'",
      "object-src 'none'",
      "form-action 'self'",
      "connect-src 'self'",
      "img-src 'self' data:",
      "script-src 'self'",
      "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
      "font-src 'self' https://fonts.gstatic.com",
    ].join("; "),
  });
  const forwardedProto = request.headers.get("x-forwarded-proto");
  const requestProto = new URL(request.url).protocol.replace(":", "");
  const scheme = forwardedProto?.split(",")[0]?.trim() || requestProto;
  if (scheme === "https") {
    headers.set(
      "Strict-Transport-Security",
      "max-age=31536000; includeSubDomains",
    );
  }
  return headers;
}

function authorizedForBFF(request: Request) {
  if (!bffBasicAuth) {
    return true;
  }
  const header = request.headers.get("authorization");
  if (!header || !header.startsWith("Basic ")) {
    return false;
  }
  const decoded = Buffer.from(header.slice("Basic ".length), "base64").toString(
    "utf8",
  );
  return decoded === `${bffBasicAuth.username}:${bffBasicAuth.password}`;
}

function unauthorizedBFFResponse(request: Request) {
  const headers = secureHeaders(request);
  headers.set(
    "WWW-Authenticate",
    'Basic realm="LSM Control Room", charset="UTF-8"',
  );
  headers.set("Content-Type", "text/plain; charset=utf-8");
  const meta = requestMeta.get(request);
  if (meta) {
    headers.set("X-Request-ID", meta.requestId);
  }
  return new Response("authentication required", {
    status: 401,
    headers,
  });
}

function requestIdFor(request: Request) {
  const incoming = request.headers.get("x-request-id")?.trim();
  if (incoming) {
    return incoming;
  }
  return crypto.randomUUID();
}

function logRequest(request: Request, status: number) {
  const meta = requestMeta.get(request);
  const startedAt = meta?.startedAt ?? Date.now();
  const requestId = meta?.requestId ?? "unknown";
  const url = new URL(request.url);
  console.log(
    JSON.stringify({
      event: "bff_request",
      request_id: requestId,
      method: request.method,
      path: url.pathname,
      status,
      duration_ms: Date.now() - startedAt,
      user_agent: request.headers.get("user-agent") ?? "",
      remote_addr: request.headers.get("x-forwarded-for") ?? "",
    }),
  );
}

const logAfterHandle: any = (context: any, response: any) => {
  const status =
    response instanceof Response
      ? response.status
      : typeof context.set?.status === "number"
        ? context.set.status
        : 200;
  logRequest(context.request as Request, status);
};

function backendHeaders(init?: HeadersInit) {
  const headers = new Headers(init);
  if (BACKEND_TOKEN) {
    headers.set("Authorization", `Bearer ${BACKEND_TOKEN}`);
  }
  return headers;
}

function proxyResponseHeaders(source: Headers) {
  const headers = new Headers();
  for (const key of ["content-type", "cache-control", "etag"]) {
    const value = source.get(key);
    if (value) {
      headers.set(key, value);
    }
  }
  return headers;
}

async function proxyRequest(path: string, init?: RequestInit) {
  const res = await fetch(`${BACKEND}${path}`, {
    ...init,
    headers: backendHeaders(init?.headers),
    signal: AbortSignal.timeout(BACKEND_REQUEST_TIMEOUT_MS),
  });
  return new Response(res.body, {
    status: res.status,
    headers: proxyResponseHeaders(res.headers),
  });
}

function backendFailureResponse(error: unknown) {
  const message =
    error instanceof Error ? error.message : "unknown backend error";
  return new Response(
    JSON.stringify({ error: "backend_unavailable", detail: message }),
    {
      status: 502,
      headers: {
        "Content-Type": "application/json; charset=utf-8",
        "X-Content-Type-Options": "nosniff",
      },
    },
  );
}

function backendWSURL() {
  const url = new URL(BACKEND_WS);
  if (BACKEND_TOKEN) {
    url.searchParams.set("access_token", BACKEND_TOKEN);
  }
  return url.toString();
}

// Connect to backend WebSocket and fan out to frontend clients
function connectBackendWS() {
  const ws = new WebSocket(backendWSURL());
  ws.onopen = () => {
    backendReconnectDelayMs = 1000;
  };
  ws.onmessage = (e) => {
    const data = e.data;
    for (const client of wsClients) {
      try {
        client.send(data);
      } catch {}
    }
  };
  ws.onclose = () => {
    const jitter = Math.floor(Math.random() * 250);
    const delay = Math.min(backendReconnectDelayMs, 30_000) + jitter;
    backendReconnectDelayMs = Math.min(backendReconnectDelayMs * 2, 30_000);
    setTimeout(connectBackendWS, delay);
  };
  ws.onerror = () => ws.close();
}

function clientFile(pathname: string) {
  const normalized = pathname.replace(/^\/+/, "");
  const target = resolve(CLIENT_DIST, normalized);
  const rel = relative(CLIENT_DIST, target);
  if (rel.startsWith("..") || rel === "") {
    return null;
  }
  return Bun.file(target);
}

async function serveClientIndex() {
  const indexFile = clientFile("index.html");
  if (indexFile && (await indexFile.exists())) {
    return new Response(indexFile, {
      headers: {
        "Content-Type": "text/html; charset=utf-8",
        "Cache-Control": "no-store",
      },
    });
  }
  return new Response(
    "Client build missing. Run `bun run index.ts` from the frontend directory.",
    {
      status: 503,
      headers: { "Content-Type": "text/plain; charset=utf-8" },
    },
  );
}

// Start backend WS connection (non-blocking)
setTimeout(connectBackendWS, 500);

export const app = new Elysia()
  .onRequest(({ request, set }) => {
    const requestId = requestIdFor(request);
    requestMeta.set(request, { startedAt: Date.now(), requestId });
    const path = new URL(request.url).pathname;
    const headers = secureHeaders(request);
    headers.set("X-Request-ID", requestId);
    headers.forEach((value, key) => {
      set.headers[key] = value;
    });
    if (path !== "/health" && !authorizedForBFF(request)) {
      return unauthorizedBFFResponse(request);
    }
  })
  .onAfterHandle(logAfterHandle)
  // Health
  .get("/health", () => ({ status: "ok" }))

  // DB operations (proxy to Go backend)
  .post(
    "/db/put",
    async ({ body }) => {
      try {
        return await proxyRequest("/db/put", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
      } catch (error) {
        return backendFailureResponse(error);
      }
    },
    { body: t.Object({ key: t.String(), value: t.String() }) },
  )

  .get(
    "/db/get",
    async ({ query }) => {
      try {
        return await proxyRequest(
          `/db/get?key=${encodeURIComponent(query.key)}`,
        );
      } catch (error) {
        return backendFailureResponse(error);
      }
    },
    { query: t.Object({ key: t.String() }) },
  )

  .delete(
    "/db/delete",
    async ({ query }) => {
      try {
        return await proxyRequest(
          `/db/delete?key=${encodeURIComponent(query.key)}`,
          {
            method: "DELETE",
          },
        );
      } catch (error) {
        return backendFailureResponse(error);
      }
    },
    { query: t.Object({ key: t.String() }) },
  )

  .get(
    "/db/scan",
    async ({ query }) => {
      try {
        return await proxyRequest(
          `/db/scan?start=${encodeURIComponent(query.start ?? "")}&end=${encodeURIComponent(query.end ?? "")}`,
        );
      } catch (error) {
        return backendFailureResponse(error);
      }
    },
    {
      query: t.Object({
        start: t.Optional(t.String()),
        end: t.Optional(t.String()),
      }),
    },
  )

  .get("/levels", async () => {
    try {
      return await proxyRequest("/levels");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  .post(
    "/db/batch",
    async ({ body }) => {
      try {
        return await proxyRequest("/db/batch", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
      } catch (error) {
        return backendFailureResponse(error);
      }
    },
    {
      body: t.Object({
        entries: t.Array(
          t.Object({
            key: t.String(),
            value: t.Optional(t.String()),
            delete: t.Optional(t.Boolean()),
          }),
        ),
      }),
    },
  )

  .get("/stats", async () => {
    try {
      return await proxyRequest("/stats");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  .get("/amplification", async () => {
    try {
      return await proxyRequest("/amplification");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  .get("/bloom/:fileID", async ({ params }) => {
    try {
      return await proxyRequest(`/bloom/${params.fileID}`);
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  .post(
    "/compaction/force",
    async ({ body }) => {
      try {
        return await proxyRequest("/compaction/force", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
      } catch (error) {
        return backendFailureResponse(error);
      }
    },
    { body: t.Object({ level: t.Number() }) },
  )

  .post(
    "/compaction/style",
    async ({ body }) => {
      try {
        return await proxyRequest("/compaction/style", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
      } catch (error) {
        return backendFailureResponse(error);
      }
    },
    { body: t.Object({ style: t.String() }) },
  )

  .post(
    "/bench/run",
    async ({ body }) => {
      try {
        return await proxyRequest("/bench/run", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
      } catch (error) {
        return backendFailureResponse(error);
      }
    },
    {
      body: t.Object({
        type: t.String(),
        num_keys: t.Number(),
        value_size: t.Number(),
        read_write_ratio: t.Optional(t.Number()),
      }),
    },
  )

  .get("/scenarios", async () => {
    try {
      return await proxyRequest("/scenarios");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  .post("/scenarios/:name/run", async ({ params }) => {
    try {
      return await proxyRequest(`/scenarios/${params.name}/run`, {
        method: "POST",
      });
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  .get("/wal/entries", async () => {
    try {
      return await proxyRequest("/wal/entries");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  .get("/cache/stats", async () => {
    try {
      return await proxyRequest("/cache/stats");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  .get("/db/open", async () => {
    try {
      return await proxyRequest("/db/open");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })
  .post("/db/close", async () => {
    try {
      return await proxyRequest("/db/close", { method: "POST" });
    } catch (error) {
      return backendFailureResponse(error);
    }
  })
  .get("/db/stats", async () => {
    try {
      return await proxyRequest("/db/stats");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })
  .get("/compaction/stats", async () => {
    try {
      return await proxyRequest("/compaction/stats");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })
  .get("/bench/result", async () => {
    try {
      return await proxyRequest("/bench/result");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })
  .get("/memtable/snapshot", async () => {
    try {
      return await proxyRequest("/memtable/snapshot");
    } catch (error) {
      return backendFailureResponse(error);
    }
  })

  // WebSocket fan-out to frontend
  .ws("/ws", {
    open(ws) {
      const client = { send: (data: string) => ws.send(data) };
      wsClients.add(client);
      (ws as any)._lsmClient = client;
    },
    close(ws) {
      const client = (ws as any)._lsmClient;
      if (client) wsClients.delete(client);
    },
    message() {},
  })

  // Serve static frontend
  .get("/assets/*", async ({ request, set }) => {
    const file = clientFile(new URL(request.url).pathname);
    if (file && (await file.exists())) {
      set.headers["x-content-type-options"] = "nosniff";
      return file;
    }
    set.status = 404;
    return "Not found";
  })
  .get("/", () => serveClientIndex())
  .get("/index.html", () => serveClientIndex())

  .listen({ hostname: HOST, port: PORT });

export type App = typeof app;

console.log(`BFF running on http://${HOST}:${app.server?.port}`);
