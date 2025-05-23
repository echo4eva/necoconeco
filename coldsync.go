//go:build coldsync

// build coldsync

package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

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

	err = filepath.WalkDir(syncDirectory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("Error getting file info")
		}
		relativePath := utils.AbsToRelConvert(syncDirectory, path)

		// Debug
		fmt.Printf("[DEBUG] path: %s | relative path: %s\n", path, relativePath)

		// if its a file, access its contents
		if !d.IsDir() {
			_, err := api.Upload(path, syncDirectory, serverURL)
			if err != nil {
				fmt.Printf("Failed to upload %s", err)
			}
		} else {
			if relativePath != "." {
				api.RemoteMkdir(path, syncDirectory, serverURL)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Printf("%s", err)
	}
}
