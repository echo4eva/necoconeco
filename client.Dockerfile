FROM golang:1.23.5

WORKDIR /app
RUN mkdir data

COPY go.mod go.sum pubsub.go ./
RUN go mod download
RUN go build -tags pubsub -o ./pubsub

CMD [ "./pubsub" ]