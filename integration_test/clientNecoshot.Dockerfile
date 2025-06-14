# Stage 1: Setup test data and generate necoshot.json
FROM golang:1.23.8-alpine AS snapshot-generator

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY internal/ ./internal/
COPY integration_test/helpers/necoshot_generator.go ./necoshot_generator.go

# Build the necoshot helper
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-w -s' -o ./necoshot_generator

# Create the sync directory with test data
RUN mkdir -p /sync

# Create sample files and directories for testing
RUN echo "Client wins LWW delete, delete on server" > /sync/client_win_delete.md && \
    echo "Client loses LWW delete, client redownloads" > /sync/client_lose_delete.md && \
    mkdir -p /sync/subdir && \
    echo "Client wins LWW, DNE on server, upload to server" > /sync/subdir/client_regular_upload.md && \
    mkdir -p /sync/empty_dir

# Notes for integration tests:
# client_win_delete.md on server Last modified is 2025-01-01 00:00:00
# client_lose_delete.md on server Last modified is 2025-01-01 00:00:01
# Forge Last modified file dates:
RUN touch -d "2025-01-01 00:00:01" /sync/client_win_delete.md && \
    touch -d "2025-01-01 00:00:00" /sync/client_lose_delete.md

# Generate the perfect necoshot.json using our helper
RUN ./necoshot_generator -dir /sync

RUN ls -la /sync/.neco/

# Stage 2: Final client image with pre-existing snapshot
FROM golang:1.23.8-alpine AS sync-builder
# Set working directory
WORKDIR /app
# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download
# Copy source code
COPY internal/ ./internal/
COPY clientsync.go ./

# Build the clientsync executable
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags clientsync -ldflags '-w -s' -o ./clientsync

# Stage 3: Final client image
FROM alpine:latest
WORKDIR /app
RUN mkdir -p /app/sync/.neco

# Copy the test data and necoshot.json from the previous stage
COPY --from=snapshot-generator /sync/.neco/necoshot.json ./sync/.neco/necoshot.json
COPY --from=sync-builder /app/clientsync ./clientsync

# Verify the necoshot.json was copied correctly
RUN ls -la /app/sync/.neco/

# Default command
CMD ["./clientsync"] 