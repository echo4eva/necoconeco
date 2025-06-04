# Stage 1: Go Builder (Common Go build logic)
FROM golang:1.23.8 AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY pubsub.go ./
# The golang:1.23.8 image (often Debian-based) uses glibc.
# The alpine:latest image uses musl libc, a different standard C library.
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags pubsub -ldflags '-w -s' -o ./pubsub

# Stage 2: Final pubsub client image
FROM alpine:latest
WORKDIR /app
# Copy only the compiled pubsub binary from the go-builder stage
COPY --from=go-builder /app/pubsub ./pubsub
# Create the directory needed by pubsub (e.g., for synced files or data)
RUN mkdir -p /app/sync
# Ensure the binary is executable
RUN chmod +x ./pubsub
# Set the entrypoint
CMD [ "./pubsub" ]
