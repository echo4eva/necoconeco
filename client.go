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
	"github.com/echo4eva/necoconeco/internal/config"
	"github.com/echo4eva/necoconeco/internal/utils"
	"github.com/fsnotify/fsnotify"
	rmq "github.com/rabbitmq/rabbitmq-amqp-go-client/pkg/rabbitmqamqp"
)

type Client struct {
	config      *config.Config
	apiClient   *api.API
	fileManager *utils.FileManager
}

// Message struct moved to internal/api package
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
	oldPath   string
	event     string
	timestamp time.Time
}

type EventProcessor struct {
	fileEvents chan EventData
}

var (
	client         *Client
	processedFiles *ProcessedFiles
	eventProcessor *EventProcessor
	writeDebouncer *WriteDebouncer
	ignoreEvents   *IgnoreEvents
)

func main() {
	client = &Client{}

	processedFiles = NewProcessFiles()
	eventProcessor = NewEventProcessor()
	writeDebouncer = NewWriteDebouncer()
	ignoreEvents = NewIgnoreEvents()

	config, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %s\n", err)
	}
	client.config = config
	client.apiClient = api.NewAPI(client.config.SyncServerURL, client.config.SyncDirectory)
	client.fileManager = utils.NewFileManager(client.config.SyncDirectory)

	// Setup RabbitMQ client
	env := rmq.NewEnvironment(client.config.RabbitMQAddress, nil)
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
		Name:         client.config.RabbitMQExchangeName,
		IsAutoDelete: false,
	})
	if err != nil {
		rmq.Error("Failed to declare exchange", err)
		return
	}

	_, err = management.DeclareQueue(context.Background(), &rmq.ClassicQueueSpecification{
		Name:         client.config.RabbitMQQueueName,
		IsAutoDelete: false,
	})
	if err != nil {
		rmq.Error("Failed to declare queue", err)
		return
	}

	// Assume that the queue exists already
	// Second measure, just incase the client running this did a sync.
	// If sync, the file server put messages in their queue that are redundant.
	purgedAmount, err := management.PurgeQueue(context.Background(), client.config.RabbitMQQueueName)
	if err != nil {
		log.Printf("[PURGE]-[ERROR] %s\n", err)
		return
	}
	log.Printf("PURGING %d\n", purgedAmount)

	_, err = management.Bind(context.TODO(), &rmq.ExchangeToQueueBindingSpecification{
		SourceExchange:   client.config.RabbitMQExchangeName,
		DestinationQueue: client.config.RabbitMQQueueName,
		BindingKey:       client.config.RabbitMQRoutingKey,
	})
	if err != nil {
		rmq.Error("Failed to bind queue to exchange", err)
		return
	}

	// Setup persistent RabbitMQ consumer
	consumer, err := amqpConnection.NewConsumer(context.Background(), client.config.RabbitMQQueueName, nil)
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
	watch(watcher)

	// Print working directory for debugging
	currentDir, _ := os.Getwd()
	log.Printf("Current working directory: %s", currentDir)

	// Check if directory exists
	if _, err := os.Stat(client.config.SyncDirectory); os.IsNotExist(err) {
		log.Fatalf("Sync directory does not exist: %s", client.config.SyncDirectory)
	}

	// Add file system watchers at the root and in all subdirectories
	err = filepath.WalkDir(client.config.SyncDirectory, func(path string, d fs.DirEntry, err error) error {
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

	eventProcessor.processEvents(watcher)

	// Block subroutines until cancel
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigs
	rmq.Info("Received signal to shutdown", sig)

	time.Sleep(1 * time.Second)
}

func watch(watcher *fsnotify.Watcher) {
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				path := event.Name
				oldPath := ""
				eventOperation := event.Op.String()
				eventString := event.String()

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
					writeDebouncer.putWrite(path)
				case fsnotify.Create:
					// if Create event was prompted from Rename event
					if hasOldPath(eventString) {
						log.Printf("[WATCH] -CREATE- has old path %s\n", eventString)
						oldPath = getOldPath(eventString)
						renameEvent := "NECO_RENAME"
						eventOperation = renameEvent
					}
					eventProcessor.putEvent(path, oldPath, eventOperation)
				case fsnotify.Rename:
					if writeDebouncer.writeExists(path) {
						writeDebouncer.deleteWrite(path)
					}
				case fsnotify.Remove:
					if writeDebouncer.writeExists(path) {
						writeDebouncer.deleteWrite(path)
					}
					eventProcessor.putEvent(path, oldPath, eventOperation)
				default:
					eventProcessor.putEvent(path, oldPath, eventOperation)
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

func upload(denormalizedPath string) error {
	log.Printf("[UPLOAD] Marking true")

	uploadResponse, err := client.apiClient.Upload(denormalizedPath, client.config.ClientID)
	if err != nil {
		log.Printf("[UPLOAD] Error: %s\n", err)
	}

	log.Printf("[UPLOAD] Upload response: %+v", uploadResponse)

	return nil
}

func download(denormalizedPath string) error {
	ignoreEvents.putIgnore(denormalizedPath)
	log.Printf("[DOWNLOAD] Marked false")

	normalizedPath := utils.AbsToRelConvert(client.config.SyncDirectory, denormalizedPath)
	err := client.apiClient.Download(normalizedPath)
	if err != nil {
		return err
	}

	log.Printf("[DOWNLOAD] File populated")

	return nil
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

				var message api.Message
				json.Unmarshal(deliveryContext.Message().GetData(), &message)

				// For testing with docker containers
				log.Printf("[DEBUG] FileURL: %s serverURL: %s", message.FileURL, client.config.SyncServerURL)
				if strings.Contains(message.FileURL, "localhost") && !strings.Contains(client.config.SyncServerURL, "localhost") {
					message.FileURL = fmt.Sprintf("%s%s", client.config.SyncServerURL, strings.TrimPrefix(message.FileURL, "http://localhost:8080"))
				} else if strings.Contains(message.FileURL, "file-server") && strings.Contains(client.config.SyncServerURL, "localhost") {
					message.FileURL = fmt.Sprintf("%s%s", client.config.SyncServerURL, strings.TrimPrefix(message.FileURL, "http://file-server:8080"))
				}

				if message.ClientID == client.config.ClientID {
					// Consumer discards message that it was sent from Broker
					err = deliveryContext.Discard(context.Background(), &amqp.Error{})
					rmq.Error("[CONSUMER] Discarded message %v", message)
					continue
				}

				rmq.Info(
					"[CONSUMER] received message",
					fmt.Sprintf("%+v", message),
				)

				// Convert normalized path to denormalized (absolute) path for local file system
				denormalizedPath := utils.RelToAbsConvert(client.config.SyncDirectory, message.Path)

				rmq.Info("[CONSUMER DEBUG] Rel to Abs: ", denormalizedPath)

				err = deliveryContext.Accept(context.Background())
				if err != nil {
					rmq.Error("[CONSUMER] Failed to accept message", err)
					continue
				}

				switch message.Event {
				case "CREATE":
					if strings.HasSuffix(message.Path, ".md") {
						rmq.Info("[CONSUMER] CREATE DOWNLOADING")
						err := download(denormalizedPath)
						if err != nil {
							rmq.Error("[CONSUMER] Failed to download create", err)
							continue
						}
						// Consuming directory create events
					} else {
						rmq.Info("[CONSUMER] CREATING DIRECTORY", message.Path)
						ignoreEvents.putIgnore(denormalizedPath)
						err := utils.MkDir(denormalizedPath)
						if err != nil {
							rmq.Error("[CONSUMER] Faield to create directory", err)
						}
						watcher.Add(denormalizedPath)
					}
				case "WRITE":
					rmq.Info("[CONSUMER] WRITE DOWNLOADING")
					err := download(denormalizedPath)
					if err != nil {
						rmq.Error("[CONSUMER] Failed to download write", err)
						continue
					}
				case "NECO_RENAME":
					denormalizedOldPath := utils.RelToAbsConvert(client.config.SyncDirectory, message.OldPath)
					denormalizedNewPath := utils.RelToAbsConvert(client.config.SyncDirectory, message.Path)

					rmq.Info("[CONSUMER] -NECO_RENAME-")
					ignoreEvents.putIgnore(denormalizedOldPath)
					ignoreEvents.putIgnore(denormalizedNewPath)
					err := utils.Rename(denormalizedOldPath, denormalizedNewPath)
					if err != nil {
						rmq.Error("[CONSUMER] -NECO_REMAKE- ", err)
						continue
					}
					if utils.IsDir(denormalizedOldPath) {
						watcher.Add(denormalizedNewPath)
					}
				case "REMOVE":
					rmq.Info("[CONSUMER] -REMOVE-")
					denormalizedPath := utils.RelToAbsConvert(client.config.SyncDirectory, message.Path)
					ignoreEvents.putIgnore(denormalizedPath)
					err := utils.Rm(denormalizedPath)
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

func (ep EventProcessor) putEvent(path, oldPath, event string) {
	ep.fileEvents <- EventData{
		path:      path,
		oldPath:   oldPath,
		event:     event,
		timestamp: time.Now(),
	}
}

func (ep EventProcessor) processEvents(watcher *fsnotify.Watcher) {
	go func() {
		for eventData := range ep.fileEvents {
			switch eventData.event {
			case "CREATE":
				if strings.HasSuffix(eventData.path, ".md") {
					if processedFiles.isProcessed(eventData.path) {
						log.Printf("File %s has already been processed for event %s", eventData.path, eventData.event)
						continue
					}
					err := upload(eventData.path)
					if err != nil {
						log.Println(err)
					}
				} else if utils.IsDir(eventData.path) {
					watcher.Add(eventData.path)
					client.apiClient.RemoteMkdir(eventData.path, client.config.ClientID)
					if processedFiles.isProcessed(eventData.path) {
						log.Printf("File %s has already been processed for event %s", eventData.path, eventData.event)
						continue
					}
				}
			case "WRITE":
				if strings.HasSuffix(eventData.path, ".md") {
					if !ignoreEvents.isIgnored(eventData.path) {
						log.Printf("[EVENT PROCESSOR] -WRITE- is write ready and uploading\n")
						err := upload(eventData.path)
						if err != nil {
							log.Println(err)
						}
						log.Printf("[EVENT PROCESSOR] -WRITE- publishing\n")
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
				err := client.apiClient.RemoteRename(eventData.oldPath, eventData.path, client.config.ClientID)
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
				err := client.apiClient.RemoteRemove(eventData.path, client.config.ClientID)
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

func (wd WriteDebouncer) putWrite(path string) {
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
				eventProcessor.putEvent(finalEvent.path, finalEvent.oldPath, finalEvent.event)
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
