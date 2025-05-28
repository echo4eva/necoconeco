FROM golang:1.23.8

WORKDIR /app
RUN mkdir data

COPY go.mod go.sum pubsub.go ./
COPY internal ./internal/
RUN go mod download
RUN go build -tags pubsub -o ./pubsub

CMD [ "./pubsub" ]
