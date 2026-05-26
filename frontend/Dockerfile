FROM oven/bun:1.3.10 AS build

WORKDIR /app
COPY package.json bun.lock tsconfig.json vite.config.ts index.ts ./
COPY client ./client
COPY server ./server
COPY src ./src
RUN bun install --frozen-lockfile
RUN bun run build:client

FROM oven/bun:1.3.10

WORKDIR /app
COPY --from=build /app /app

ENV HOST=0.0.0.0
ENV PORT=3001
ENV SKIP_CLIENT_BUILD=1
ENV ALLOW_REMOTE_BFF=1

EXPOSE 3001

CMD ["bun", "run", "index.ts"]
