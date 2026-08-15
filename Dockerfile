# AnchorDB links tree-sitter, so this is a cgo build and cannot use a scratch
# or static base. The runtime stage carries glibc and git for that reason.
FROM golang:1.25-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=1 go build \
      -trimpath \
      -buildvcs=false \
      -ldflags "-s -w -X github.com/jolovicdev/anchor-db/internal/version.Version=${VERSION#v}" \
      -o /out/ \
      ./cmd/anchordb-mcp ./cmd/anchorctl ./cmd/anchord

FROM debian:bookworm-slim

# AnchorDB shells out to git to resolve refs and read diffs, so git is a runtime
# dependency, not a build one.
RUN apt-get update \
    && apt-get install --no-install-recommends -y git ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    # Mounted repositories are owned by the host user, which git otherwise
    # refuses to read. Inside a container that ownership check protects nothing.
    && git config --system --add safe.directory '*'

COPY --from=build /out/ /usr/local/bin/

# Anchors record absolute repository paths, so a repository must be mounted at
# the same path it has on the host for those paths to stay meaningful.
VOLUME ["/data"]

ENTRYPOINT ["anchordb-mcp"]
CMD ["--db", "/data/anchor.db"]
