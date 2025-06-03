FROM golang:1.23.8

WORKDIR /app
RUN mkdir data

COPY go.mod go.sum sync.go ./
COPY internal ./internal/
RUN go mod download
RUN go build -tags sync -o ./sync

# No CMD or ENTRYPOINT so the container is idle for test setup 
CMD ["tail", "-f", "/dev/null"] 