# ==========================================
# Build Stage
# ==========================================
FROM golang:alpine AS builder

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
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 10001 -S perso && \
    adduser -u 10001 -S perso -G perso

# Persistent directories
RUN mkdir -p /data/sessions && \
    chown -R perso:perso /data/sessions

WORKDIR /app

COPY --from=builder /app/bin/perso /app/perso
COPY --from=builder /app/scripts/seed.sql /app/scripts/seed.sql

USER perso:perso

EXPOSE 8080

ENTRYPOINT ["/app/perso"]
CMD ["all"]
