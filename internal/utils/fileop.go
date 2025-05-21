package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

type FileMetadata struct {
	LastModified string `json:"last_modified"`
	ContentHash  string `json:"content_hash"`
}

type DirectoryMetadata struct {
	Files map[string]FileMetadata `json:"files"`
}

func GetLocalMetadata(syncDirectory string) (*DirectoryMetadata, error) {
	directoryMetadata := DirectoryMetadata{
		Files: make(map[string]FileMetadata),
	}

	err := filepath.WalkDir(syncDirectory, func(path string, d fs.DirEntry, err error) error {
		fileInfo, err := d.Info()
		if err != nil {
			log.Printf("Error getting file info")
			return err
		}
		lastModified := fileInfo.ModTime().String()
		relativePath, err := filepath.Rel(syncDirectory, path)
		if err != nil {
			fmt.Printf("Error converting %s to relative path", path)
			return err
		}

		// if its a file, access its contents
		if !d.IsDir() {
			fileContent, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("Error reading file %s", path)
				return err
			}
			sum := sha256.Sum256(fileContent)
			contentHash := hex.EncodeToString(sum[:])
			directoryMetadata.Files[relativePath] = FileMetadata{
				LastModified: lastModified,
				ContentHash:  contentHash,
			}
		} else {
			if relativePath != "." {
				directoryMetadata.Files[relativePath] = FileMetadata{
					LastModified: lastModified,
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &directoryMetadata, nil
}
