//go:build client
// +build client

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
	"sync"
	"syscall"
	"time"

	"github.com/Azure/go-amqp"
	"github.com/echo4eva/necoconeco/internal/api"
	"github.com/echo4eva/necoconeco/internal/utils"
	"github.com/fsnotify/fsnotify"
	"github.com/joho/godotenv"
	rmq "github.com/rabbitmq/rabbitmq-amqp-go-client/pkg/rabbitmqamqp"
)

type Message struct {
	ClientID string `json:"client_id"`
	Event    string `json:"event"`
	Path     string `json:"path"`
	FileURL  string `json:"file_url"`
	OldPath  string `json:"old_path"`
}

type FileStat struct {
	event        string
	fromSource   bool
	isWriteReady bool
}

type ProcessedFiles struct {
	mutex sync.Mutex
	files map[string]FileStat
}

type IgnoreEvents struct {
	mutex    sync.Mutex
	timers   map[string]*time.Timer
	duration time.Duration
}

type WriteDebouncer struct {
	mutex    sync.Mutex
	files    map[string]EventData
	timers   map[string]*time.Timer
	duration time.Duration
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
	writeDebouncer *WriteDebouncer
	ignoreEvents   *IgnoreEvents
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
	writeDebouncer = NewWriteDebouncer()
	ignoreEvents = NewIgnoreEvents()

	// Debug
	log.Printf("CLIENT ID: %s", clientID)
	log.Printf("ADDRESS: %s", address)
	log.Printf("EXCHANGE NAME: %s", exchangeName)
	log.Printf("QUEUE NAME: %s", queueName)
	log.Printf("ROUTING KEY: %s", routingKey)
	log.Printf("SYNC DIRECTORY: %s", syncDirectory)
	log.Printf("SERVER URL: %s", serverURL)

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

	// Assume that the queue exists already
	// Second measure, just incase the client running this did a sync.
	// If sync, the file server put messages in their queue that are redundant.
	purgedAmount, err := management.PurgeQueue(context.Background(), queueName)
	if err != nil {
		log.Printf("[PURGE]-[ERROR] %s\n", err)
		return
	}
	log.Printf("PURGING %d\n", purgedAmount)

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

	// Setup filesystem watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	consume(consumer, consumerContext, watcher)
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
				eventString := event.String()

				message := Message{
					ClientID: clientID,
					Event:    event.Op.String(),
					Path:     event.Name,
				}

				if ignoreEvents.isIgnored(path) {
					log.Printf("[WATCH]-[IGNORE EVENTS] Detected ignored, %s, for %s\n", path, eventOperation)
					continue
				}

				switch event.Op {
				case fsnotify.Write:
					if strings.HasSuffix(path, "workspace.json") {
						continue
					}
					log.Printf("[WATCH] Putting in write debouncer\n")
					writeDebouncer.putWrite(path, &message)
				case fsnotify.Create:
					// if Create event was prompted from Rename event
					if hasOldPath(eventString) {
						log.Printf("[WATCH] -CREATE- has old path %s\n", eventString)
						oldPath := getOldPath(eventString)

						renameEvent := "NECO_RENAME"

						message.Event = renameEvent
						message.OldPath = oldPath

						eventOperation = renameEvent
					}
					eventProcessor.putEvent(path, eventOperation, &message)
				case fsnotify.Rename:
					if writeDebouncer.writeExists(path) {
						writeDebouncer.deleteWrite(path)
					}
				case fsnotify.Remove:
					if writeDebouncer.writeExists(path) {
						writeDebouncer.deleteWrite(path)
					}
					eventProcessor.putEvent(path, eventOperation, &message)
				default:
					eventProcessor.putEvent(path, eventOperation, &message)
				}

				log.Printf("%s\n", eventString)

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
	log.Printf("[UPLOAD] Marking true")

	fields := map[string]string{
		"client_id": message.ClientID,
		"event":     message.Event,
		"path":      utils.AbsToRelConvert(syncDirectory, message.Path),
	}

	// Sending relative path, target file's path to upload, and serverURL to send req to
	// `uploadResponse` comes unmarshalled already
	uploadResponse, err := api.Upload(fields, message.Path, serverURL)
	if err != nil {
		log.Printf("[UPLOAD] Error: %s\n", err)
	}

	message.FileURL = uploadResponse.FileURL
	return nil
}

func download(message Message) error {
	// Download has ABSOLUTE PATH REMEMBER THIS
	absolutePath := utils.RelToAbsConvert(syncDirectory, message.Path)
	// Works for create events but not for consuming write evensts
	// processedFiles.mark(absolutePath, message.Event, false, false)
	// defer processedFiles.unmark(absolutePath)
	ignoreEvents.putIgnore(absolutePath)
	log.Printf("[DOWNLOAD] Marked false")

	err := api.Download(message.Path, syncDirectory, serverURL)
	if err != nil {
		return err
	}

	log.Printf("[DOWNLOAD] File populated")

	return nil
}

func publish(publisher *rmq.Publisher, message *Message) {
	message.Path = utils.AbsToRelConvert(syncDirectory, message.Path)
	message.OldPath = utils.AbsToRelConvert(syncDirectory, message.OldPath)
	if strings.Contains(message.Path, "\\") {
		message.Path = uncursing(message.Path)
	}

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

func consume(consumer *rmq.Consumer, ctx context.Context, watcher *fsnotify.Watcher) {
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
					rmq.Info("[CONSUMER] Failed tto receive message", err)
					continue
				}

				var message Message
				json.Unmarshal(deliveryContext.Message().GetData(), &message)
				if strings.Contains(message.Path, "/") && strings.Contains(syncDirectory, "\\") {
					message.Path = cursing(message.Path)
				}

				// For testing with docker containers
				log.Printf("[DEBUG] FileURL: %s serverURL: %s", message.FileURL, serverURL)
				if strings.Contains(message.FileURL, "localhost") && !strings.Contains(serverURL, "localhost") {
					message.FileURL = fmt.Sprintf("http://%s%s", serverURL, strings.TrimPrefix(message.FileURL, "http://localhost:8080"))
				} else if strings.Contains(message.FileURL, "file-server") && strings.Contains(serverURL, "localhost") {
					message.FileURL = fmt.Sprintf("http://%s%s", serverURL, strings.TrimPrefix(message.FileURL, "http://file-server:8080"))
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
				absolutePath := utils.RelToAbsConvert(syncDirectory, message.Path)
				rmq.Info("[CONSUMER DEBUG] Rel to Abs: ", absolutePath)

				err = deliveryContext.Accept(context.Background())
				if err != nil {
					rmq.Error("[CONSUMER] Failed to accept message", err)
					continue
				}

				switch message.Event {
				case "CREATE":
					if strings.HasSuffix(message.Path, ".md") {
						rmq.Info("[CONSUMER] CREATE DOWNLOADING")
						err := download(message)
						if err != nil {
							rmq.Error("[CONSUMER] Failed to download create", err)
							continue
						}
						// Consuming directory create events
					} else {
						absolutePath := utils.RelToAbsConvert(syncDirectory, message.Path)
						rmq.Info("[CONSUMER] CREATING DIRECTORY", message.Path)
						ignoreEvents.putIgnore(absolutePath)
						err := utils.MkDir(absolutePath)
						if err != nil {
							rmq.Error("[CONSUMER] Faield to create directory", err)
						}
						watcher.Add(absolutePath)
					}
				case "WRITE":
					rmq.Info("[CONSUMER] WRITE DOWNLOADING")
					err := download(message)
					if err != nil {
						rmq.Error("[CONSUMER] Failed to download write", err)
						continue
					}
				case "NECO_RENAME":
					absoluteOldPath := utils.RelToAbsConvert(syncDirectory, message.OldPath)
					absoluteNewPath := utils.RelToAbsConvert(syncDirectory, message.Path)

					rmq.Info("[CONSUMER] -NECO_RENAME-")
					ignoreEvents.putIgnore(absoluteOldPath)
					ignoreEvents.putIgnore(absoluteNewPath)
					err := utils.Rename(absoluteOldPath, absoluteNewPath)
					if err != nil {
						rmq.Error("[CONSUMER] -NECO_REMAKE- ", err)
					}
					if utils.IsDir(absoluteOldPath) {
						watcher.Add(absoluteNewPath)
					}
				case "REMOVE":
					rmq.Info("[CONSUMER] -REMOVE-")
					absolutePath := utils.RelToAbsConvert(syncDirectory, message.Path)
					ignoreEvents.putIgnore(absolutePath)
					err := utils.Rm(absolutePath)
					if err != nil {
						rmq.Error("[CONSUMER] -REMOVE- Failed to remove", err)
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
					// publish(publisher, eventData.message)
				} else if utils.IsDir(eventData.path) {
					watcher.Add(eventData.path)
					api.RemoteMkdir(eventData.message.Path, syncDirectory, serverURL, clientID)
					if processedFiles.isProcessed(eventData.path) {
						log.Printf("File %s has already been processed for event %s", eventData.path, eventData.event)
						continue
					}
					// publish(publisher, eventData.message)
				}
			case "WRITE":
				if strings.HasSuffix(eventData.path, ".md") {
					if !ignoreEvents.isIgnored(eventData.path) {
						log.Printf("[EVENT PROCESSOR] -WRITE- is write ready and uploading\n")
						err := upload(eventData.message)
						if err != nil {
							log.Println(err)
						}
						log.Printf("[EVENT PROCESSOR] -WRITE- publishing\n")
						publish(publisher, eventData.message)
					} else {
						log.Printf("[EVENT PROCESSOR]-[IGNORE EVENTS] -WRITE- is still being ignored")
					}
				}
			case "RENAME":
				log.Printf("[EVENT PROCESSOR] -RENAME- processed, debug")
			case "NECO_RENAME":
				log.Printf("[EVENT PROCESSOR] -NECO_RENAME- processed")
				if utils.IsDir(eventData.path) {
					watcher.Add(eventData.path)
				}
				err := api.RemoteRename(eventData.message.OldPath, eventData.message.Path, syncDirectory, serverURL, clientID)
				if err != nil {
					log.Println(err)
				}
				// publish(publisher, eventData.message)
			case "REMOVE":
				if processedFiles.isProcessed(eventData.path) {
					log.Printf("File %s has already been processed for event %s", eventData.path, eventData.event)
					continue
				}
				log.Printf("[EVENT PROCESSOR] -REMOVE- removing")
				err := api.RemoteRemove(eventData.path, syncDirectory, serverURL, clientID)
				if err != nil {
					log.Println(err)
				}
				// publish(publisher, eventData.message)
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

func (pf *ProcessedFiles) mark(path, event string, fromSource bool, isWriteReady bool) {
	log.Printf("Marking file as processed: %s (event: %s) (isWriteReady: %s)", path, event, isWriteReady)
	pf.mutex.Lock()

	pf.files[path] = FileStat{
		event:        event,
		fromSource:   fromSource,
		isWriteReady: isWriteReady,
	}

	pf.mutex.Unlock()

	pf.debugStatus(path)
}

func (pf *ProcessedFiles) unmark(path string) {
	log.Printf("[PROCESSED FILES] UNMARKING: %s", path)
	pf.mutex.Lock()
	defer pf.mutex.Unlock()

	delete(pf.files, path)
}

func (pf *ProcessedFiles) isRenamed(path string) bool {
	pf.mutex.Lock()
	defer pf.mutex.Unlock()

	stat, _ := pf.files[path]
	return stat.event == "RENAME"
}

func (pf *ProcessedFiles) isProcessed(path string) bool {
	pf.mutex.Lock()
	defer pf.mutex.Unlock()

	_, exists := pf.files[path]
	return exists
}

func (pf *ProcessedFiles) isWriteReady(path string) bool {
	pf.mutex.Lock()
	defer pf.mutex.Unlock()

	stat, _ := pf.files[path]
	return stat.isWriteReady
}

func (pf ProcessedFiles) debugStatus(path string) {
	file := pf.files[path]
	fmt.Printf("[%s] %s : %t : %t \n", path, file.event, file.fromSource, file.isWriteReady)
}

func NewWriteDebouncer() *WriteDebouncer {
	return &WriteDebouncer{
		mutex:    sync.Mutex{},
		files:    make(map[string]EventData),
		timers:   make(map[string]*time.Timer),
		duration: time.Second * 10,
	}
}

func (wd WriteDebouncer) putWrite(path string, message *Message) {
	wd.mutex.Lock()
	defer wd.mutex.Unlock()

	if _, exists := wd.timers[path]; !exists {
		// Timer that has a callback function when duration elapsed
		newTimer := time.AfterFunc(wd.duration, func() {
			wd.mutex.Lock()

			finalEvent, okEvent := wd.files[path]
			_, okTimer := wd.timers[path]

			if okEvent && okTimer {
				delete(wd.files, path)
				delete(wd.timers, path)

				wd.mutex.Unlock()

				log.Printf("[WRITE DEBOUNCER] Putting in event processor\n")
				eventProcessor.putEvent(finalEvent.path, finalEvent.event, finalEvent.message)
			} else {
				wd.mutex.Unlock()
				log.Printf("[WRITE DEBOUNCER] Debounce fired for %s but state removed", path)
			}
		})
		wd.timers[path] = newTimer
	} else {
		wd.timers[path].Reset(wd.duration)
	}

	wd.files[path] = EventData{
		path:      path,
		event:     "WRITE",
		timestamp: time.Now(),
		message:   message,
	}
}

func (wd *WriteDebouncer) deleteWrite(path string) {
	wd.mutex.Lock()
	defer wd.mutex.Unlock()

	if timer, ok := wd.timers[path]; ok {
		timer.Stop()
		delete(wd.timers, path)
		log.Printf("[DEBUG] Timer deleted for %s", path)
	}

	delete(wd.files, path)
}

func (wd WriteDebouncer) writeExists(path string) bool {
	wd.mutex.Lock()
	defer wd.mutex.Unlock()

	_, exists := wd.timers[path]

	log.Printf("[WRITE DEBOUNCER] %s, %t", path, exists)
	return exists
}

func NewIgnoreEvents() *IgnoreEvents {
	return &IgnoreEvents{
		mutex:    sync.Mutex{},
		timers:   make(map[string]*time.Timer),
		duration: time.Duration(time.Second * 1),
	}
}

func (ie IgnoreEvents) isIgnored(path string) bool {
	ie.mutex.Lock()
	defer ie.mutex.Unlock()

	_, exists := ie.timers[path]
	return exists
}

func (ie IgnoreEvents) putIgnore(path string) {
	ie.mutex.Lock()
	defer ie.mutex.Unlock()

	if _, exists := ie.timers[path]; !exists {
		log.Printf("[IGNORE EVENTS] - Putting Ignore on %s\n", path)
		newTimer := time.AfterFunc(ie.duration, func() {
			ie.mutex.Lock()
			defer ie.mutex.Unlock()

			log.Printf("[IGNORE EVENTS] - Remove Ignore %s\n", path)
			delete(ie.timers, path)
		})
		ie.timers[path] = newTimer
	}
}

// This is to detect the event of renaming in fsnotify event logs
func hasOldPath(eventString string) bool {
	return strings.Contains(eventString, "←")
}

// This is to extract the old name, since the old name is private
func getOldPath(eventString string) string {
	_, oldHalf, _ := strings.Cut(eventString, "←")
	firstQuote := strings.Index(oldHalf, "\"")
	lastQuote := strings.LastIndex(oldHalf, "\"")

	return oldHalf[firstQuote+1 : lastQuote]
}

// Windows shit, DONT TOUCH EWW WHAT TEH FUUUCK
func uncursing(windowsPath string) string {
	linuxPath := strings.ReplaceAll(windowsPath, "\\", "/")

	return linuxPath
}

func cursing(linuxPath string) string {
	windowsPath := strings.ReplaceAll(linuxPath, "/", "\\")

	return windowsPath
}
