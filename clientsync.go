//go:build clientsync

// build clientsync

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/echo4eva/necoconeco/internal/api"
	"github.com/echo4eva/necoconeco/internal/utils"
	"github.com/joho/godotenv"
	rmq "github.com/rabbitmq/rabbitmq-amqp-go-client/pkg/rabbitmqamqp"
)

var (
	clientID string
	address       string
	queueName     string
	serverURL     string
	syncDirectory string
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("No environment variables found, %s\n", err)
	}

	log.Printf("Starting sync client\n")

	address = os.Getenv("RABBITMQ_ADDRESS")
	queueName = os.Getenv("RABBITMQ_QUEUE_NAME")
	serverURL = os.Getenv("SYNC_SERVER_URL")
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

	// Declaring queue just in case the client's queue doesn't exist
	_, err = management.DeclareQueue(context.Background(), &rmq.ClassicQueueSpecification{
		Name:         queueName,
		IsAutoDelete: false,
	})
	if err != nil {
		rmq.Error("Failed to declare queue", err)
		return
	}

	// Assume that the queue exists already
	purgedAmount, err := management.PurgeQueue(context.Background(), queueName)
	if err != nil {
		log.Printf("[SYNC]-[PURGE]-[ERROR] %s\n", err)
		return
	}
	log.Printf("[SYNC] PURGING %d\n", purgedAmount)

	// Grab last snapshot if possible
	log.Println("Getting last snapshot")
	lastSnapshot, exists, err := utils.GetLastSnapshot(syncDirectory)
	if err != nil {
		log.Println(err)
	}
	log.Printf("Last snapshot struct: %+v\n", lastSnapshot)

	// Start of sync
	log.Println("Getting local metadata/current snapshot")
	currentSnapshot, err := utils.GetLocalMetadata(syncDirectory)
	if err != nil {
		log.Println(err)
		return
	}
	log.Printf("Current snapshot struct: %+v\n", currentSnapshot)

	// Check last snapshot existence
	// --- True: compare last with current, send to server
	// --- False: send current to server
	var syncActionMetadata *utils.SyncActionMetadata
	if exists {
		log.Println("Last snapshot exists, comparing with current snapshot")
		finalSnapshot := processSnapshots(lastSnapshot, currentSnapshot)
		syncActionMetadata, err = postSnapshot(finalSnapshot)
		if err != nil {
			log.Println(err)
			return
		}

	} else {
		log.Println("Last snapshot does not exist, sending current snapshot to server")
		syncActionMetadata, err = postSnapshot(currentSnapshot)
		if err != nil {
			log.Println(err)
			return
		}
	}

	log.Println("Processing actions")
	processActions(syncActionMetadata)
}

func processSnapshots(lastSnapshot, currentSnapshot *utils.DirectoryMetadata) *utils.DirectoryMetadata {
	finalSnapshot := utils.DirectoryMetadata{
		Files: make(map[string]utils.FileMetadata),
	}

	// Get all lastSnapshot metadata
	for path, fileMetadata := range lastSnapshot.Files {
		// if path DNE on currentSnapshot, then add tombstone to final
		if _, exists := currentSnapshot.Files[path]; !exists {
			log.Printf("Path %s does not exist on current snapshot, adding tombstone", path)
			fileMetadata.Status = utils.StatusDeleted
			finalSnapshot.Files[path] = fileMetadata
		}
	}

	// Get all currentSnapshot metadata
	for path, fileMetadata := range currentSnapshot.Files {
		finalSnapshot.Files[path] = fileMetadata
	}

	return &finalSnapshot
}

func postSnapshot(finalSnapshot *utils.DirectoryMetadata) (*utils.SyncActionMetadata, error) {
	log.Println("Posting snapshot to server")
	postURL := fmt.Sprintf("http://%s/snapshot", serverURL)
	log.Printf("Final snapshot to be sent to server: %+v\n", finalSnapshot)

	payload := api.PostSnapshotRequest{
		ClientID: client
		FinalSnapshot: finalSnapshot,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, postURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response api.PostSnapshotResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return nil, err
	}

	return response.SyncActionMetadata, nil
}

func processActions(syncActionMetadata *utils.SyncActionMetadata) {
	if syncActionMetadata == nil {
		log.Println("No sync actions to process")
		return
	}

	// Iterate through all file actions
	for relativePath, fileActionMetadata := range syncActionMetadata.Files {
		log.Printf("Processing action %s for file: %s", fileActionMetadata.Action, relativePath)

		switch fileActionMetadata.Action {
		case utils.ActionUpload:
			// Upload file to server
			absolutePath := utils.RelToAbsConvert(syncDirectory, relativePath)

			// Create fields map for upload
			fields := map[string]string{
				"path": relativePath,
			}

			uploadResponse, err := api.Upload(fields, absolutePath, serverURL)
			if err != nil {
				log.Printf("Failed to upload %s: %s", relativePath, err)
			} else {
				log.Printf("Successfully uploaded %s, FileURL: %s", relativePath, uploadResponse.FileURL)
			}

		case utils.ActionDownload:
			// Download file from server
			err := api.Download(relativePath, syncDirectory, serverURL)
			if err != nil {
				log.Printf("Failed to download %s: %s", relativePath, err)
			} else {
				log.Printf("Successfully downloaded %s", relativePath)
			}
		case utils.ActionMkdir:
			absolutePath := utils.RelToAbsConvert(syncDirectory, relativePath)
			err := utils.MkDir(absolutePath)
			if err != nil {
				log.Printf("Failed to create directory %s: %s", relativePath, err)
			} else {
				log.Printf("Successfully created directory %s", relativePath)
			}
		default:
			log.Printf("Unknown action: %s for file: %s", fileActionMetadata.Action, relativePath)
		}
	}

	// After processing all actions, create a new snapshot
	log.Println("Creating new snapshot")
	err := utils.CreateDirectorySnapshot(syncDirectory)
	if err != nil {
		log.Printf("Failed to create snapshot after sync: %s", err)
	} else {
		log.Println("Successfully created snapshot after sync")
	}
}
