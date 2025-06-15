# Stage 1: Build the Go application
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /socket-proxy main.go

# Stage 2: Create the final image
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /socket-proxy .

EXPOSE 8080

CMD ["./socket-proxy", "--auto-connect=true"]