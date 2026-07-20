FROM node:22-bookworm AS frontend

WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.24-bookworm AS backend

ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/out ./static/out
RUN CGO_ENABLED=0 go build -tags=jsoniter \
    -ldflags="-X 'github.com/bestruirui/octopus/internal/conf.Version=${VERSION}' -X 'github.com/bestruirui/octopus/internal/conf.Author=tianxia3111' -X 'github.com/bestruirui/octopus/internal/conf.Commit=${COMMIT}' -s -w" \
    -o /octopus .

FROM debian:bookworm-slim

ENV TZ=Asia/Shanghai
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata gosu \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /app/data
COPY --from=backend /octopus /app/octopus
COPY scripts/dockerfiles/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 8080
VOLUME ["/app/data"]
CMD ["/entrypoint.sh"]
