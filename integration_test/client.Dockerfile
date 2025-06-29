# Multi-stage Dockerfile for clientsync + client

# Stage 1: Build clientsync
FROM golang:1.23.8-alpine AS clientsync-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o clientsync ./clientsync.go

# Stage 2: Build client
FROM golang:1.23.8-alpine AS client-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o client ./client.go

# Stage 3: Final runtime image
FROM alpine:latest
WORKDIR /app

# Copy built executables from previous stages
COPY --from=clientsync-builder /app/clientsync /app/clientsync
COPY --from=client-builder /app/client /app/client

# Create sync directory
RUN mkdir -p /app/sync

# Create inline entrypoint script
RUN echo '#!/bin/sh' > /app/entrypoint.sh && \
    echo 'set -e' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo 'echo "Starting clientsync..."' >> /app/entrypoint.sh && \
    echo '/app/clientsync' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo 'echo "Starting client..."' >> /app/entrypoint.sh && \
    echo '/app/client' >> /app/entrypoint.sh

# Make entrypoint executable
RUN chmod +x /app/entrypoint.sh

# Set the entrypoint
ENTRYPOINT ["/app/entrypoint.sh"] 