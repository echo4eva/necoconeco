# Stage 1: Go Builder for offline sync executable
FROM golang:1.23.8 AS offline-sync-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY offlinesync.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags offlinesync -ldflags '-w -s' -o ./offlinesync

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

# Copy only the compiled offlinesync binary from the go-builder stage
COPY --from=offline-sync-builder /app/offlinesync ./offlinesync
COPY --from=pubsub-builder /app/pubsub ./pubsub

# Create the sync directory and test files for offline sync testing
RUN mkdir -p /app/sync/nested/deep && \
    echo "This is a root level file for offline sync testing." > /app/sync/root_file.md && \
    echo "It contains multiple lines of content." >> /app/sync/root_file.md && \
    echo "# Middle File" > /app/sync/nested/middle_file.md && \
    echo "This is a markdown file." >> /app/sync/nested/middle_file.md && \
    echo "This is the most updated conflicting file." > /app/sync/conflict.md

# Create entrypoint script that runs sync first, then pubsub
RUN echo '#!/bin/sh' > ./offline_entrypoint.sh && \
    echo 'echo "Starting initial offline sync..."' >> ./offline_entrypoint.sh && \
    echo './offlinesync' >> ./offline_entrypoint.sh && \
    echo 'echo "Initial sync completed, starting pubsub..."' >> ./offline_entrypoint.sh && \
    echo './pubsub' >> ./offline_entrypoint.sh && \
    chmod +x ./offline_entrypoint.sh

# Set the entrypoint to run both sync and pubsub
CMD [ "./offline_entrypoint.sh" ]
