# Stage 1: Go Builder (Common Go build logic)
FROM golang:1.23.8 AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY server.go ./
# The golang:1.23.8 image (often Debian-based) uses glibc.
# The alpine:latest image uses musl libc, a different standard C library.
RUN CGO_ENABLED=0 GOOS=linux go build -a -tags server -ldflags '-w -s' -o ./server

FROM alpine:latest
WORKDIR /app
RUN mkdir -p ./storage
COPY --from=go-builder /app/server ./server
RUN chmod +x ./server
CMD [ "./server" ]
