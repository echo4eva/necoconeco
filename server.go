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
)

type UploadResponse struct {
	Response
	FileURL string `json:"file_url"`
}

type CreateDirectoryRequest struct {
	Directory string `json:"directory"`
}

type Response struct {
	Status int `json:"status"`
}

var (
	syncDirectory = "/app/storage"
)

func main() {
	fs := http.FileServer(http.Dir("./storage"))
	http.Handle("/files/", http.StripPrefix("/files/", fs))

	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/directory", directoryHandler)

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

func absoluteConvert(relativePath string) string {
	log.Printf("[FILE SERVER] relative path: %s\n", relativePath)
	return syncDirectory + relativePath
}
