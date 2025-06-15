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

	absolutePath := utils.RelToAbsConvert(syncDirectory, relativePath)
	directory := utils.GetOnlyDir(absolutePath)
	if err := utils.MkDir(directory); err != nil {
		http.Error(w, "Error creating directory", http.StatusInternalServerError)
		return
	}
	dst, err := os.Create(absolutePath)
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

	// Accumulate all unique paths from both local and server metadata
	allPathsSet := make(map[string]struct{})
	for path := range serverSnapshot.Files {
		allPathsSet[path] = struct{}{}
	}
	for path := range clientSnapshot.Files {
		allPathsSet[path] = struct{}{}
	}

	// Reallocate paths into slice, just makes sense
	allPaths := make([]string, 0, len(allPathsSet))
	for path := range allPathsSet {
		allPaths = append(allPaths, path)
	}

	for _, path := range allPaths {
		serverFileMetadata, existsOnServer := serverSnapshot.Files[path]
		clientFileMetadata, existsOnClient := clientSnapshot.Files[path]

		// If exists do LWW
		if existsOnServer && existsOnClient {
			// isDirectory should be correct, since we're operating on a path existing in both
			// but do `==` anyways to look good.
			// Don't think there should be edge case of foo (dir) and foo (file) since we operate
			// with only markdown.
			// But could be if a dumbass uses `foo.md` (dir) and `foo.md` (file) in same director
			// TODO: fix edge case later
			isDirectory := clientFileMetadata.IsDirectory
			serverLastModified, err := time.Parse(utils.TimeFormat, serverFileMetadata.LastModified)
			if err != nil {
				return nil, err
			}
			clientLastModified, err := time.Parse(utils.TimeFormat, clientFileMetadata.LastModified)
			if err != nil {
				return nil, err
			}

			// LWW, Last Write Wins
			// --- 1 - server wins
			// --- -1 - client wins
			// --- 0 - tie
			log.Printf("[PROCESS SNAPSHOTS - %d] Server last modified: %s | Client last modified: %s | Path: %s\n", serverLastModified.Compare(clientLastModified), serverLastModified, clientLastModified, path)

			switch serverLastModified.Compare(clientLastModified) {
			// Server wins
			case 1:
				log.Printf("[PROCESS SNAPSHOTS] Server wins, HELLO WORLD\n")
				if !isDirectory {
					log.Printf("[PROCESS SNAPSHOTS] Server wins, sending download action to client: %s\n", path)
					fileActions.Files[path] = utils.FileActionMetadata{
						Action: utils.ActionDownload,
					}
				}
			// Client wins
			case -1:
				clientFileStatus := clientFileMetadata.Status
				// If deleted on client, server needs to delete
				if clientFileStatus == utils.StatusDeleted {
					absolutePath := utils.RelToAbsConvert(syncDirectory, path)
					if isDirectory {
						log.Printf("[PROCESS SNAPSHOTS]-[TOMBSTONE] Client wins, removing directory on server: %s\n", absolutePath)
					} else {
						log.Printf("[PROCESS SNAPSHOTS]-[TOMBSTONE] Client wins, removing file on server: %s\n", absolutePath)
					}
					log.Printf("[PROCESS SNAPSHOTS] Removing file: %s\n", absolutePath)
					// Deletes path or file
					err := utils.Rm(absolutePath)
					if err != nil {
						log.Printf("[PROCESS SNAPSHOTS] Error removing file: %s\n", err)
						return nil, err
					}
					// Else, send upload action to client
				} else {
					if !isDirectory {
						log.Printf("[PROCESS SNAPSHOTS] Client wins, sending upload action to client: %s\n", path)
						fileActions.Files[path] = utils.FileActionMetadata{
							Action: utils.ActionUpload,
						}
					}
				}
				// Tie
			case 0:
				clientFileStatus := clientFileMetadata.Status
				if clientFileStatus == utils.StatusDeleted {
					absolutePath := utils.RelToAbsConvert(syncDirectory, path)
					if isDirectory {
						log.Printf("[PROCESS SNAPSHOTS]-[TOMBSTONE] Tie, removing directory on server: %s\n", absolutePath)
					} else {
						log.Printf("[PROCESS SNAPSHOTS]-[TOMBSTONE] Tie, removing file on server: %s\n", absolutePath)
					}
					// Deletes path or file
					err := utils.Rm(absolutePath)
					if err != nil {
						return nil, err
					}
				} else {
					if !isDirectory {
						if clientFileMetadata.ContentHash != serverFileMetadata.ContentHash {
							log.Printf("[PROCESS SNAPSHOTS] Tie, file content hash mismatch, uploading to server: %s\n", path)
							fileActions.Files[path] = utils.FileActionMetadata{
								Action: utils.ActionUpload,
							}
						}
					}
				}
			default:
				log.Printf("[PROCESS SNAPSHOTS] NOT WORKING: %s\n", path)
			}
		} else if !existsOnServer && existsOnClient {
			if clientFileMetadata.IsDirectory {
				log.Printf("[PROCESS SNAPSHOTS] Directory exists only on client, creating on server: %s\n", path)
				absolutePath := utils.RelToAbsConvert(syncDirectory, path)
				utils.MkDir(absolutePath)
			} else {
				log.Printf("[PROCESS SNAPSHOTS] File only exists on client, upload to server: %s\n", path)
				fileActions.Files[path] = utils.FileActionMetadata{
					Action: utils.ActionUpload,
				}
			}
		} else if existsOnServer && !existsOnClient {
			if serverFileMetadata.IsDirectory {
				log.Printf("[PROCESS SNAPSHOTS] Directory exists only on server, action client to make directory: %s\n", path)
				fileActions.Files[path] = utils.FileActionMetadata{
					Action: utils.ActionMkdir,
				}
			} else {
				log.Printf("[PROCESS SNAPSHOTS] File exists only on server, action client to download from server: %s\n", path)
				fileActions.Files[path] = utils.FileActionMetadata{
					Action: utils.ActionDownload,
				}
			}
		}
	}

	return &fileActions, nil
}
