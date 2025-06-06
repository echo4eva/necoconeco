//go:build offlinesync

// build offlinesync

package main

import (
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
	address       string
	queueName     string
	serverURL     string
	syncDirectory string
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("No environment variables found\n", err)
	}

	log.Printf("Starting offline sync client\n")

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
		log.Printf("[OFFLINE-SYNC]-[PURGE]-[ERROR] %s\n", err)
	}
	log.Printf("[OFFLINE-SYNC] PURGING %d\n", purgedAmount)

	// Get file-server directory+file metadata
	serverMetadata, err := downloadMetadata()
	if err != nil {
		log.Printf("[OFFLINE-SYNC]-[SERVER METADATA] %s\n", err)
	}

	// Get local/client's directory+file metadata
	localMetadata, err := utils.GetLocalMetadata(syncDirectory)
	if err != nil {
		log.Printf("[OFFLINE-SYNC]-[LOCAL METADATA] %s\n")
	}

	processMetadata(localMetadata, serverMetadata)
}

func downloadMetadata() (*utils.DirectoryMetadata, error) {
	downloadURL := fmt.Sprintf("http://%s/metadata", serverURL)

	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to create request", err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to do request", err)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read response body", err)
	}

	var serverMetadata api.MetadataResponse
	if err := json.Unmarshal(bodyBytes, &serverMetadata); err != nil {
		return nil, fmt.Errorf("Failed to unmarshal server metadata", err)
	}

	var fileserverDirectoryMetadata *utils.DirectoryMetadata
	fileserverDirectoryMetadata = serverMetadata.DirectoryMetadata

	return fileserverDirectoryMetadata, nil
}

func processMetadata(localMetadata, serverMetadata *utils.DirectoryMetadata) {
	// Accumulate all unique paths from both local and server metadata
	allPathsSet := make(map[string]struct{})
	for path := range localMetadata.Files {
		allPathsSet[path] = struct{}{}
	}
	for path := range serverMetadata.Files {
		allPathsSet[path] = struct{}{}
	}

	// Reallocate into paths into slice, just makes sense
	allPaths := make([]string, 0, len(allPathsSet))
	for path := range allPathsSet {
		allPaths = append(allPaths, path)
	}

	// Compare local and server meta data - REVERSE LOGIC (local is source of truth)
	for _, path := range allPaths {
		localFileMetadata, existsLocally := localMetadata.Files[path]
		serverFileMetadata, existsOnServer := serverMetadata.Files[path]
		absolutePath := utils.RelToAbsConvert(syncDirectory, path)

		if path == "." {
			continue
		}

		fmt.Printf("[%s] local exist: %t | server exist: %t\n", path, existsLocally, existsOnServer)

		if existsLocally && existsOnServer {
			// If content hashes are different, upload local to server (local is source of truth)
			if !utils.IsTrueHash(localFileMetadata.ContentHash, serverFileMetadata.ContentHash) {
				fmt.Printf("[%s] hash difference detected, uploading to server\n", path)
				uploadToServer(path, absolutePath)
			}
		} else if existsLocally && !existsOnServer {
			if utils.IsDir(path) {
				fmt.Printf("[%s] making directory on server\n", path)
				err := api.RemoteMkdir(absolutePath, syncDirectory, serverURL)
				if err != nil {
					fmt.Printf("[%s] [ERROR] %s\n", path, err)
				}
			} else {
				fmt.Printf("[%s] uploading file to server\n", path)
				uploadToServer(path, absolutePath)
			}
		} else if !existsLocally && existsOnServer {
			fmt.Printf("[%s] removing file/directory from server\n", path)
			err := api.RemoteRemove(absolutePath, syncDirectory, serverURL)
			if err != nil {
				fmt.Printf("[%s] [ERROR] %s\n", path, err)
			}
		}
	}
}

func uploadToServer(relativePath, absolutePath string) {
	// Create fields map for upload
	fields := map[string]string{
		"path": relativePath,
	}

	_, err := api.Upload(fields, absolutePath, serverURL)
	if err != nil {
		fmt.Printf("[%s] [UPLOAD ERROR] %s\n", relativePath, err)
	} else {
		fmt.Printf("[%s] upload successful\n", relativePath)
	}
}
