# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.2.0-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X homelab-agent/internal/version.Version=${VERSION}" -o /out/homelab-agent ./cmd/homelab-agent

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 65532 agent && adduser -D -H -u 65532 -G agent agent
COPY --from=build /out/homelab-agent /usr/local/bin/homelab-agent
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/homelab-agent"]
CMD ["-config", "/etc/homelab-agent/config.yaml"]
