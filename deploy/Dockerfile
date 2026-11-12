# Stage 1: Build static grab executable
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source repository
COPY . .

# Build statically linked binary without CGO
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/grab ./cmd/grab

# Stage 2: Minimal runtime image
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S grab \
    && adduser -S grab -G grab

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/grab /usr/local/bin/grab

# Copy sample landing app and template
COPY examples/landing.grab ./landing.grab
COPY examples/landing.grab.html ./landing.grab.html

# Switch to unprivileged user
USER grab:grab

EXPOSE 8080

# Serve the application in production mode behind a reverse proxy
ENTRYPOINT ["/usr/local/bin/grab", "serve", "--addr", "0.0.0.0:8080", "--allow-public", "--trusted-proxy", "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.1", "--log-format", "json", "--diagnostics", "production"]
CMD ["landing.grab"]
