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

	serverMetadata, err := downloadMetadata()
	if err != nil {
		log.Printf("[SYNC]-[FAILED] %s\n", err)
	}

	fmt.Println(serverMetadata)
}

func downloadMetadata() (*api.MetadataResponse, error) {
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

	return &serverMetadata, nil
}
