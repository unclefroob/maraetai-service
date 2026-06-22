# Build a static binary, then ship it on a minimal base.
FROM golang:1.26 AS build
WORKDIR /src
# Copy the module files first so dependency download is cached independently of
# source changes. go.sum is required to build with dependencies.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off → fully static binary (pure-Go SQLite), runnable on distroless/static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/maraetai-service .
# A /data dir owned by the distroless nonroot uid (65532). A fresh named volume
# mounted here inherits that ownership, so the nonroot process can write the DB
# without a manual chown.
RUN mkdir -p /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/maraetai-service /maraetai-service
COPY --from=build --chown=65532:65532 /data /data
# Absolute DB path so a `-v <vol>:/data` mount persists the play history by
# default. The config default is relative (./data/maraetai.db), which resolves
# against the container's working dir — NOT a mounted /data — so the DB would
# land in the ephemeral layer and be lost on container recreate.
ENV DB_PATH=/data/maraetai.db
EXPOSE 4534
# The binary checks its own /healthz (no shell/curl in distroless).
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/maraetai-service", "healthcheck"]
ENTRYPOINT ["/maraetai-service"]
