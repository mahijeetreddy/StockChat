# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && mkdir -p /out/data

# ---- runtime: distroless, non-root ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
# Pre-create the data dir owned by the nonroot user so the named volume inherits it.
COPY --from=build --chown=65532:65532 /out/data /data
ENV BIND_ADDR=0.0.0.0 \
    PORT=8080 \
    APP_ENV=prod \
    DB_PATH=/data/stockchat.db
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/app/server", "healthcheck"]
ENTRYPOINT ["/app/server"]
