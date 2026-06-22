# Build a static binary, then ship it on a minimal base.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
# (go.sum copied when dependencies are added in later milestones)
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/maraetai-service .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/maraetai-service /maraetai-service
EXPOSE 8080
ENTRYPOINT ["/maraetai-service"]
