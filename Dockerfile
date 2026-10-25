# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src

# Dependencies first so a source-only change does not refetch them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o /out/rh-sc-mcp ./cmd/rh-sc-mcp

FROM gcr.io/distroless/static-debian12:nonroot

# OpenShift assigns an arbitrary UID from the namespace range and ignores
# the image's USER, so nothing may depend on a particular uid. The binary
# is world-readable, and with the CMD below the process only reads the
# config it is given — it writes nothing — which is what is meant to make
# an unassigned uid safe. Checked once, with uid 1000650000; that is one
# sample, not a sweep. Drop the --config and it will try to create a
# default under $HOME, which an assigned uid does not own. runAsNonRoot
# in the manifests needs a numeric USER to check against, hence the uid
# rather than a name.
USER 65532:65532

COPY --from=build /out/rh-sc-mcp /usr/local/bin/rh-sc-mcp

EXPOSE 8080

# Config and credentials are mounted or passed as environment, never baked
# in: an image layer is readable by anyone who can pull the image, and this
# server holds a Red Hat offline token.
ENTRYPOINT ["/usr/local/bin/rh-sc-mcp"]
CMD ["--config", "/etc/rh-sc-mcp/config.yaml"]
