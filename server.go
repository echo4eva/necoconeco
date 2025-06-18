//go:build server
// +build server

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/echo4eva/necoconeco/internal/api"
	"github.com/echo4eva/necoconeco/internal/utils"
	"github.com/joho/godotenv"
	rmq "github.com/rabbitmq/rabbitmq-amqp-go-client/pkg/rabbitmqamqp"
)

type Message struct {
	ClientID string `json:"client_id"`
	Event    string `json:"event"`
	Path     string `json:"path"`
	FileURL  string `json:"file_url"`
	OldPath  string `json:"old_path,omitempty"`
}

type Server struct {
	publisher     *rmq.Publisher
	syncDirectory string
	clientID      string
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	server := &Server{}

	server.clientID = os.Getenv("CLIENT_ID")
	server.syncDirectory = os.Getenv("SYNC_DIRECTORY")
	address := os.Getenv("RABBITMQ_ADDRESS")
	exchangeName := os.Getenv("RABBITMQ_EXHANGE_NAME")
	routingKey := os.Getenv("RABBITMQ_ROUTING_KEY")

	env := rmq.NewEnvironment(address, nil)
	defer env.CloseConnections(context.Background())

	amqpConnection, err := env.NewConnection(context.Background())
	if err != nil {
		rmq.Error("Failed to create new connection")
		return
	}
	defer amqpConnection.Close(context.Background())

	management := amqpConnection.Management()
	defer management.Close(context.Background())

	_, err = management.DeclareExchange(context.Background(), &rmq.TopicExchangeSpecification{
		Name:         exchangeName,
		IsAutoDelete: false,
	})
	if err != nil {
		rmq.Error("Failed to declare exchange", err)
		return
	}

	server.publisher, err = amqpConnection.NewPublisher(context.Background(), &rmq.ExchangeAddress{
		Exchange: exchangeName,
		Key:      routingKey,
	}, nil)
	if err != nil {
		rmq.Error("Failed to create new publisher", err)
		return
	}
	defer server.publisher.Close(context.Background())

	fs := http.FileServer(http.Dir("./storage"))
	http.Handle("/files/", http.StripPrefix("/files/", fs))

	http.HandleFunc("/upload", server.uploadHandler)
	http.HandleFunc("/directory", server.directoryHandler)
	http.HandleFunc("/rename", server.renameHandler)
	http.HandleFunc("/remove", server.removeHandler)
	http.HandleFunc("/metadata", server.metadataHandler)
	http.HandleFunc("/snapshot", server.snapshotHandler)

	fmt.Println("Server started at :8080")
	err = http.ListenAndServe("0.0.0.0:8080", nil)
	if err != nil {
		fmt.Println("someshit happened %w", err)
	}
}

func (s *Server) publish(message *Message) error {
	jsonData, err := json.Marshal(message)
	if err != nil {
		return err
	}

	publishResult, err := s.publisher.Publish(context.Background(), rmq.NewMessage(jsonData))
	if err != nil {
		return err
	}
	rmq.Info("[PUBLISHER] SENDING MESSAGE")

	// ACKs and NACKs from broker to publisher
	switch publishResult.Outcome.(type) {
	case *rmq.StateAccepted:
		rmq.Info("[PUBLISHER] Message accepted", publishResult.Message.Data[0])
	case *rmq.StateRejected:
		rmq.Info("[PUBLISHER] Message rejected", publishResult.Message.Data[0])
	case *rmq.StateReleased:
		rmq.Info("[PUBLISHER] Message released", publishResult.Message.Data[0])
	}

	return nil
}

func (s *Server) handleUpload(clientID, path string) error {
	message := &Message{
		ClientID: clientID,
		Event:    "CREATE",
		Path:     path,
	}

	err := s.publish(message)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relativePath := r.FormValue("path")
	clientID := r.FormValue("client_id")

	r.ParseMultipartForm(10 << 20)

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	absolutePath := utils.RelToAbsConvert(s.syncDirectory, relativePath)
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

	err = s.handleUpload(clientID, relativePath)
	if err != nil {
		http.Error(w, "Error handling upload", http.StatusInternalServerError)
		log.Printf("Error handling upload: %s\n", err)
	}

	w.Header().Set("Content-Type", "application/json")
	response := api.UploadResponse{
		Response: api.Response{
			http.StatusOK,
		},
		FileURL: fileURL,
	}
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleDirectory(clientID, absolutePath string) error {
	if err := utils.MkDir(absolutePath); err != nil {
		return err
	}

	message := &Message{
		ClientID: clientID,
		Event:    "CREATE",
		Path:     utils.AbsToRelConvert(s.syncDirectory, absolutePath),
	}

	err := s.publish(message)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) directoryHandler(w http.ResponseWriter, r *http.Request) {
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
		clientID := reqPayload.ClientID

		log.Printf("[DIRECTORY HANDLER] %s\n", reqPayload.Directory)
		absolutePath := utils.RelToAbsConvert(s.syncDirectory, relativeDirectory)
		err = s.handleDirectory(clientID, absolutePath)
		if err != nil {
			log.Printf("Error handling directory: %s\n", err)
			http.Error(w, "Error creating direcgtory", http.StatusInternalServerError)
			return
		}

		response := api.Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func (s *Server) handleRename(clientID, oldName, newName string) error {
	if err := utils.Rename(oldName, newName); err != nil {
		return err
	}

	message := &Message{
		ClientID: clientID,
		Event:    "NECO_RENAME",
		Path:     utils.AbsToRelConvert(s.syncDirectory, newName),
		OldPath:  utils.AbsToRelConvert(s.syncDirectory, oldName),
	}

	err := s.publish(message)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) renameHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var reqPayload api.RenameRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		clientID := reqPayload.ClientID
		oldName := reqPayload.OldName
		newName := reqPayload.NewName
		absoluteOld := utils.RelToAbsConvert(s.syncDirectory, oldName)
		absoluteNew := utils.RelToAbsConvert(s.syncDirectory, newName)
		log.Printf("[RENAME HANDLER] Old: %s New: %s\n", oldName, newName)

		err = s.handleRename(clientID, absoluteOld, absoluteNew)
		if err != nil {
			log.Printf("Error handling rename: %s\n", err)
			http.Error(w, fmt.Sprintf("Error renaming, old: %s | new: %s", absoluteOld, absoluteNew), http.StatusInternalServerError)
			return
		}

		response := api.Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func (s *Server) handleRemove(clientID, absolutePath string) error {
	if err := utils.Rm(absolutePath); err != nil {
		return err
	}

	message := &Message{
		ClientID: clientID,
		Event:    "REMOVE",
		Path:     utils.AbsToRelConvert(s.syncDirectory, absolutePath),
	}

	if err := s.publish(message); err != nil {
		return err
	}

	return nil
}

func (s *Server) removeHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var reqPayload api.RemoveRequest

		err := json.NewDecoder(r.Body).Decode(&reqPayload)
		if err != nil {
			http.Error(w, "Error decoding json", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		clientID := reqPayload.ClientID
		path := reqPayload.Path
		absolutePath := utils.RelToAbsConvert(s.syncDirectory, path)

		log.Printf("[REMOVE HANDLER] To remove: %s\n", absolutePath)
		err = s.handleRemove(clientID, absolutePath)
		if err != nil {
			log.Printf("Error handling remove %s\n", err)
			http.Error(w, "Error removing", http.StatusInternalServerError)
			return
		}

		response := api.Response{
			http.StatusOK,
		}
		json.NewEncoder(w).Encode(response)
	}
}

func (s *Server) metadataHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		log.Println("Received metadata request from client")
		response := api.MetadataResponse{
			DirectoryMetadata: &utils.DirectoryMetadata{},
			Response:          api.Response{},
		}

		_, err := os.Stat(s.syncDirectory)
		if err != nil {
			http.Error(w, "Error sync directory not initialized", http.StatusInternalServerError)
			return
		}

		var localMetadata *utils.DirectoryMetadata
		localMetadata, err = utils.GetLocalMetadata(s.syncDirectory)
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

func (s *Server) snapshotHandler(w http.ResponseWriter, r *http.Request) {
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

		clientID := reqPayload.ClientID
		clientSnapshot := reqPayload.FinalSnapshot

		_, err = os.Stat(s.syncDirectory)
		if err != nil {
			http.Error(w, "Error sync directory not initialized", http.StatusInternalServerError)
			return
		}

		var serverSnapshot *utils.DirectoryMetadata
		serverSnapshot, err = utils.GetLocalMetadata(s.syncDirectory)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error retrieving server local metadata: %s", err), http.StatusInternalServerError)
			return
		}
		log.Printf("Server snapshot: %+v\n", serverSnapshot)

		// Retrieve client actions by comparing snapshots
		clientFileActions, err := s.processSnapshots(serverSnapshot, clientSnapshot, clientID)
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

func (s *Server) processSnapshots(serverSnapshot, clientSnapshot *utils.DirectoryMetadata, clientID) (*utils.SyncActionMetadata, error) {
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
					if serverFileMetadata.ContentHash != clientFileMetadata.ContentHash {
						log.Printf("[PROCESS SNAPSHOTS] Server wins, sending download action to client: %s\n", path)
						fileActions.Files[path] = utils.FileActionMetadata{
							Action: utils.ActionDownload,
						}
					}
				}
			// Client wins
			case -1:
				clientFileStatus := clientFileMetadata.Status
				// If deleted on client, server needs to delete
				if clientFileStatus == utils.StatusDeleted {
					absolutePath := utils.RelToAbsConvert(s.syncDirectory, path)
					if isDirectory {
						log.Printf("[PROCESS SNAPSHOTS]-[TOMBSTONE] Client wins, removing directory on server: %s\n", absolutePath)
					} else {
						log.Printf("[PROCESS SNAPSHOTS]-[TOMBSTONE] Client wins, removing file on server: %s\n", absolutePath)
					}
					log.Printf("[PROCESS SNAPSHOTS] Removing file: %s\n", absolutePath)
					// Deletes path or file
					err := s.handleRemove(clientID, absolutePath)
					if err != nil {
						log.Printf("[PROCESS SNAPSHOTS] Error removing file: %s\n", err)
						return nil, err
					}
					// Else, send upload action to client
				} else {
					if !isDirectory {
						if serverFileMetadata.ContentHash != clientFileMetadata.ContentHash {
							log.Printf("[PROCESS SNAPSHOTS] Client wins, sending upload action to client: %s\n", path)
							fileActions.Files[path] = utils.FileActionMetadata{
								Action: utils.ActionUpload,
							}
						}
					}
				}
				// Tie
			case 0:
				clientFileStatus := clientFileMetadata.Status
				if clientFileStatus == utils.StatusDeleted {
					absolutePath := utils.RelToAbsConvert(s.syncDirectory, path)
					if isDirectory {
						log.Printf("[PROCESS SNAPSHOTS]-[TOMBSTONE] Tie, removing directory on server: %s\n", absolutePath)
					} else {
						log.Printf("[PROCESS SNAPSHOTS]-[TOMBSTONE] Tie, removing file on server: %s\n", absolutePath)
					}
					// Deletes path or file
					err := s.handleRemove(clientID, absolutePath)
					if err != nil {
						return nil, err
					}
				} else {
					if !isDirectory {
						if serverFileMetadata.ContentHash != clientFileMetadata.ContentHash {
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
				absolutePath := utils.RelToAbsConvert(s.syncDirectory, path)
				err = s.handleDirectory(clientID, absolutePath)
				if err != nil {
					return nil, err
				}
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
