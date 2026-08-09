# Build stage. Runs code generation because backend/internal/gen and
# backend/internal/database/sqlc are gitignored — a clean checkout has neither,
# so a build that skipped this step would fail on missing packages.
#
# Build context is the repository root, not backend/, because the proto
# definitions live outside this directory:
#   docker build -f backend/Dockerfile -t idp-backend:dev .
FROM golang:1.24-alpine AS build

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Dependencies resolve in their own layer so a source-only change does not
# re-download the module cache.
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /src/backend
RUN go mod download

RUN go install github.com/bufbuild/buf/cmd/buf@latest \
    && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest \
    && go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest \
    && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

WORKDIR /src
COPY proto ./proto
COPY backend ./backend

RUN cd proto && buf generate
RUN cd backend && sqlc generate

# CGO off yields a static binary that runs on distroless.
# -trimpath keeps build-host paths out of panics.
ARG VERSION=dev
RUN cd backend \
    && CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/idp-server ./cmd/server

# Runtime stage. Distroless rather than alpine: no shell and no package
# manager, so a container breakout has nothing to pivot with. The nonroot
# variant runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot

# The platform makes outbound TLS calls to Keycloak (JWKS) and to container
# registries, both of which need the public trust store.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/idp-server /usr/local/bin/idp-server

USER nonroot:nonroot
EXPOSE 8090

# No shell in the image, so this must be exec form.
ENTRYPOINT ["/usr/local/bin/idp-server"]
