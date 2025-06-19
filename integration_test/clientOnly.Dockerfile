# Stage 1: Go Builder (Common Go build logic)
FROM golang:1.23.8-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY client.go ./
RUN go build -a -tags client -ldflags '-w -s' -o ./client

# Stage 2: Final client image
FROM alpine:latest
WORKDIR /app
# Copy only the compiled client binary from the go-builder stage
COPY --from=go-builder /app/client ./client
# Create the directory needed by client (e.g., for synced files or data)
RUN mkdir -p /app/sync
# Ensure the binary is executable
RUN chmod +x ./client
# Set the entrypoint to the client binary
CMD [ "./client" ]
