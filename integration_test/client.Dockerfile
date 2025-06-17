# Multi-stage Dockerfile for clientsync + pubsub

# Stage 1: Build clientsync
FROM golang:1.23.8-alpine AS clientsync-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o clientsync ./clientsync.go

# Stage 2: Build pubsub  
FROM golang:1.23.8-alpine AS pubsub-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o pubsub ./pubsub.go

# Stage 3: Final runtime image
FROM alpine:latest
RUN apk --no-cache add ca-certificates bash
WORKDIR /app

# Copy built executables from previous stages
COPY --from=clientsync-builder /app/clientsync /app/clientsync
COPY --from=pubsub-builder /app/pubsub /app/pubsub

# Create sync directory
RUN mkdir -p /app/sync

# Create inline entrypoint script
RUN cat > /app/entrypoint.sh << 'EOF'
#!/bin/bash
set -e

echo "Starting necoconeco sync services..."

# Start pubsub in background
echo "Starting pubsub service..."
/app/pubsub &
PUBSUB_PID=$!

# Start clientsync in background  
echo "Starting clientsync service..."
/app/clientsync &
CLIENTSYNC_PID=$!

# Function to handle shutdown
cleanup() {
    echo "Shutting down services..."
    kill $PUBSUB_PID $CLIENTSYNC_PID 2>/dev/null || true
    wait
    echo "Services stopped."
    exit 0
}

# Trap signals for graceful shutdown
trap cleanup SIGTERM SIGINT

echo "Both services started successfully."
echo "PubSub PID: $PUBSUB_PID"
echo "ClientSync PID: $CLIENTSYNC_PID"

# Wait for either process to exit
wait $PUBSUB_PID $CLIENTSYNC_PID
EOF

# Make entrypoint executable
RUN chmod +x /app/entrypoint.sh

# Expose any ports if needed (adjust as necessary)
EXPOSE 8080

# Set the entrypoint
ENTRYPOINT ["/app/entrypoint.sh"] 