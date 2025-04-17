//go:build server
// +build server

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type UploadResponse struct {
	Status  string `json:"status"`
	FileURL string `json:"file_url"`
}

func main() {
	fs := http.FileServer(http.Dir("./storage"))
	http.Handle("/files/", http.StripPrefix("/files/", fs))

	http.HandleFunc("/upload", uploadHandler)

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

	r.ParseMultipartForm(10 << 20)

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	os.MkdirAll("./storage", os.ModePerm)

	dst, err := os.Create(filepath.Join("./storage/", handler.Filename))
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

	fileURL := fmt.Sprintf("http://%s/files/%s", r.Host, handler.Filename)

	w.Header().Set("Content-Type", "application/json")
	response := UploadResponse{
		Status:  "good",
		FileURL: fileURL,
	}
	json.NewEncoder(w).Encode(response)
}
