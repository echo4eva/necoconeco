//go:build clientsync

// build clientsync

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/echo4eva/necoconeco/internal/api"
	"github.com/echo4eva/necoconeco/internal/utils"
	"github.com/joho/godotenv"
)

var (
	serverURL     string
	syncDirectory string
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("No environment variables found, %s\n", err)
	}

	serverURL = os.Getenv("SYNC_SERVER_URL")
	syncDirectory = os.Getenv("SYNC_DIRECTORY")

	// Grab last snapshot if possible
	lastSnapshot, exists, err := utils.GetLastSnapshot(syncDirectory)
	if err != nil {
		log.Println(err)
	}
	currentSnapshot, err := utils.GetLocalMetadata(syncDirectory)
	if err != nil {
		log.Println(err)
		return
	}

	// Check last snapshot existence
	// --- True: compare last with current, send
	// --- False: send
	if exists {
		finalSnapshot := processSnapshots(lastSnapshot, currentSnapshot)
		postSnapshot(finalSnapshot)
	} else {
		postSnapshot(currentSnapshot)
	}

	processResponse()
}

func processSnapshots(lastSnapshot, currentSnapshot *utils.DirectoryMetadata) *utils.DirectoryMetadata {
	finalSnapshot := utils.DirectoryMetadata{
		Files: make(map[string]utils.FileMetadata),
	}

	// Get all lastSnapshot metadata
	for path, fileMetadata := range lastSnapshot.Files {
		// if path DNE on currentSnapshot, then add tombstone to final
		if _, exists := currentSnapshot.Files[path]; !exists {
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

func postSnapshot(finalSnapshot *utils.DirectoryMetadata) error {
	postURL := fmt.Sprintf("http://%s/snapshot", serverURL)

	payload := api.PostSnapshotRequest{
		FinalSnapshot: finalSnapshot,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, postURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
}
