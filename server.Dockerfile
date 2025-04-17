FROM golang:1.23.5

WORKDIR /app
RUN mkdir data

COPY go.mod go.sum server.go ./
RUN go mod download
RUN go build -tags server -o ./server

CMD [ "./server" ]