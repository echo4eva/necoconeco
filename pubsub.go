//go:build pubsub
// +build pubsub

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
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
	FileURL  string `json:"file_url"`
}

type FileStat struct {
	event      string
	fromSource bool
}

type ProcessedFiles struct {
	mutex sync.Mutex
	files map[string]FileStat
}

type EventData struct {
	path      string
	event     string
	timestamp time.Time
	message   *Message
}

type EventProcessor struct {
	// mutex      sync.Mutex
	// fileEvents map[string]EventData
	fileEvents chan EventData
}

var (
	clientID       string
	exchangeName   string
	queueName      string
	routingKey     string
	syncDirectory  string
	address        string
	serverURL      string
	processedFiles *ProcessedFiles
	eventProcessor *EventProcessor
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
	serverURL = os.Getenv("SYNC_SERVER_URL")
	processedFiles = NewProcessFiles()
	eventProcessor = NewEventProcessor()

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

	eventProcessor.processEvents(publisher, watcher)

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

				path := event.Name
				eventOperation := event.Op.String()

				payload := Message{
					ClientID: clientID,
					Event:    eventOperation,
					Path:     path,
				}

				eventProcessor.putEvent(event.Name, event.Op.String(), &payload)

				log.Println("event: ", event)

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error: ", err)
			}
		}
	}()
}

func upload(message *Message) error {
	processedFiles.mark(message.Path, message.Event, true)

	file, err := os.Open(message.Path)
	if err != nil {
		return fmt.Errorf("Failed to open file %w", err)
	}
	defer file.Close()

	var requestBody bytes.Buffer
	multiWriter := multipart.NewWriter(&requestBody)

	fields := map[string]string{
		"client_id": message.ClientID,
		"event":     message.Event,
		"path":      message.Path,
	}
	for field, value := range fields {
		err = multiWriter.WriteField(field, value)
		if err != nil {
			return fmt.Errorf("Failed to write field %s: %w", field, err)
		}
	}

	fileWriter, err := multiWriter.CreateFormFile("file", filepath.Base(message.Path))
	if err != nil {
		return fmt.Errorf("Failed to create form file %w:", err)
	}

	_, err = io.Copy(fileWriter, file)
	if err != nil {
		return fmt.Errorf("Failed to copy file content %w", err)
	}

	err = multiWriter.Close()
	if err != nil {
		return fmt.Errorf("Failed to close multipart writer %w", err)
	}

	uploadURL := fmt.Sprintf("http://%s/upload", serverURL)
	request, err := http.NewRequest("POST", uploadURL, &requestBody)
	if err != nil {
		return fmt.Errorf("Failed to create request %w", err)
	}

	request.Header.Set("Content-Type", multiWriter.FormDataContentType())

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("Failed to send request %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("Failed to read response %w", err)
	}

	fmt.Printf("Upload successful: %s\n", string(responseBody))

	type UploadResponse struct {
		Status  string `json:"status"`
		FileURL string `json:"file_url"`
	}

	uploadResponse := UploadResponse{}

	err = json.Unmarshal(responseBody, &uploadResponse)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal response %w", err)
	}

	message.FileURL = uploadResponse.FileURL

	return nil
}

func download(message Message) error {
	absolutePath := absoluteConvert(message.Path)
	processedFiles.mark(absolutePath, message.Event, false)

	// Create file
	out, err := os.Create(absolutePath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Use link to download
	resp, err := http.Get(message.FileURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Copy data into file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func publish(publisher *rmq.Publisher, message *Message) {
	message.Path = relativeConvert(message.Path)

	jsonData, err := json.Marshal(message)
	if err != nil {
		log.Fatal(err)
	}

	publishResult, err := publisher.Publish(context.Background(), rmq.NewMessage(jsonData))
	if err != nil {
		log.Fatal(err)
	}
	rmq.Info("[PUBLISHER] SENDING MESSAGE")

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

				var message Message
				json.Unmarshal(deliveryContext.Message().GetData(), &message)

				// For testing with docker containers
				if strings.Contains(message.FileURL, "localhost") && !strings.Contains(serverURL, "localhost") {
					message.FileURL = fmt.Sprintf("http://%s%s", serverURL, strings.TrimPrefix(message.FileURL, "http://localhost:8080"))
				}

				if message.ClientID == clientID {
					// Consumer discards message that it was sent from Broker
					err = deliveryContext.Discard(context.Background(), &amqp.Error{})
					rmq.Error("[CONSUMER] Discarded message %v", message)
					continue
				}

				rmq.Info(
					"[CONSUMER] received message",
					fmt.Sprintf("%+v", message),
				)
				absolutePath := absoluteConvert(message.Path)
				rmq.Info("[CONSUMER DEBUG] Rel to Abs: ", absolutePath)

				err = deliveryContext.Accept(context.Background())
				if err != nil {
					rmq.Error("[CONSUMER] Failed to accept message", err)
					continue
				}

				if message.Event == "CREATE" && strings.HasSuffix(message.Path, ".md") {
					rmq.Info("[CONSUMER] DOWNLOADING YAY")
					err := download(message)
					if err != nil {
						rmq.Error("[CONSUMER] Failed to download", err)
						continue
					}
				}
			}
		}
	}()
}

func NewEventProcessor() *EventProcessor {
	return &EventProcessor{
		fileEvents: make(chan EventData, 100),
	}
}

func (ep EventProcessor) putEvent(path string, event string, message *Message) {
	ep.fileEvents <- EventData{
		path:      path,
		event:     event,
		timestamp: time.Now(),
		message:   message,
	}
}

func (ep EventProcessor) processEvents(publisher *rmq.Publisher, watcher *fsnotify.Watcher) {
	go func() {
		for eventData := range ep.fileEvents {
			switch eventData.event {
			case "CREATE":
				if strings.HasSuffix(eventData.path, ".md") {
					if processedFiles.isProcessed(eventData.path) {
						log.Printf("File %s has already been processed for event %s", eventData.path, eventData.event)
						continue
					}
					err := upload(eventData.message)
					if err != nil {
						log.Println(err)
					}
					publish(publisher, eventData.message)
				} else if isDir(eventData.path) {
					watcher.Add(eventData.path)
					publish(publisher, eventData.message)
				}
			default:
				log.Printf("Unexpected event occured: %s", eventData.event)
			}
		}
	}()
}

func NewProcessFiles() *ProcessedFiles {
	return &ProcessedFiles{
		files: make(map[string]FileStat),
		mutex: sync.Mutex{},
	}
}

func (pf *ProcessedFiles) mark(path, event string, fromSource bool) {
	log.Printf("Marking file as processed: %s (event: %s)", path, event)
	pf.mutex.Lock()
	defer pf.mutex.Unlock()

	pf.files[path] = FileStat{
		event:      event,
		fromSource: fromSource,
	}
}

func (pf *ProcessedFiles) isProcessed(path string) bool {
	pf.mutex.Lock()
	defer pf.mutex.Unlock()

	_, exists := pf.files[path]
	return exists
}

func (pf ProcessedFiles) debugStatus(path string) {
	file := pf.files[path]
	fmt.Printf("[%s] %s : %t\n", path, file.event, file.fromSource)
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
