# Stage 1: Go Builder for sync executable
FROM golang:1.23.8 AS sync-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY sync.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags sync -ldflags '-w -s' -o ./sync

# Stage 2: Go Builder for pubsub executable  
FROM golang:1.23.8 AS pubsub-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY pubsub.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags pubsub -ldflags '-w -s' -o ./pubsub

# Stage 3: Final client image
FROM alpine:latest
WORKDIR /app

# Copy both executables from their respective builders
COPY --from=sync-builder /app/sync ./syncbin
COPY --from=pubsub-builder /app/pubsub ./pubsub

# Create the directory needed for synced files
RUN mkdir -p /app/sync

# Ensure both binaries are executable
RUN chmod +x ./syncbin ./pubsub

# Create entrypoint script that runs sync first, then pubsub
RUN echo '#!/bin/sh' > ./sync_pubsub_entrypoint.sh && \
    echo 'echo "Starting initial sync..."' >> ./sync_pubsub_entrypoint.sh && \
    echo './syncbin' >> ./sync_pubsub_entrypoint.sh && \
    echo 'echo "Initial sync completed, starting pubsub..."' >> ./sync_pubsub_entrypoint.sh && \
    echo './pubsub' >> ./sync_pubsub_entrypoint.sh && \
    chmod +x ./sync_pubsub_entrypoint.sh

# Set the entrypoint to run both sync and pubsub
CMD [ "./sync_pubsub_entrypoint.sh" ] 