# Stage 1: Setup test data and generate necoshot.json
FROM golang:1.23.8-alpine AS snapshot-generator
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY integration_test/helpers/necoshot_generator.go ./necoshot_generator.go
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-w -s' -o ./necoshot_generator

# Create the sync directory with test data
RUN mkdir -p /sync
# Create sample files and directories for testing
# Exists on both:
# --- client_win_delete.md (DNE on Final)
# --- client_lose_delete.md (DNE on Final)
# Exists only on client:
# --- client_subdir/client_regular_upload.md (Exists on Final)
# --- client_empty_dir/  (Exists on Final)
RUN echo "Client wins LWW delete, delete on server" > /sync/client_win_delete.md && \
    echo "Client loses LWW delete, client redownloads" > /sync/client_lose_delete.md && \
    mkdir -p /sync/client_subdir && \
    echo "Client wins LWW, DNE on server, upload to server" > /sync/client_subdir/client_regular_upload.md && \
    mkdir -p /sync/client_empty_dir

# Notes for integration tests:
# client_win_delete.md on server Last modified is 2025-01-01 00:00:00
# client_lose_delete.md on server Last modified is 2025-01-01 00:00:01
# Forge Last modified file dates:
RUN touch -d "2025-01-01 00:00:01" /sync/client_win_delete.md && \
    touch -d "2025-01-01 00:00:00" /sync/client_lose_delete.md
# Generate the perfect necoshot.json using our helper
RUN ./necoshot_generator -dir /sync
RUN ls -la /sync/.neco/

# Stage 2: Build clientsync and pubsub with pre-existing snapshot
FROM golang:1.23.8-alpine AS sync-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY clientsync.go ./
COPY pubsub.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags clientsync -ldflags '-w -s' -o ./clientsync
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags pubsub -ldflags '-w -s' -o ./pubsub

# Stage 3: Final client image
FROM alpine:latest
WORKDIR /app
RUN mkdir -p /app/sync/.neco
COPY --from=snapshot-generator /sync ./sync
COPY --from=sync-builder /app/clientsync ./clientsync
COPY --from=sync-builder /app/pubsub ./pubsub
RUN ls -la /app/sync/.neco/

# Create entrypoint script that runs sync first, then pubsub
RUN echo '#!/bin/sh' > ./entrypoint.sh && \
    echo 'echo "Starting sync..."' >> ./entrypoint.sh && \
    echo './clientsync' >> ./entrypoint.sh && \
    echo 'echo "Initial sync completed, starting pubsub..."' >> ./entrypoint.sh && \
    echo './pubsub' >> ./entrypoint.sh && \
    chmod +x ./entrypoint.sh

CMD ["./entrypoint.sh"] 
