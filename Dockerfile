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

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/maraetai-service /maraetai-service
EXPOSE 4534
# The binary checks its own /healthz (no shell/curl in distroless).
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/maraetai-service", "healthcheck"]
ENTRYPOINT ["/maraetai-service"]
