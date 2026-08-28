# Orchestrator image. Static binary, distroless-ish runtime, no shell in the
# final layer beyond what busybox provides for the healthcheck.
FROM golang:1.23-alpine AS build

WORKDIR /src
RUN apk add --no-cache git ca-certificates

# Dependencies first so a source-only change does not re-download the module graph.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/agentfleet ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl \
    && adduser -D -u 10001 agentfleet \
    && mkdir -p /var/lib/agentfleet/artifacts \
    && chown -R agentfleet /var/lib/agentfleet

COPY --from=build /out/agentfleet /usr/local/bin/agentfleet

# The orchestrator talks to the Docker socket, which on most hosts is owned by
# root:docker. Running as root here would be simpler and worse; the compose file
# passes the host's docker group id instead.
USER agentfleet
WORKDIR /var/lib/agentfleet

EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=20s \
    CMD curl -fsS http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/agentfleet"]
