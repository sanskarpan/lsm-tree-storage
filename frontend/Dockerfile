# Pin with digest for reproducible builds.
# Update digest when upgrading: docker pull oven/bun:1.3.10 && docker inspect --format='{{index .RepoDigests 0}}' oven/bun:1.3.10
FROM oven/bun:1.3.10@sha256:b86c67b531d87b4db11470d9b2bd0c519b1976eee6fcd71634e73abfa6230d2e AS build

WORKDIR /app
COPY package.json bun.lock tsconfig.json vite.config.ts index.ts ./
COPY client ./client
COPY server ./server
COPY src ./src
RUN bun install --frozen-lockfile
RUN bun run build:client

FROM oven/bun:1.3.10@sha256:b86c67b531d87b4db11470d9b2bd0c519b1976eee6fcd71634e73abfa6230d2e

RUN useradd --system --no-create-home --uid 1001 appuser

WORKDIR /app
COPY --from=build /app /app

RUN chown -R appuser /app

USER appuser

ENV HOST=0.0.0.0
ENV PORT=3001
ENV SKIP_CLIENT_BUILD=1
ENV ALLOW_REMOTE_BFF=1

EXPOSE 3001

CMD ["bun", "run", "index.ts"]
