package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/echo4eva/necoconeco/internal/utils"
)

func main() {
	// Define command line flags
	var syncDir string
	flag.StringVar(&syncDir, "dir", "", "Directory to generate necoshot.json for (required)")
	flag.Parse()

	// Validate required flag
	if syncDir == "" {
		fmt.Println("Usage: necoshot_generator -dir <directory_path>")
		fmt.Println("  -dir: Directory to generate necoshot.json for")
		os.Exit(1)
	}

	fm := utils.NewFileManager(syncDir)

	// Check if directory exists
	if _, err := os.Stat(syncDir); os.IsNotExist(err) {
		log.Fatalf("Directory does not exist: %s", syncDir)
	}

	// Generate the snapshot
	fmt.Printf("Generating necoshot.json for directory: %s\n", syncDir)
	err := fm.CreateDirectorySnapshot()
	if err != nil {
		log.Fatalf("Failed to create directory snapshot: %v", err)
	}

	// Confirm success
	necoShotPath := fmt.Sprintf("%s/.neco/necoshot.json", syncDir)
	fmt.Printf("Successfully created necoshot.json at: %s\n", necoShotPath)
}
