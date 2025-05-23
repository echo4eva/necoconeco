package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	// "path/filepath"

	"github.com/echo4eva/necoconeco/internal/utils"
)

type MetadataResponse struct {
	Response
	*utils.DirectoryMetadata
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

type UploadRequest struct {
	Path string `json:"path"`
}

type RemoveRequest struct {
	Path string `json:"path"`
}

type Response struct {
	Status int `json:"status"`
}

func Download(relativePath, syncDirectory, serverURL string) error {
	absolutePath := utils.RelToAbsConvert(relativePath, syncDirectory)

	directory := utils.GetOnlyDir(absolutePath)
	utils.MkDir(directory)

	// Create file locally
	out, err := os.Create(absolutePath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Download from file-server
	downloadURL := fmt.Sprintf("http://%s/files/%s", serverURL, relativePath)
	resp, err := http.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	fmt.Printf("%s\n", downloadURL)

	// Copy data into file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func Upload(absolutePath, syncDirectory, serverURL string) (*UploadResponse, error) {
	file, err := os.Open(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to open file %w", err)
	}
	defer file.Close()

	var requestBody bytes.Buffer
	multiWriter := multipart.NewWriter(&requestBody)

	fields := map[string]string{
		"path": utils.AbsToRelConvert(syncDirectory, absolutePath),
	}
	for field, value := range fields {
		err = multiWriter.WriteField(field, value)
		if err != nil {
			return nil, fmt.Errorf("Failed to write field %s: %w", field, err)
		}
	}

	// Debug
	// fmt.Printf("[UPLOAD] abs: %s, rel: %s, sync: %s\n", absolutePath, utils.AbsToRelConvert(absolutePath, syncDirectory), syncDirectory)

	fileWriter, err := multiWriter.CreateFormFile("file", filepath.Base(absolutePath))
	if err != nil {
		return nil, fmt.Errorf("Failed to create form file %w:", err)
	}

	_, err = io.Copy(fileWriter, file)
	if err != nil {
		return nil, fmt.Errorf("Failed to copy file content %w", err)
	}

	err = multiWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("Failed to close multipart writer %w", err)
	}

	uploadURL := fmt.Sprintf("http://%s/upload", serverURL)
	request, err := http.NewRequest("POST", uploadURL, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("Failed to create request %w", err)
	}

	request.Header.Set("Content-Type", multiWriter.FormDataContentType())

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Failed to send request %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read response %w", err)
	}

	fmt.Printf("Upload successful: %s\n", string(responseBody))

	var uploadResponse UploadResponse
	err = json.Unmarshal(responseBody, &uploadResponse)
	if err != nil {
		return nil, fmt.Errorf("Failed to unmarshal response %w", err)
	}

	return &uploadResponse, nil
}

func RemoteMkdir(directory, syncDirectory, serverURL string) error {
	createDirectoryURL := fmt.Sprintf("http://%s/directory", serverURL)

	payload := CreateDirectoryRequest{
		Directory: utils.AbsToRelConvert(syncDirectory, directory),
	}

	// log.Printf("[REMOTE CREATE DIRECTORY] directory: %s payload.Directory: %s", directory, payload.Directory)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("Failed to marshal data: %w", err)
	}

	bodyReader := bytes.NewReader(jsonData)

	req, err := http.NewRequest(http.MethodPost, createDirectoryURL, bodyReader)
	if err != nil {
		return fmt.Errorf("Failed to create post request: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to do request: %w", err)
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("Failed to read response body: %w", err)
	}
	log.Printf("Code: %s Response: %s\n", res.Status, resBody)

	return nil
}
