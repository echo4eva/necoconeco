//go:build server
// +build server

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/echo4eva/necoconeco/internal/api"
	"github.com/echo4eva/necoconeco/internal/utils"
)

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
	http.HandleFunc("/snapshot", snapshotHandler)

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

	log.Printf("%s", relativePath)

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

	fileURL := fmt.Sprintf("http://%s/files/%s", r.Host, relativePath)

	w.Header().Set("Content-Type", "application/json")
	response := api.UploadResponse{
		Response: api.Response{
			http.StatusOK,
		},
		FileURL: fileURL,
	}
	json.NewEncoder(w).Encode(response)
}

func directoryHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var reqPayload api.CreateDirectoryRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		relativeDirectory := reqPayload.Directory
		log.Printf("[DIRECTORY HANDLER] %s\n", reqPayload.Directory)
		absolutePath := utils.RelToAbsConvert(syncDirectory, relativeDirectory)
		err = utils.MkDir(absolutePath)
		if err != nil {
			http.Error(w, "Error creating direcgtory", http.StatusInternalServerError)
			return
		}

		response := api.Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func renameHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var reqPayload api.RenameRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		oldName := reqPayload.OldName
		newName := reqPayload.NewName
		absoluteOld := utils.RelToAbsConvert(syncDirectory, oldName)
		absoluteNew := utils.RelToAbsConvert(syncDirectory, newName)
		log.Printf("[RENAME HANDLER] Old: %s New: %s\n", oldName, newName)

		err = os.Rename(absoluteOld, absoluteNew)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error renaming, old: %s | new: %s", absoluteOld, absoluteNew), http.StatusInternalServerError)
			return
		}

		response := api.Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func removeHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var reqPayload api.RemoveRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		path := reqPayload.Path
		absolutePath := utils.RelToAbsConvert(syncDirectory, path)

		log.Printf("[REMOVE HANDLER] To remove: %s\n", absolutePath)
		err = os.RemoveAll(absolutePath)
		if err != nil {
			http.Error(w, "Error removing", http.StatusInternalServerError)
			return
		}

		response := api.Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func metadataHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		log.Println("Received metadata request from client")
		response := api.MetadataResponse{
			DirectoryMetadata: &utils.DirectoryMetadata{},
			Response:          api.Response{},
		}

		_, err := os.Stat(syncDirectory)
		if err != nil {
			http.Error(w, "Error sync directory not initialized", http.StatusInternalServerError)
			return
		}

		var localMetadata *utils.DirectoryMetadata
		localMetadata, err = utils.GetLocalMetadata(syncDirectory)
		if err != nil {
			http.Error(w, fmt.Sprintf("%s", err), http.StatusInternalServerError)
			return
		}

		response.DirectoryMetadata = localMetadata
		response.Status = http.StatusOK

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

func snapshotHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		response := api.PostSnapshotResponse{
			SyncActionMetadata: &utils.SyncActionMetadata{},
			Response:           api.Response{},
		}

		var reqPayload api.PostSnapshotRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		log.Printf("Received snapshot from client: %+v\n", reqPayload.FinalSnapshot)

		clientSnapshot := reqPayload.FinalSnapshot

		_, err = os.Stat(syncDirectory)
		if err != nil {
			http.Error(w, "Error sync directory not initialized", http.StatusInternalServerError)
			return
		}

		var serverSnapshot *utils.DirectoryMetadata
		serverSnapshot, err = utils.GetLocalMetadata(syncDirectory)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error retrieving server local metadata: %s", err), http.StatusInternalServerError)
			return
		}
		log.Printf("Server snapshot: %+v\n", serverSnapshot)

		// Retrieve client actions by comparing snapshots
		clientFileActions, err := processSnapshots(serverSnapshot, clientSnapshot)
		if err != nil {
			log.Printf("Error processing client and server snapshots: %s", err)
			http.Error(w, fmt.Sprintf("Error processing client and server snapshots: %s", err), http.StatusInternalServerError)
			return
		}
		log.Printf("Client file actions: %+v\n", clientFileActions)

		response.SyncActionMetadata = clientFileActions
		response.Status = http.StatusOK

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

func processSnapshots(serverSnapshot, clientSnapshot *utils.DirectoryMetadata) (*utils.SyncActionMetadata, error) {
	fileActions := utils.SyncActionMetadata{
		Files: make(map[string]utils.FileActionMetadata),
	}

	// Compare client snapshot to server serverSnapshot to determine actions for client/server
	for path, clientFileMetadata := range clientSnapshot.Files {
		clientLastModified, err := time.Parse(utils.TimeFormat, clientFileMetadata.LastModified)
		if err != nil {
			return nil, err
		}
		clientFileStatus := clientFileMetadata.Status
		// If exists in both
		if serverFileMetadata, existsOnServer := serverSnapshot.Files[path]; existsOnServer {
			serverLastModified, err := time.Parse(utils.TimeFormat, serverFileMetadata.LastModified)
			if err != nil {
				return nil, err
			}
			// LWW, Last Write Wins
			// --- 1 - server wins
			// --- 0 - same, do nothing
			// --- -1 - client wins
			switch serverLastModified.Compare(clientLastModified) {
			// Server wins, send download action to client
			case 1:
				fileActions.Files[path] = utils.FileActionMetadata{
					Action: utils.ActionDownload,
				}
			// Client wins
			case -1:
				// If deleted on client, server needs to delete
				if clientFileStatus == utils.StatusDeleted {
					absolutePath := utils.RelToAbsConvert(syncDirectory, path)
					os.RemoveAll(absolutePath)
					// Else, send upload action to client
				} else {
					fileActions.Files[path] = utils.FileActionMetadata{
						Action: utils.ActionUpload,
					}
				}
			}
			// Only exists on client, so just upload to file server
		} else {
			if clientFileStatus == utils.StatusExists {
				fileActions.Files[path] = utils.FileActionMetadata{
					Action: utils.ActionUpload,
				}
			}
		}
	}
	// Now compare serverSnapshot to clientSnapshot to find files that exist on server but not on client
	for path, serverFileMetadata := range serverSnapshot.Files {
		if _, existsOnClient := clientSnapshot.Files[path]; !existsOnClient {
			// If the file exists on the server and is not marked as deleted, client should download it
			if serverFileMetadata.Status == utils.StatusExists {
				fileActions.Files[path] = utils.FileActionMetadata{
					Action: utils.ActionDownload,
				}
			}
		}
	}

	return &fileActions, nil
}
