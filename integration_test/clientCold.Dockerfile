FROM golang:1.23.8 AS go-builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY coldsync.go ./

# The golang:1.23.8 image (often Debian-based) uses glibc.
# The alpine:latest image uses musl libc, a different standard C library.
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags coldsync -ldflags '-w -s' -o ./coldsync

# Stage 2: Final client image
FROM alpine:latest
WORKDIR /app

# Copy only the compiled coldsync binary from the go-builder stage
COPY --from=go-builder /app/coldsync ./coldsync

# Create the directory needed by pubsub (e.g., for synced files or data)
RUN mkdir -p /app/sync

# Create initial files and directories for cold sync test in /app/sync
RUN mkdir -p /app/sync/folder && \
    echo "content for one.md" > /app/sync/one.md && \
    echo "content for two.md" > /app/sync/folder/two.md

COPY integration_test/cold_entrypoint.sh ./cold_entrypoint.sh
RUN chmod +x ./cold_entrypoint.sh
ENTRYPOINT [ "./cold_entrypoint.sh" ]
