FROM golang:1.23.8 AS go-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY sync.go ./

# The golang:1.23.8 image (often Debian-based) uses glibc.
# The alpine:latest image uses musl libc, a different standard C library.
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags sync -ldflags '-w -s' -o ./sync

# Stage 2: Final client image
FROM alpine:latest
WORKDIR /app
# Copy only the compiled coldsync binary from the go-builder stage
COPY --from=go-builder /app/sync ./syncbin

# Create the directory needed by pubsub (e.g., for synced files or data)
RUN mkdir -p /app/sync

# Test setup: create files and directories for sync test in /app/sync
RUN mkdir -p /app/sync/onClient && \
    touch /app/sync/same.md && \
    touch /app/sync/different.md && \
    echo 'delete me' > /app/sync/onClient/toBeDeleted.md

COPY integration_test/entrypoint.sh ./entrypoint.sh
RUN chmod +x ./entrypoint.sh
ENTRYPOINT [ "./entrypoint.sh" ]