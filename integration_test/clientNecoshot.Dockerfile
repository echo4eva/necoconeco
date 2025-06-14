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
RUN echo "This is a test file 1" > /sync/test1.md && \
    echo "This is a test file 2" > /sync/test2.md && \
    mkdir -p /sync/subdir && \
    echo "This is a nested file" > /sync/subdir/nested.md && \
    mkdir -p /sync/empty_dir && \
    echo "Another test file" > /sync/another.md

RUN touch -d "2025-01-01 00:00:00" /sync/test1.md && \
    touch -d "2025-01-01 00:00:00" /sync/test2.md && \
    touch -d "2025-01-01 00:00:00" /sync/subdir/nested.md && \
    touch -d "2025-01-01 00:00:00" /sync/another.md

# Generate the perfect necoshot.json using our helper
RUN ./necoshot_generator -dir /sync

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

# Copy the test data and necoshot.json from the previous stage
COPY --from=snapshot-generator /app/sync/.neco/necoshot.json /app/sync/.neco/necoshot.json
COPY --from=sync-builder /app/clientsync ./clientsync

# Verify the necoshot.json was copied correctly
RUN ls -la /app/sync/.neco/

# Default command
CMD ["./clientsync"] 