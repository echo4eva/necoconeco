# Stage 1: Go Builder for clientsync executable
FROM golang:1.23.8-alpine AS sync-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags clientsync -ldflags '-w -s' -o ./clientsync

# Stage 2: Go Builder for pubsub executable
FROM golang:1.23.8-alpine AS pubsub-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags pubsub -ldflags '-w -s' -o ./pubsub

# Stage 3: Final client image
FROM alpine:latest
WORKDIR /app
# Create sync directory and test files
RUN mkdir -p /app/sync

# Set up test files for different scenarios
# This will be customized per test case
RUN echo "initial content" > /app/sync/file.md
RUN echo "another file" > /app/sync/another.md

COPY --from=sync-builder /app/clientsync ./clientsync
COPY --from=pubsub-builder /app/pubsub ./pubsub

RUN echo '#!/bin/sh' > ./client_sync_entrypoint.sh && \
    echo 'echo "Starting initial sync..."' >> ./client_sync_entrypoint.sh && \
    echo './clientsync' >> ./client_sync_entrypoint.sh && \
    echo 'echo "Initial sync completed, starting pubsub..."' >> ./client_sync_entrypoint.sh && \
    echo './pubsub' >> ./client_sync_entrypoint.sh && \
    chmod +x ./client_sync_entrypoint.sh

# The container will run clientsync once and exit
CMD ["./client_sync_entrypoint.sh"] 