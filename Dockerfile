# Multi-stage Dockerfile for Silmaril P2P AI Model Distribution

# Stage 1: Builder
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN make build

# Stage 2: Runtime
FROM alpine:latest

# Create non-root user
RUN addgroup -g 1000 silmaril && \
    adduser -D -u 1000 -G silmaril silmaril

# Create necessary directories
RUN mkdir -p /home/silmaril/.silmaril /home/silmaril/.config/silmaril && \
    chown -R silmaril:silmaril /home/silmaril

# Copy binary from builder
COPY --from=builder /build/build/silmaril /usr/local/bin/silmaril

# Copy config example
COPY --from=builder /build/config.yaml.example /home/silmaril/.config/silmaril/config.yaml.example

# Switch to non-root user
USER silmaril
WORKDIR /home/silmaril

# Expose ports
# 8737: REST API
# 6881: DHT/BitTorrent
EXPOSE 8737 6881/udp


# Default command
CMD ["silmaril", "daemon", "start"]