package api

import (
	"github.com/echo4eva/necoconeco/internal/utils"
)

type MetadataResponse struct {
	Response
	Files map[string]utils.FileMetadata `json:"files"`
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
