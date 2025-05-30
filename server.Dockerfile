FROM golang:1.23.8

WORKDIR /app
RUN mkdir data

COPY go.mod go.sum server.go ./
COPY internal ./internal/
RUN go mod download
RUN go build -tags server -o ./server

CMD [ "./server" ]
