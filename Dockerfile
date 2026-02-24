# ── Build stage ───────────────────────────────────────────────
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o distribcache .

# ── Runtime stage ─────────────────────────────────────────────
FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/distribcache .
COPY --from=builder /app/frontend ./frontend

EXPOSE 8080
CMD ["./distribcache"]
