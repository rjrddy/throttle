FROM node:22-bookworm-slim AS web
WORKDIR /web
COPY apps/web/package*.json ./
RUN npm ci
COPY apps/web/ ./
RUN npm run build
FROM golang:1.27.3-bookworm AS go
WORKDIR /src
COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/
RUN go test ./... && CGO_ENABLED=0 go build -trimpath -o /throttle ./cmd/throttle
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* && groupadd --gid 10001 throttle && useradd --uid 10001 --gid throttle --no-create-home throttle
WORKDIR /app
COPY --from=go /throttle /app/throttle
COPY --from=web /web/dist /app/web
USER throttle
EXPOSE 8086
ENTRYPOINT ["/app/throttle"]
CMD ["--demo", "--addr", "0.0.0.0:8086", "--web", "/app/web"]
