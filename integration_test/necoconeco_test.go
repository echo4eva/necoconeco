package integration_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"bytes"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

type TestLogConsumer struct {
	prefix string
}

type ClientConfig struct {
	ID         string
	Dockerfile string
	QueueName  string
}

type TestEnvironment struct {
	Network    *testcontainers.DockerNetwork
	RabbitMQ   *rabbitmq.RabbitMQContainer
	FileServer testcontainers.Container
	Clients    map[string]testcontainers.Container
}

func (tlc *TestLogConsumer) Accept(l testcontainers.Log) {
	fmt.Printf("[%s] - %s", tlc.prefix, l.Content)
}

func setupNetwork(ctx context.Context, t *testing.T) *testcontainers.DockerNetwork {
	netNetwork, err := network.New(ctx)
	require.NoError(t, err)
	return netNetwork
}

func setupRabbitMQ(ctx context.Context, t *testing.T, netNetwork *testcontainers.DockerNetwork) *rabbitmq.RabbitMQContainer {
	rabbitContainer, err := rabbitmq.Run(
		ctx,
		"rabbitmq:4.0-management",
		rabbitmq.WithAdminUsername("guest"),
		rabbitmq.WithAdminPassword("guest"),
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{"rabbitmq-diagnostics", "check_port_connectivity"}).
				WithStartupTimeout(10*time.Second).
				WithPollInterval(2*time.Second),
		),
		network.WithNetwork([]string{"rabbitmq"}, netNetwork),
		testcontainers.WithLogConsumers(&TestLogConsumer{
			prefix: "rabbitmq",
		}),
	)
	require.NoError(t, err)
	return rabbitContainer
}

func setupFileServer(ctx context.Context, t *testing.T, netNetwork *testcontainers.DockerNetwork) testcontainers.Container {

	env := map[string]string{
		"CLIENT_ID":              "server-pub",
		"SYNC_DIRECTORY":         "/app/storage",
		"RABBITMQ_ADDRESS":       "amqp://guest:guest@rabbitmq:5672/",
		"RABBITMQ_EXCHANGE_NAME": "exchange",
		"RABBITMQ_ROUTING_KEY":   "routing.key",
		"PORT":                   "8080",
	}

	fileServerContainer, err := testcontainers.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(
			testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "integration_test/server.Dockerfile",
			},
		),
		testcontainers.WithExposedPorts("8080/tcp"),
		testcontainers.WithEnv(env),
		network.WithNetwork([]string{"file-server"}, netNetwork),
		testcontainers.WithLogConsumers(&TestLogConsumer{
			prefix: "file-server",
		}),
	)
	require.NoError(t, err)
	return fileServerContainer
}

func setupClient(ctx context.Context, t *testing.T, netNetwork *testcontainers.DockerNetwork, config ClientConfig) testcontainers.Container {
	env := map[string]string{
		"CLIENT_ID":              config.ID,
		"RABBITMQ_ADDRESS":       "amqp://guest:guest@rabbitmq:5672/",
		"RABBITMQ_EXCHANGE_NAME": "exchange",
		"RABBITMQ_QUEUE_NAME":    config.QueueName,
		"RABBITMQ_ROUTING_KEY":   "routing.key",
		"SYNC_DIRECTORY":         "/app/sync",
		"SYNC_SERVER_URL":        "http://file-server:8080",
	}

	container, err := testcontainers.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(
			testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "integration_test/" + config.Dockerfile,
			},
		),
		testcontainers.WithEnv(env),
		testcontainers.WithLogConsumers(&TestLogConsumer{
			prefix: config.ID,
		}),
		network.WithNetwork([]string{config.ID}, netNetwork),
	)
	require.NoError(t, err)
	return container
}

// setupTestEnvironment creates the basic infrastructure needed for all tests
func setupTestEnvironment(ctx context.Context, t *testing.T) *TestEnvironment {
	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	return &TestEnvironment{
		Network:    netNetwork,
		RabbitMQ:   rabbitContainer,
		FileServer: fileServerContainer,
		Clients:    make(map[string]testcontainers.Container),
	}
}

// addClient adds a new client to the test environment
func (env *TestEnvironment) addClient(ctx context.Context, t *testing.T, config ClientConfig) {
	client := setupClient(ctx, t, env.Network, config)
	testcontainers.CleanupContainer(t, client)
	env.Clients[config.ID] = client
}

// waitForSync provides a consistent wait time for sync operations
func waitForSync() {
	time.Sleep(3 * time.Second)
}

// waitForLongSync provides a longer wait time for complex sync operations
func waitForLongSync() {
	time.Sleep(10 * time.Second)
}

// FileOperations provides a consistent interface for file operations and assertions
type FileOperations struct {
	t   *testing.T
	ctx context.Context
}

func NewFileOps(t *testing.T, ctx context.Context) *FileOperations {
	return &FileOperations{t: t, ctx: ctx}
}

func (fo *FileOperations) createFile(container testcontainers.Container, filePath string) {
	touchCmd := []string{"touch", filePath}
	exitCode, _, err := container.Exec(fo.ctx, touchCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode)
}

func (fo *FileOperations) writeToFile(container testcontainers.Container, filePath, content string) {
	writeCmd := []string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", content, filePath)}
	exitCode, _, err := container.Exec(fo.ctx, writeCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode)
}

func (fo *FileOperations) renameFile(container testcontainers.Container, oldPath, newPath string) {
	renameCmd := []string{"mv", oldPath, newPath}
	exitCode, _, err := container.Exec(fo.ctx, renameCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode)
}

func (fo *FileOperations) createDirectory(container testcontainers.Container, dirPath string) {
	mkdirCmd := []string{"mkdir", dirPath}
	exitCode, _, err := container.Exec(fo.ctx, mkdirCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode)
}

func (fo *FileOperations) deleteFile(container testcontainers.Container, filePath string) {
	rmCmd := []string{"rm", "-f", filePath}
	exitCode, _, err := container.Exec(fo.ctx, rmCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode)
}

func (fo *FileOperations) deleteDirectory(container testcontainers.Container, dirPath string) {
	rmDirCmd := []string{"rm", "-rf", dirPath}
	exitCode, _, err := container.Exec(fo.ctx, rmDirCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode)
}

func (fo *FileOperations) assertFileExists(container testcontainers.Container, path string) {
	checkCmd := []string{"test", "-f", path}
	exitCode, _, err := container.Exec(fo.ctx, checkCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode, "File not found: %s", path)
}

func (fo *FileOperations) assertDirectoryExists(container testcontainers.Container, path string) {
	checkCmd := []string{"test", "-d", path}
	exitCode, _, err := container.Exec(fo.ctx, checkCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode, "Directory not found: %s", path)
}

func (fo *FileOperations) assertFileContent(container testcontainers.Container, path, expected string) {
	catCmd := []string{"cat", path}
	_, reader, err := container.Exec(fo.ctx, catCmd)
	require.NoError(fo.t, err)

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
	require.NoError(fo.t, err)
	actual := strings.TrimSpace(stdout.String())
	require.Equal(fo.t, expected, actual, "File content mismatch for %s", path)
}

func (fo *FileOperations) assertNotExists(container testcontainers.Container, path string) {
	checkCmd := []string{"test", "-e", path}
	exitCode, _, err := container.Exec(fo.ctx, checkCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 1, exitCode, "Should not exist: %s", path)
}

func (fo *FileOperations) setLastModified(container testcontainers.Container, filePath, timestamp string) {
	touchCmd := []string{"touch", "-d", timestamp, filePath}
	exitCode, _, err := container.Exec(fo.ctx, touchCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode)
}

func (fo *FileOperations) createFileWithContent(container testcontainers.Container, filePath, content string) {
	writeCmd := []string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", content, filePath)}
	exitCode, _, err := container.Exec(fo.ctx, writeCmd)
	require.NoError(fo.t, err)
	require.Equal(fo.t, 0, exitCode)
}

func TestFileSyncBetweenClients(t *testing.T) {
	ctx := context.Background()

	env := setupTestEnvironment(ctx, t)

	env.addClient(ctx, t, ClientConfig{
		ID:         "t-client-1",
		Dockerfile: "clientOnly.Dockerfile",
		QueueName:  "queue-1",
	})
	env.addClient(ctx, t, ClientConfig{
		ID:         "t-client-2",
		Dockerfile: "clientOnly.Dockerfile",
		QueueName:  "queue-2",
	})

	firstClient := env.Clients["t-client-1"]
	secondClient := env.Clients["t-client-2"]

	// 1. Create a file in the first client's sync directory
	fileName := "testfile.md"
	filePath := fmt.Sprintf("/app/sync/%s", fileName)
	fo := NewFileOps(t, ctx)
	fo.createFile(firstClient, filePath)
	waitForSync()

	// Assert file exists on file server and client 2
	fo.assertFileExists(env.FileServer, "/app/storage/"+fileName)
	fo.assertFileExists(secondClient, filePath)

	// 2. Write to the file
	content := "hello world"
	fo.writeToFile(firstClient, filePath, content)
	waitForLongSync()

	// Assert file content on file server and client 2
	fo.assertFileContent(env.FileServer, "/app/storage/"+fileName, content)
	fo.assertFileContent(secondClient, filePath, content)

	// 3. Rename the file
	newFileName := "renamed.md"
	newFilePath := fmt.Sprintf("/app/sync/%s", newFileName)
	fo.renameFile(firstClient, filePath, newFilePath)
	waitForSync()

	// Assert new file exists, old does not
	fo.assertFileExists(env.FileServer, "/app/storage/"+newFileName)
	fo.assertNotExists(secondClient, filePath)
	fo.assertNotExists(env.FileServer, "/app/storage/"+fileName)

	// 4. Create a directory
	dirName := "mydir"
	dirPath := fmt.Sprintf("/app/sync/%s", dirName)
	fo.createDirectory(firstClient, dirPath)
	waitForSync()

	// Assert directory exists
	fo.assertDirectoryExists(env.FileServer, "/app/storage/"+dirName)
	fo.assertDirectoryExists(secondClient, dirPath)

	// 5. Move the file into the directory
	movedFilePath := fmt.Sprintf("%s/%s", dirPath, newFileName)
	fo.renameFile(firstClient, newFilePath, movedFilePath)
	waitForSync()

	// Assert file is in directory
	fo.assertFileExists(env.FileServer, fmt.Sprintf("/app/storage/%s/%s", dirName, newFileName))
	fo.assertFileExists(secondClient, movedFilePath)
	fo.assertNotExists(env.FileServer, "/app/storage/"+newFileName)
	fo.assertNotExists(secondClient, newFilePath)

	// 6. Rename the directory
	newDirName := "renameddir"
	newDirPath := fmt.Sprintf("/app/sync/%s", newDirName)
	fo.renameFile(firstClient, dirPath, newDirPath)
	waitForSync()

	// Assert new directory and file location
	fo.assertDirectoryExists(env.FileServer, "/app/storage/"+newDirName)
	fo.assertDirectoryExists(secondClient, newDirPath)
	fo.assertFileExists(env.FileServer, fmt.Sprintf("/app/storage/%s/%s", newDirName, newFileName))
	fo.assertFileExists(secondClient, fmt.Sprintf("%s/%s", newDirPath, newFileName))
	fo.assertNotExists(env.FileServer, fmt.Sprintf("/app/storage/%s/%s", dirName, newFileName))
	fo.assertNotExists(secondClient, movedFilePath)
	fo.assertNotExists(env.FileServer, "/app/storage/"+dirName)
	fo.assertNotExists(secondClient, dirPath)

	// 7. Delete the directory
	fo.deleteDirectory(firstClient, newDirPath)
	waitForSync()

	// Assert directory and file are gone
	fo.assertNotExists(env.FileServer, "/app/storage/"+newDirName)
	fo.assertNotExists(secondClient, newDirPath)
	fo.assertNotExists(env.FileServer, fmt.Sprintf("/app/storage/%s/%s", newDirName, newFileName))
	fo.assertNotExists(secondClient, fmt.Sprintf("%s/%s", newDirPath, newFileName))
}

func TestWriteDebouncerRemoval(t *testing.T) {
	ctx := context.Background()

	env := setupTestEnvironment(ctx, t)

	env.addClient(ctx, t, ClientConfig{
		ID:         "debouncer-pub",
		Dockerfile: "clientOnly.Dockerfile",
		QueueName:  "debouncer-queue-1",
	})
	env.addClient(ctx, t, ClientConfig{
		ID:         "debouncer-rec",
		Dockerfile: "clientOnly.Dockerfile",
		QueueName:  "debouncer-queue-2",
	})

	publishingClient := env.Clients["debouncer-pub"]
	receivingClient := env.Clients["debouncer-rec"]

	fileName := "edge.md"
	filePath := "/app/sync/" + fileName

	// 1. Publishing client creates a file named "edge.md"
	fo := NewFileOps(t, ctx)
	fo.createFile(publishingClient, filePath)
	waitForSync()

	// Wait for sync
	waitForSync()

	// Assert file exists on file server and receiving client
	fo.assertFileExists(env.FileServer, "/app/storage/"+fileName)
	fo.assertFileExists(receivingClient, filePath)

	// 2. Publishing client writes to the file but quickly renames it
	content := "debouncer test 1"
	fo.writeToFile(publishingClient, filePath, content)
	renamedFileName := "renamed_edge.md"
	renamedFilePath := "/app/sync/" + renamedFileName
	fo.renameFile(publishingClient, filePath, renamedFilePath)

	// Wait for sync
	waitForLongSync()

	// Assert old file does not exist, new file exists on both
	fo.assertNotExists(env.FileServer, "/app/storage/"+fileName)
	fo.assertNotExists(receivingClient, filePath)
	fo.assertFileExists(env.FileServer, "/app/storage/"+renamedFileName)
	fo.assertFileExists(receivingClient, renamedFilePath)

	// 3. Publishing client writes to renamed file, then quickly deletes it
	content2 := "debouncer test 2"
	fo.writeToFile(publishingClient, renamedFilePath, content2)
	fo.deleteFile(publishingClient, renamedFilePath)

	// Wait for sync
	waitForLongSync()

	// Assert file is deleted on both
	fo.assertNotExists(env.FileServer, "/app/storage/"+renamedFileName)
	fo.assertNotExists(receivingClient, renamedFilePath)
}

func TestClientSyncNecoshot(t *testing.T) {
	ctx := context.Background()

	env := setupTestEnvironment(ctx, t)

	// Create files on server with content
	fo := NewFileOps(t, ctx)
	fo.createFileWithContent(env.FileServer, "/app/storage/client_win_upload.md", "filler for compare")
	fo.createFileWithContent(env.FileServer, "/app/storage/client_win_delete.md", "should be gone")
	fo.createFileWithContent(env.FileServer, "/app/storage/client_lose_delete.md", "server win")
	fo.createFileWithContent(env.FileServer, "/app/storage/client_tie_delete.md", "tie delete")
	fo.createFileWithContent(env.FileServer, "/app/storage/client_tie_win.md", "filler for compare")

	// Create directories on server
	fo.createDirectory(env.FileServer, "/app/storage/server_subdir")
	fo.writeToFile(env.FileServer, "/app/storage/server_subdir/server.md", "Client should download this")
	fo.createDirectory(env.FileServer, "/app/storage/server_empty_subdir")
	fo.createDirectory(env.FileServer, "/app/storage/client_win_delete_subdir")
	fo.createDirectory(env.FileServer, "/app/storage/client_tie_delete_subdir")

	// Notes for integration tests:
	// client_win_upload.md on client Last modified is 2025-01-01 00:00:01
	// client_win_delete.md on client Last modified is 2025-01-01 00:00:01
	// client_lose_delete.md on client Last modified is 2025-01-01 00:00:00
	// Forge Last modified file dates:
	fo.setLastModified(env.FileServer, "/app/storage/client_win_upload.md", "2025-01-01 00:00:00")
	fo.setLastModified(env.FileServer, "/app/storage/client_win_delete.md", "2025-01-01 00:00:00")
	fo.setLastModified(env.FileServer, "/app/storage/client_lose_delete.md", "2025-01-01 00:00:01")
	fo.setLastModified(env.FileServer, "/app/storage/client_win_delete_subdir", "2025-01-01 00:00:00")
	fo.setLastModified(env.FileServer, "/app/storage/client_tie_delete.md", "2025-01-01 00:00:00")
	fo.setLastModified(env.FileServer, "/app/storage/client_tie_delete_subdir", "2025-01-01 00:00:00")
	fo.setLastModified(env.FileServer, "/app/storage/client_tie_win.md", "2025-01-01 00:00:00")

	// Create clientsync container
	env.addClient(ctx, t, ClientConfig{
		ID:         "clientsync-necoshot",
		Dockerfile: "clientSync.Dockerfile",
		QueueName:  "sync-queue-1",
	})
	clientContainer := env.Clients["clientsync-necoshot"]

	// Wait for sync to complete
	waitForSync()

	// Check file server
	fo.assertFileContent(env.FileServer, "/app/storage/client_win_upload.md", "client win")
	fo.assertNotExists(env.FileServer, "/app/storage/client_win_delete.md")
	fo.assertFileExists(env.FileServer, "/app/storage/client_lose_delete.md")
	fo.assertDirectoryExists(env.FileServer, "/app/storage/client_subdir")
	fo.assertFileExists(env.FileServer, "/app/storage/client_subdir/client_regular_upload.md")
	fo.assertNotExists(env.FileServer, "/app/storage/client_win_delete_subdir")
	fo.assertNotExists(env.FileServer, "/app/storage/client_tie_delete.md")
	fo.assertNotExists(env.FileServer, "/app/storage/client_tie_delete_subdir")
	fo.assertFileContent(env.FileServer, "/app/storage/client_tie_win.md", "client tie win")
	// Check client
	fo.assertNotExists(clientContainer, "/app/sync/client_win_delete.md")
	fo.assertFileContent(clientContainer, "/app/sync/client_lose_delete.md", "server win")
	fo.assertDirectoryExists(clientContainer, "/app/sync/server_subdir")
	fo.assertFileExists(clientContainer, "/app/sync/server_subdir/server.md")
}

func TestClientConsumingSync(t *testing.T) {
	ctx := context.Background()

	env := setupTestEnvironment(ctx, t)

	fo := NewFileOps(t, ctx)

	// Client A
	// Is initially empty, as is in a the consuming state
	env.addClient(ctx, t, ClientConfig{
		ID:         "consuming-sync-client",
		Dockerfile: "clientConsumingSync.Dockerfile",
		QueueName:  "sync-queue-1",
	})
	consumingClient := env.Clients["consuming-sync-client"]

	waitForSync()

	// Client B
	// Is filled with files needed to be synced
	env.addClient(ctx, t, ClientConfig{
		ID:         "syncing-client",
		Dockerfile: "clientSync.Dockerfile",
		QueueName:  "sync-queue-2",
	})

	// Client A should receive message from file server (publisher)
	// As Client B syncs with the file server, the file server will be publishing messages to the queue
	// NOTE: should see errors "failed to upload file" in logs, but that is expected
	// because file server and client snapshots aren't in sync in this test,
	// file server is fresh, client is "old."
	waitForLongSync()

	// Check if consuming client (Client A) has consumed syncing client (Client B)
	// these files listed/ are listed in clientSync.Dockerfile
	fo.assertDirectoryExists(consumingClient, "/app/sync/client_subdir")
	fo.assertDirectoryExists(consumingClient, "/app/sync/client_empty_subdir")
	fo.assertFileExists(consumingClient, "/app/sync/client_subdir/client_regular_upload.md")
	fo.assertFileExists(consumingClient, "/app/sync/client_win_upload.md")
	fo.assertFileExists(consumingClient, "/app/sync/client_tie_win.md")
}
