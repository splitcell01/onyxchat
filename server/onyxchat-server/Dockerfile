# ============================
# 1) Build stage
# ============================
FROM golang:1.25-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o server ./cmd/server

# ============================
# 2) Runtime stage
# ============================
FROM alpine:3.20

RUN apk add --no-cache netcat-openbsd wget ca-certificates postgresql-client

RUN adduser -D -g '' appuser \
    && mkdir -p /app/data \
    && chown -R appuser:appuser /app

WORKDIR /app
COPY --from=builder /app/server /app/server
COPY wait-for-db.sh /app/wait-for-db.sh
RUN sed -i 's/\r$//' /app/wait-for-db.sh && chmod +x /app/wait-for-db.sh

ENV SM_SERVER_ADDR=:8080
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://localhost:8080/health/ready >/dev/null 2>&1 || exit 1

USER appuser
ENTRYPOINT ["/app/wait-for-db.sh"]
CMD ["/app/server"]
