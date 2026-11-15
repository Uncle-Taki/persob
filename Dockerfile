# ==========================================
# Build Stage
# ==========================================
FROM golang:1.26.11-alpine AS builder

WORKDIR /app

ENV GOTOOLCHAIN=auto
RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/perso ./cmd/perso

# ==========================================
# Runtime Stage
# ==========================================
FROM alpine:3.23

# Refresh the runtime layer's Alpine packages before installing application dependencies.
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 10001 -S perso && \
    adduser -u 10001 -S perso -G perso

# Persistent directories
RUN mkdir -p /data/sessions && \
    chown -R perso:perso /data/sessions

WORKDIR /app

COPY --from=builder /app/bin/perso /app/perso
COPY --from=builder /app/scripts/seed.sql /app/scripts/seed.sql

# API and worker also construct the collector; use the owned runtime directory.
ENV TELEGRAM_SESSION_DIR=/data/sessions

USER perso:perso

EXPOSE 8080

ENTRYPOINT ["/app/perso"]
CMD ["all"]
