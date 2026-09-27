# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.3.2-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/brendlij/labbeacon/internal/version.Version=${VERSION}" -o /out/labbeacon ./cmd/labbeacon

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 65532 agent && adduser -D -H -u 65532 -G agent agent
COPY --from=build /out/labbeacon /usr/local/bin/labbeacon
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/labbeacon"]
CMD ["-config", "/etc/labbeacon/config.yaml"]
