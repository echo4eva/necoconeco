package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Azure/go-amqp"
	"github.com/fsnotify/fsnotify"
	"github.com/joho/godotenv"
	rmq "github.com/rabbitmq/rabbitmq-amqp-go-client/pkg/rabbitmqamqp"
)

type Message struct {
	ClientID string `json:"client_id"`
	Event    string `json:"event"`
	Path     string `json:"path"`
	Data     string `json:"data"`
}

var (
	clientID      string
	exchangeName  string
	queueName     string
	routingKey    string
	syncDirectory string
	address       string
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	clientID = os.Getenv("CLIENT_ID")
	address = os.Getenv("RABBITMQ_ADDRESS")
	exchangeName = os.Getenv("RABBITMQ_EXCHANGE_NAME")
	queueName = os.Getenv("RABBITMQ_QUEUE_NAME")
	routingKey = os.Getenv("RABBITMQ_ROUTING_KEY")
	syncDirectory = os.Getenv("SYNC_DIRECTORY")

	// Setup RabbitMQ client
	env := rmq.NewEnvironment(address, nil)
	defer env.CloseConnections(context.Background())

	amqpConnection, err := env.NewConnection(context.Background())
	if err != nil {
		rmq.Error("Failed to create new connection")
		return
	}
	defer amqpConnection.Close(context.Background())

	management := amqpConnection.Management()
	defer management.Close(context.Background())

	_, err = management.DeclareExchange(context.Background(), &rmq.TopicExchangeSpecification{
		Name:         exchangeName,
		IsAutoDelete: false,
	})
	if err != nil {
		rmq.Error("Failed to declare exchange", err)
		return
	}

	_, err = management.DeclareQueue(context.Background(), &rmq.ClassicQueueSpecification{
		Name:         queueName,
		IsAutoDelete: false,
	})
	if err != nil {
		rmq.Error("Failed to declare queue", err)
		return
	}

	_, err = management.Bind(context.TODO(), &rmq.ExchangeToQueueBindingSpecification{
		SourceExchange:   exchangeName,
		DestinationQueue: queueName,
		BindingKey:       routingKey,
	})
	if err != nil {
		rmq.Error("Failed to bind queue to exchange", err)
		return
	}

	// Setup RabbitMQ publisher and publish message
	publisher, err := amqpConnection.NewPublisher(context.Background(), &rmq.ExchangeAddress{
		Exchange: exchangeName,
		Key:      routingKey,
	}, nil)
	if err != nil {
		rmq.Error("Failed to create new publisher", err)
		return
	}
	defer publisher.Close(context.Background())

	payload := Message{
		ClientID: clientID,
		Data:     "meow",
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("Failed to marshal payload into jsonData")
		return
	}

	publishResult, err := publisher.Publish(context.Background(), rmq.NewMessage(jsonData))
	if err != nil {
		rmq.Error("Failed to publish message", err)
		return
	}

	// ACKs and NACKs from broker to publisher
	switch publishResult.Outcome.(type) {
	case *rmq.StateAccepted:
		rmq.Info("[PUBLISHER] Message accepted", publishResult.Message.Data[0])
	case *rmq.StateRejected:
		rmq.Info("[PUBLISHER] Message rejected", publishResult.Message.Data[0])
	case *rmq.StateReleased:
		rmq.Info("[PUBLISHER] Message released", publishResult.Message.Data[0])
	}

	// Setup persistent RabbitMQ consumer
	consumer, err := amqpConnection.NewConsumer(context.Background(), queueName, nil)
	if err != nil {
		rmq.Error("Failed to create new consumer", err)
		return
	}

	consumerContext, consumerCancel := context.WithCancel(context.Background())
	defer consumerCancel()

	consume(consumer, consumerContext)

	// Setup filesystem watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	watch(watcher, publisher)

	// Print working directory for debugging
	currentDir, _ := os.Getwd()
	log.Printf("Current working directory: %s", currentDir)

	// Check if directory exists
	if _, err := os.Stat(syncDirectory); os.IsNotExist(err) {
		log.Fatalf("Sync directory does not exist: %s", syncDirectory)
	}

	// Add file system watchers at the root and in all subdirectories
	err = filepath.WalkDir(syncDirectory, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			err := watcher.Add(path)
			if err != nil {
				log.Fatal(path, err)
			}
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// Block subroutines until cancel
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigs
	rmq.Info("Received signal to shutdown", sig)

	time.Sleep(1 * time.Second)
}

func watch(watcher *fsnotify.Watcher, publisher *rmq.Publisher) {
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Create) {
					if isDir(event.Name) {
						watcher.Add(event.Name)
					}
				}
				log.Println("event: ", event)
				if event.Has(fsnotify.Write) {
					log.Println("modified file: ", event.Name)
				}
				payload := Message{
					ClientID: clientID,
					Event:    event.Op.String(),
					Path:     relativeConvert(event.Name),
					Data:     "testdata",
				}
				publish(publisher, payload)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error: ", err)
			}
		}
	}()
}

func publish(publisher *rmq.Publisher, message Message) {
	jsonData, err := json.Marshal(message)
	if err != nil {
		log.Fatal(err)
	}

	publishResult, err := publisher.Publish(context.Background(), rmq.NewMessage(jsonData))
	if err != nil {
		log.Fatal(err)
	}

	// ACKs and NACKs from broker to publisher
	switch publishResult.Outcome.(type) {
	case *rmq.StateAccepted:
		rmq.Info("[PUBLISHER] Message accepted", publishResult.Message.Data[0])
	case *rmq.StateRejected:
		rmq.Info("[PUBLISHER] Message rejected", publishResult.Message.Data[0])
	case *rmq.StateReleased:
		rmq.Info("[PUBLISHER] Message released", publishResult.Message.Data[0])
	}
}

func consume(consumer *rmq.Consumer, ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				rmq.Info("[CONSUMER] Done Listening")
				return
			default:
				deliveryContext, err := consumer.Receive(ctx)

				if errors.Is(err, context.Canceled) {
					rmq.Info("[CONSUMER] Consumer closed context", err)
					return
				}
				if err != nil {
					rmq.Info("[CONSUMER] Failed to receive message", err)
					continue
				}

				var receivedData Message
				json.Unmarshal(deliveryContext.Message().GetData(), &receivedData)

				rmq.Info("[CONSUMER] Rel to Abs: ", absoluteConvert(receivedData.Path))

				if receivedData.ClientID == clientID {
					// Consumer discards message that it was sent from Broker
					err = deliveryContext.Discard(context.Background(), &amqp.Error{})
					rmq.Error("[CONSUMER] Discarded message", err)
					continue
				}

				rmq.Info(
					"[CONSUMER] received message",
					fmt.Sprintf("%+v", receivedData),
				)

				err = deliveryContext.Accept(context.Background())
				if err != nil {
					rmq.Error("[CONSUMER] Failed to accept message", err)
					continue
				}
			}
		}
	}()
}

func isDir(directory string) bool {
	if stat, err := os.Stat(directory); err == nil && stat.IsDir() {
		return true
	}
	return false
}

func relativeConvert(absolutePath string) string {
	return strings.TrimPrefix(absolutePath, syncDirectory)
}

func absoluteConvert(relativePath string) string {
	return syncDirectory + relativePath
}
