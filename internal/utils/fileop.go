package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

type FileAction string
type FileStatus string

const (
	StatusExists        = "exists"
	StatusDeleted       = "deleted"
	SnapshotFileName    = "necoshot.json"
	HiddenDirectoryName = ".neco"

	ActionUpload   FileAction = "upload"
	ActionDownload FileAction = "download"

	TimeFormat string = "RFC3339"
)

type FileMetadata struct {
	LastModified string `json:"last_modified"`
	ContentHash  string `json:"content_hash,omitempty"`
	Status       string `json:"status"`
}

type FileActionMetadata struct {
	Action FileAction `json:"action"`
}

type DirectoryMetadata struct {
	Files map[string]FileMetadata `json:"files"`
}

type SyncActionMetadata struct {
	Files map[string]FileActionMetadata `json:"files"`
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
				Status:       StatusExists,
			}
		} else {
			if relativePath != "." {
				directoryMetadata.Files[relativePath] = FileMetadata{
					LastModified: lastModified,
					Status:       StatusExists,
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

func CreateDirectorySnapshot(syncDirectory string) error {
	directoryMetadata := DirectoryMetadata{
		Files: make(map[string]FileMetadata),
	}

	err := filepath.WalkDir(syncDirectory, func(path string, d fs.DirEntry, err error) error {
		fileInfo, err := d.Info()
		if err != nil {
			log.Printf("Error getting file info")
			return err
		}
		lastModified := fileInfo.ModTime().Format(TimeFormat)
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
				Status:       StatusExists,
			}
		} else {
			if relativePath != "." {
				directoryMetadata.Files[relativePath] = FileMetadata{
					LastModified: lastModified,
					Status:       StatusExists,
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	jsonData, err := json.Marshal(&directoryMetadata)
	if err != nil {
		return err
	}
	// Make hidden directory to store snapshot if it doesn't exist already
	err = mkHiddenNecoDir(syncDirectory)
	if err != nil {
		return err
	}
	// Create the necoshot.json
	necoShotPath := getNecoShotPath(syncDirectory)
	err = os.WriteFile(necoShotPath, jsonData, 0644)
	if err != nil {
		return err
	}
	return nil
}

func GetLastSnapshot(syncDirectory string) (*DirectoryMetadata, bool, error) {
	lastSnapshotPath := getNecoShotPath(syncDirectory)

	// Read the json file
	jsonData, err := os.ReadFile(lastSnapshotPath)
	if err != nil {
		// Determine error, DNE or an actual error
		exists := os.IsNotExist(err)
		if !exists {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("Failed to read json: %s\n", err)
	}

	var snapshot DirectoryMetadata
	json.Unmarshal(jsonData, &snapshot)
	if err != nil {
		return nil, false, fmt.Errorf("Failed to unmarshal json data: %s\n", err)
	}

	return &snapshot, true, nil
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

func getHiddenNecoDir(syncDirectory string) string {
	hiddenDirPath := filepath.Join(syncDirectory, HiddenDirectoryName)
	return hiddenDirPath
}

func getNecoShotPath(syncDirectory string) string {
	hiddenDirPath := getHiddenNecoDir(syncDirectory)
	necoShotPath := filepath.Join(hiddenDirPath, SnapshotFileName)
	return necoShotPath
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

func mkHiddenNecoDir(syncDirectory string) error {
	hiddenDirPath := getHiddenNecoDir(syncDirectory)
	err := os.MkdirAll(hiddenDirPath, os.ModePerm)
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
