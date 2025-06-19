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
    echo 'echo "Starting necoconeco sync services..."' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo '# Start pubsub in background' >> /app/entrypoint.sh && \
    echo 'echo "Starting client service..."' >> /app/entrypoint.sh && \
    echo '/app/client &' >> /app/entrypoint.sh && \
    echo 'CLIENT_PID=$!' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo '# Start clientsync in background' >> /app/entrypoint.sh && \
    echo 'echo "Starting clientsync service..."' >> /app/entrypoint.sh && \
    echo '/app/clientsync &' >> /app/entrypoint.sh && \
    echo 'CLIENTSYNC_PID=$!' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo '# Function to handle shutdown' >> /app/entrypoint.sh && \
    echo 'cleanup() {' >> /app/entrypoint.sh && \
    echo '    echo "Shutting down services..."' >> /app/entrypoint.sh && \
    echo '    kill $PUBSUB_PID $CLIENTSYNC_PID 2>/dev/null || true' >> /app/entrypoint.sh && \
    echo '    wait' >> /app/entrypoint.sh && \
    echo '    echo "Services stopped."' >> /app/entrypoint.sh && \
    echo '    exit 0' >> /app/entrypoint.sh && \
    echo '}' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo '# Trap signals for graceful shutdown' >> /app/entrypoint.sh && \
    echo 'trap cleanup SIGTERM SIGINT' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo 'echo "Both services started successfully."' >> /app/entrypoint.sh && \
    echo 'echo "Client PID: $PUBSUB_PID"' >> /app/entrypoint.sh && \
    echo 'echo "ClientSync PID: $CLIENTSYNC_PID"' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo '# Wait for either process to exit' >> /app/entrypoint.sh && \
    echo 'wait $PUBSUB_PID $CLIENTSYNC_PID' >> /app/entrypoint.sh

# Make entrypoint executable
RUN chmod +x /app/entrypoint.sh

# Expose any ports if needed (adjust as necessary)
EXPOSE 8080

# Set the entrypoint
ENTRYPOINT ["/app/entrypoint.sh"] 