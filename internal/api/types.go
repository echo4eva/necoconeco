package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
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

type RemoveRequest struct {
	Path string `json:"path"`
}

type Response struct {
	Status int `json:"status"`
}

func Download(relativePath, serverURL, syncDirectory string) error {
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
