FROM golang:1.23.8

WORKDIR /app
RUN mkdir data

COPY go.mod go.sum sync.go ./
COPY internal ./internal/
RUN go mod download
RUN go build -tags sync -o ./sync

# Test setup: create files and directories for sync test
RUN mkdir -p /app/onClient \
 && touch /app/same.md \
 && touch /app/different.md \
 && echo 'delete me' > /app/onClient/toBeDeleted.md

COPY integration_test/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"] 