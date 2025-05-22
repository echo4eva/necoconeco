//go:build sync

// build sync

package main

import (
	// "bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/echo4eva/necoconeco/internal/api"
	"github.com/echo4eva/necoconeco/internal/utils"
	"github.com/joho/godotenv"
	// "github.com/echo4eva/necoconeco/internal/utils"
)

var (
	serverURL     string
	syncDirectory string
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("No environment variables found\n", err)
	}

	serverURL = os.Getenv("SYNC_SERVER_URL")
	syncDirectory = os.Getenv("SYNC_DIRECTORY")

	// Get file-server directory+file metadata
	serverMetadata, err := downloadMetadata()
	if err != nil {
		log.Printf("[SYNC]-[SERVER METADATA] %s\n", err)
	}

	// Get local/client's directory+file metadata
	localMetadata, err := utils.GetLocalMetadata(syncDirectory)
	if err != nil {
		log.Printf("[SYNC]-[LOCAL METADATA] %s\n")
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

	// Compare local and server meta data
	for _, path := range allPaths {
		localFileMetadata, existsLocally := localMetadata.Files[path]
		serverFileMetadata, existsOnServer := serverMetadata.Files[path]
		absolutePath := utils.RelToAbsConvert(path, syncDirectory)
		fmt.Printf("[%s] local exist: %t | server exist: %t\n", path, existsLocally, existsOnServer)
		// File exists on both
		// ----------- only on server
		// ----------- only locally
		if existsLocally && existsOnServer {
			// If content hashes are different, download from file-server (source of truth)
			if !utils.IsTrueHash(localFileMetadata.ContentHash, serverFileMetadata.ContentHash) {
				fmt.Printf("[%s] hash dif detected, downloading locally\n", path)
				api.Download(path, serverURL, syncDirectory)
			}
		} else if !existsLocally && existsOnServer {
			if utils.IsDir(path) {
				fmt.Printf("[%s] making directory locally\n", path)
				utils.MkDir(absolutePath)
			} else {
				fmt.Printf("[%s] downloading file locally\n", path)
				api.Download(path, serverURL, syncDirectory)
			}
		} else if existsLocally && !existsOnServer {
			fmt.Printf("[%s] removing file/directory locally\n", path)
			utils.Rm(absolutePath)
		}
	}
}
