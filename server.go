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
