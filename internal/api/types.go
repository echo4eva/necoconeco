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

// Message represents the common message structure used for RabbitMQ communication
type Message struct {
	ClientID string `json:"client_id"`
	Event    string `json:"event"`
	Path     string `json:"path"`
	FileURL  string `json:"file_url"`
	OldPath  string `json:"old_path,omitempty"`
}

// API service object for handling all API interactions
type API struct {
	serverURL     string
	syncDirectory string
}

// NewAPI creates a new API client instance
func NewAPI(serverURL, syncDirectory string) *API {
	return &API{
		serverURL:     serverURL,
		syncDirectory: syncDirectory,
	}
}

type MetadataResponse struct {
	Response
	*utils.DirectoryMetadata
}

type PostSnapshotResponse struct {
	Response
	*utils.SyncActionMetadata
}

type PostSnapshotRequest struct {
	ClientID      string                   `json"client_id"`
	FinalSnapshot *utils.DirectoryMetadata `json:"final_snapshot"`
}

type UploadResponse struct {
	Response
	FileURL string `json:"file_url"`
}

type CreateDirectoryRequest struct {
	ClientID  string `json:"client_id"`
	Directory string `json:"directory"`
}

type RenameRequest struct {
	ClientID string `json:"client_id"`
	OldName  string `json:"old_name"`
	NewName  string `json:"new_name"`
}

type UploadRequest struct {
	Path string `json:"path"`
}

type RemoveRequest struct {
	ClientID string `json:"client_id"`
	Path     string `json:"path"`
}

type Response struct {
	Status int `json:"status"`
}

// Download downloads a file from the server using a normalized (relative) path
// and saves it to the local filesystem using a denormalized (absolute) path
func (api *API) Download(normalizedPath string) error {
	// Convert normalized path to denormalized (absolute) path for local file system
	denormalizedPath := utils.RelToAbsConvert(api.syncDirectory, normalizedPath)

	directory := utils.GetOnlyDir(denormalizedPath)
	utils.MkDir(directory)

	// Create file locally
	out, err := os.Create(denormalizedPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Download from file-server using normalized path
	downloadURL := fmt.Sprintf("%s/files/%s", api.serverURL, normalizedPath)
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

// Upload uploads a file to the server, taking a denormalized (absolute) path
// and converting it to a normalized (relative) path for server communication
func (api *API) Upload(denormalizedPath, clientID string) (*UploadResponse, error) {
	file, err := os.Open(denormalizedPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to open file %w", err)
	}
	defer file.Close()

	var requestBody bytes.Buffer
	multiWriter := multipart.NewWriter(&requestBody)

	// Convert denormalized (absolute) path to normalized (relative) path for server
	normalizedPath := utils.AbsToRelConvert(api.syncDirectory, denormalizedPath)

	// Write fields to multipart writer
	err = multiWriter.WriteField("path", normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to write field %s: %w", "path", err)
	}

	err = multiWriter.WriteField("client_id", clientID)
	if err != nil {
		return nil, fmt.Errorf("Failed to write field %s: %w", "client_id", err)
	}

	// Create form file for file upload
	fileWriter, err := multiWriter.CreateFormFile("file", filepath.Base(denormalizedPath))
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

	uploadURL := fmt.Sprintf("%s/upload", api.serverURL)
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

// RemoteMkdir creates a directory on the server using a denormalized (absolute) path
// which is converted to a normalized (relative) path for server communication
func (api *API) RemoteMkdir(denormalizedPath, clientID string) error {
	createDirectoryURL := fmt.Sprintf("%s/directory", api.serverURL)

	// Convert denormalized (absolute) path to normalized (relative) path for server
	normalizedPath := utils.AbsToRelConvert(api.syncDirectory, denormalizedPath)

	payload := CreateDirectoryRequest{
		ClientID:  clientID,
		Directory: normalizedPath,
	}

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

// RemoteRename renames a file/directory on the server using denormalized (absolute) paths
// which are converted to normalized (relative) paths for server communication
func (api *API) RemoteRename(denormalizedOldName, denormalizedNewName, clientID string) error {
	renameURL := fmt.Sprintf("%s/rename", api.serverURL)

	// Convert denormalized (absolute) paths to normalized (relative) paths for server
	normalizedOldName := utils.AbsToRelConvert(api.syncDirectory, denormalizedOldName)
	normalizedNewName := utils.AbsToRelConvert(api.syncDirectory, denormalizedNewName)

	payload := RenameRequest{
		ClientID: clientID,
		OldName:  normalizedOldName,
		NewName:  normalizedNewName,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("Failed to marshal data: %w", err)
	}

	bodyReader := bytes.NewReader(jsonData)

	req, err := http.NewRequest(http.MethodPost, renameURL, bodyReader)
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

// RemoteRemove removes a file/directory on the server using a denormalized (absolute) path
// which is converted to a normalized (relative) path for server communication
func (api *API) RemoteRemove(denormalizedPath, clientID string) error {
	removeURL := fmt.Sprintf("%s/remove", api.serverURL)

	// Convert denormalized (absolute) path to normalized (relative) path for server
	normalizedPath := utils.AbsToRelConvert(api.syncDirectory, denormalizedPath)

	payload := RemoveRequest{
		ClientID: clientID,
		Path:     normalizedPath,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("Failed to marshal data: %w", err)
	}

	bodyReader := bytes.NewReader(jsonData)

	req, err := http.NewRequest(http.MethodPost, removeURL, bodyReader)
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
