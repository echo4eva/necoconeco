FROM golang:1.23.8

WORKDIR /app
RUN mkdir sync

COPY go.mod go.sum coldsync.go ./
COPY internal ./internal/
RUN go mod download
RUN go build -tags coldsync -o ./coldsync

# Create initial files and directories for cold sync test
RUN mkdir -p /sync/folder && \
    echo "content for one.md" > /sync/one.md && \
    echo "content for two.md" > /sync/folder/two.md

# Copy the new entrypoint script
COPY integration_test/cold_entrypoint.sh /cold_entrypoint.sh
RUN chmod +x /cold_entrypoint.sh

ENTRYPOINT ["/cold_entrypoint.sh"]
