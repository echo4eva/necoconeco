//go:build server
// +build server

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

type MetadataResponse struct {
	Response
	Files map[string]FileMetadata `json:"files"`
}

type UploadResponse struct {
	Response
	FileURL string `json:"file_url"`
}

type CreateDirectoryRequest struct {
	Directory string `json:"directory"`
}

type RenameRequest struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

type RemoveRequest struct {
	Path string `json:"path"`
}

type Response struct {
	Status int `json:"status"`
}

type FileMetadata struct {
	LastModified string `json:"last_modified"`
	ContentHash  string `json:"content_hash"`
}

var (
	syncDirectory = "/app/storage"
)

func main() {
	fs := http.FileServer(http.Dir("./storage"))
	http.Handle("/files/", http.StripPrefix("/files/", fs))

	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/directory", directoryHandler)
	http.HandleFunc("/rename", renameHandler)
	http.HandleFunc("/remove", removeHandler)
	http.HandleFunc("/metadata", metadataHandler)

	fmt.Println("Server started at :8080")
	err := http.ListenAndServe("0.0.0.0:8080", nil)
	if err != nil {
		fmt.Println("someshit happened %w", err)
	}
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relativePath := r.FormValue("path")

	r.ParseMultipartForm(10 << 20)

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	os.MkdirAll("./storage", os.ModePerm)

	dst, err := os.Create(filepath.Join("./storage/", relativePath))
	if err != nil {
		http.Error(w, "Error creating file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}

	fileURL := fmt.Sprintf("http://%s/files%s", r.Host, relativePath)

	w.Header().Set("Content-Type", "application/json")
	response := UploadResponse{
		Response: Response{
			http.StatusOK,
		},
		FileURL: fileURL,
	}
	json.NewEncoder(w).Encode(response)
}

func directoryHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var reqPayload CreateDirectoryRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		relativeDirectory := reqPayload.Directory
		log.Printf("[DIRECTORY HANDLER] %s\n", reqPayload.Directory)
		err = os.MkdirAll(absoluteConvert(relativeDirectory), 755)
		if err != nil {
			http.Error(w, "Error creating direcgtory", http.StatusInternalServerError)
			return
		}

		response := Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func renameHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var reqPayload RenameRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		oldName := reqPayload.OldName
		newName := reqPayload.NewName
		log.Printf("[RENAME HANDLER] Old: %s New: %s\n", oldName, newName)

		err = os.Rename(absoluteConvert(oldName), absoluteConvert(newName))
		if err != nil {
			http.Error(w, "Error renaming", http.StatusInternalServerError)
			return
		}

		response := Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func removeHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var reqPayload RemoveRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		path := reqPayload.Path
		log.Printf("[REMOVE HANDLER] To remove: %s\n", path)

		err = os.RemoveAll(absoluteConvert(path))
		if err != nil {
			http.Error(w, "Error removing", http.StatusInternalServerError)
			return
		}

		response := Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func metadataHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response := MetadataResponse{
			Files:    make(map[string]FileMetadata),
			Response: Response{},
		}

		if _, err := os.Stat(syncDirectory); err != nil {
			http.Error(w, "Error sync directory not initialized", http.StatusInternalServerError)
			return
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
				response.Files[relativePath] = FileMetadata{
					LastModified: lastModified,
					ContentHash:  contentHash,
				}
			} else {
				if relativePath != "." {
					response.Files[relativePath] = FileMetadata{
						LastModified: lastModified,
					}
				}
			}
			return nil
		})
		if err != nil {
			log.Fatal(err)
			http.Error(w, "Error walking file server directory", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response.Status = http.StatusOK
		json.NewEncoder(w).Encode(response)
	}
}

func absoluteConvert(relativePath string) string {
	log.Printf("[FILE SERVER] relative path: %s\n", relativePath)
	return syncDirectory + relativePath
}
