# Stage 1: Build the Go application
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /socket-proxy main.go

FROM alpine:latest

RUN addgroup -g 1000 appgroup && adduser -u 1000 -G appgroup -s /bin/sh -D appuser

WORKDIR /app

COPY --from=builder /socket-proxy /app/socket-proxy

RUN chmod +x /app/socket-proxy && chown appuser:appgroup /app/socket-proxy

RUN mkdir -p /app/recordings && chown appuser:appgroup /app/recordings

USER appuser

EXPOSE 8080

CMD ["./socket-proxy", "--auto-connect=true"]