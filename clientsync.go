//go:build clientsync

// build clientsync

package main

import (
	"log"
	"os"

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

	lastSnapshot, err := utils.RetrieveLastSnapshot()
	if err != nil {
		return
	}
}
