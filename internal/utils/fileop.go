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
		relativePath := AbsToRelConvert(syncDirectory, path)

		// Debug
		fmt.Printf("[DEBUG] path: %s | relative path: %s\n", path, relativePath)

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

func IsTrueHash(localHash, trueHash string) bool {
	return localHash == trueHash
}

func RelToAbsConvert(syncDirectory, relativePath string) string {
	absolutePath := filepath.Join(syncDirectory, relativePath)
	return absolutePath
}

func AbsToRelConvert(syncDirectory, absolutePath string) string {
	relativePath, _ := filepath.Rel(syncDirectory, absolutePath)
	return relativePath
}

func GetOnlyDir(fullPath string) string {
	return filepath.Dir(fullPath)
}

func IsDir(path string) bool {
	ext := filepath.Ext(path)
	return ext == ""
}

func MkDir(absolutePath string) error {
	err := os.MkdirAll(absolutePath, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

func Rm(absolutePath string) error {
	err := os.RemoveAll(absolutePath)
	if err != nil {
		return err
	}
	return nil
}

func Rename(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err != nil {
		return err
	}
	return nil
}
