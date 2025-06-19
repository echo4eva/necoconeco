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
RUN ./necoshot_generator -dir /sync
RUN ls -la /sync/.neco/

# Stage 2: Build clientsync and client with pre-existing snapshot
FROM golang:1.23.8-alpine AS sync-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY clientsync.go ./
COPY client.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags clientsync -ldflags '-w -s' -o ./clientsync
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags client -ldflags '-w -s' -o ./client

# Stage 3: Final client image
FROM alpine:latest
WORKDIR /app
RUN mkdir -p /app/sync/.neco
# Grab snapshot from snapshot-generator
COPY --from=snapshot-generator /sync/.neco ./sync/.neco
# Grab executables
COPY --from=sync-builder /app/clientsync ./clientsync
COPY --from=sync-builder /app/client ./client
RUN ls -la /app/sync/.neco/

# Create entrypoint script that runs sync first, then client
RUN echo '#!/bin/sh' > ./entrypoint.sh && \
    echo 'echo "Starting sync..."' >> ./entrypoint.sh && \
    echo './clientsync' >> ./entrypoint.sh && \
    echo 'echo "Initial sync completed, starting client..."' >> ./entrypoint.sh && \
    echo './client' >> ./entrypoint.sh && \
    chmod +x ./entrypoint.sh

CMD ["./entrypoint.sh"] 
