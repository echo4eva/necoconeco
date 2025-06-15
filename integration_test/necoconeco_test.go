package integration_test

import (
	"context"
	"fmt"
	"log"
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
		"SYNC_SERVER_URL":        "file-server:8080",
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

func TestMain(t *testing.T) {
	ctx := context.Background()

	log.Printf("MAKING NETWORK\n")
	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	log.Printf("MAKING RABBIT CONTAINER\n")
	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	log.Printf("MAKING FILE SERVER CONTAINER\n")
	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	log.Printf("MAKING CONTAINER-1\n")
	firstContainer := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "t-client-1",
		Dockerfile: "client.Dockerfile",
		QueueName:  "queue-1",
	})
	testcontainers.CleanupContainer(t, firstContainer)

	log.Printf("MAKING CONTAINER-2\n")
	secondContainer := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "t-client-2",
		Dockerfile: "client.Dockerfile",
		QueueName:  "queue-2",
	})
	testcontainers.CleanupContainer(t, secondContainer)
}

func TestFileSyncBetweenClients(t *testing.T) {
	ctx := context.Background()

	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	firstClient := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "t-client-1",
		Dockerfile: "client.Dockerfile",
		QueueName:  "queue-1",
	})
	testcontainers.CleanupContainer(t, firstClient)

	secondClient := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "t-client-2",
		Dockerfile: "client.Dockerfile",
		QueueName:  "queue-2",
	})
	testcontainers.CleanupContainer(t, secondClient)

	// 1. Create a file in the first client's sync directory
	fileName := "testfile.md"
	filePath := fmt.Sprintf("/app/sync/%s", fileName)
	touchCmd := []string{"touch", filePath}
	exitCode, _, err := firstClient.Exec(ctx, touchCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	// Wait for sync
	time.Sleep(3 * time.Second)

	// Assert file exists on file server and client 2
	checkFile := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "-f", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "File not found: %s", path)
	}
	checkFile(fileServerContainer, "/app/storage/"+fileName)
	checkFile(secondClient, filePath)

	// 2. Write to the file
	content := "hello world"
	writeCmd := []string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", content, filePath)}
	exitCode, _, err = firstClient.Exec(ctx, writeCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	time.Sleep(20 * time.Second)

	// Assert file content on file server and client 2
	checkContent := func(container testcontainers.Container, path, expected string) {
		catCmd := []string{"cat", path}
		_, reader, err := container.Exec(ctx, catCmd)
		require.NoError(t, err)

		var stdout, stderr bytes.Buffer
		// Demultiplex the Docker stream
		_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
		require.NoError(t, err)
		actual := strings.TrimSpace(stdout.String())
		require.Equal(t, expected, actual, "File content mismatch for %s", path)
	}
	checkContent(fileServerContainer, "/app/storage/"+fileName, content)
	checkContent(secondClient, filePath, content)

	// 3. Rename the file
	newFileName := "renamed.md"
	newFilePath := fmt.Sprintf("/app/sync/%s", newFileName)
	renameCmd := []string{"mv", filePath, newFilePath}
	exitCode, _, err = firstClient.Exec(ctx, renameCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	time.Sleep(3 * time.Second)

	// Assert new file exists, old does not
	checkFile(fileServerContainer, "/app/storage/"+newFileName)
	checkFile(secondClient, newFilePath)
	// Old file should not exist
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/"+fileName)
	checkNotExist(t, ctx, secondClient, filePath)

	// 4. Create a directory
	dirName := "mydir"
	dirPath := fmt.Sprintf("/app/sync/%s", dirName)
	mkdirCmd := []string{"mkdir", dirPath}
	exitCode, _, err = firstClient.Exec(ctx, mkdirCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	time.Sleep(3 * time.Second)

	// Assert directory exists
	checkDir := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "-d", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "Directory not found: %s", path)
	}
	checkDir(fileServerContainer, "/app/storage/"+dirName)
	checkDir(secondClient, dirPath)

	// 5. Move the file into the directory
	movedFilePath := fmt.Sprintf("%s/%s", dirPath, newFileName)
	moveCmd := []string{"mv", newFilePath, movedFilePath}
	exitCode, _, err = firstClient.Exec(ctx, moveCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	time.Sleep(3 * time.Second)

	// Assert file is in directory
	checkFile(fileServerContainer, fmt.Sprintf("/app/storage/%s/%s", dirName, newFileName))
	checkFile(secondClient, movedFilePath)
	// Old file should not exist
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/"+newFileName)
	checkNotExist(t, ctx, secondClient, newFilePath)

	// 6. Rename the directory
	newDirName := "renameddir"
	newDirPath := fmt.Sprintf("/app/sync/%s", newDirName)
	renameDirCmd := []string{"mv", dirPath, newDirPath}
	exitCode, _, err = firstClient.Exec(ctx, renameDirCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	time.Sleep(3 * time.Second)

	// Assert new directory and file location
	checkDir(fileServerContainer, "/app/storage/"+newDirName)
	checkDir(secondClient, newDirPath)
	checkFile(fileServerContainer, fmt.Sprintf("/app/storage/%s/%s", newDirName, newFileName))
	checkFile(secondClient, fmt.Sprintf("%s/%s", newDirPath, newFileName))
	// Old directory and file should not exist
	checkNotExist(t, ctx, fileServerContainer, fmt.Sprintf("/app/storage/%s/%s", dirName, newFileName))
	checkNotExist(t, ctx, secondClient, movedFilePath)
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/"+dirName)
	checkNotExist(t, ctx, secondClient, dirPath)

	// 7. Delete the directory
	rmDirCmd := []string{"rm", "-rf", newDirPath}
	exitCode, _, err = firstClient.Exec(ctx, rmDirCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	time.Sleep(3 * time.Second)

	// Assert directory and file are gone
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/"+newDirName)
	checkNotExist(t, ctx, secondClient, newDirPath)
	checkNotExist(t, ctx, fileServerContainer, fmt.Sprintf("/app/storage/%s/%s", newDirName, newFileName))
	checkNotExist(t, ctx, secondClient, fmt.Sprintf("%s/%s", newDirPath, newFileName))
}

func TestSyncGoBehavior(t *testing.T) {
	ctx := context.Background()

	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	// Setup: file-server
	// 1. /app/storage/same.md (empty)
	// 2. /app/storage/different.md (with content)
	// 3. /app/storage/onServer/toBeDownloaded.md (with content)
	_, _, err := fileServerContainer.Exec(ctx, []string{"mkdir", "-p", "/app/storage/onServer"})
	require.NoError(t, err)
	_, _, err = fileServerContainer.Exec(ctx, []string{"touch", "/app/storage/same.md"})
	require.NoError(t, err)
	_, _, err = fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'server content' > /app/storage/different.md"})
	require.NoError(t, err)
	_, _, err = fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'download me' > /app/storage/onServer/toBeDownloaded.md"})
	require.NoError(t, err)

	// No client-side setup needed; handled by Dockerfile
	clientContainer := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "sync-client",
		Dockerfile: "clientBare.Dockerfile",
		QueueName:  "sync-queue-1",
	})
	testcontainers.CleanupContainer(t, clientContainer)

	time.Sleep(3 * time.Second)

	// Assertions
	// 1. same.md exists and is empty on both
	checkFile := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "-f", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "File not found: %s", path)
	}
	checkContent := func(container testcontainers.Container, path, expected string) {
		catCmd := []string{"cat", path}
		_, reader, err := container.Exec(ctx, catCmd)
		require.NoError(t, err)
		var stdout, stderr bytes.Buffer
		_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
		require.NoError(t, err)
		actual := strings.TrimSpace(stdout.String())
		require.Equal(t, expected, actual, "File content mismatch for %s", path)
	}
	checkDir := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "-d", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "Directory not found: %s", path)
	}

	// same.md
	log.Printf("Checking same.md\n")
	checkFile(fileServerContainer, "/app/storage/same.md")
	checkFile(clientContainer, "/app/sync/same.md")
	checkContent(fileServerContainer, "/app/storage/same.md", "")
	checkContent(clientContainer, "/app/sync/same.md", "")

	// different.md
	log.Printf("Checking different.md\n")
	checkFile(fileServerContainer, "/app/storage/different.md")
	checkFile(clientContainer, "/app/sync/different.md")
	checkContent(fileServerContainer, "/app/storage/different.md", "server content")
	checkContent(clientContainer, "/app/sync/different.md", "server content")

	// onClient and toBeDeleted.md should NOT exist on either
	log.Printf("Checking onClient and toBeDeleted.md\n")
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/onClient")
	checkNotExist(t, ctx, clientContainer, "/app/sync/onClient")
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/onClient/toBeDeleted.md")
	checkNotExist(t, ctx, clientContainer, "/app/sync/onClient/toBeDeleted.md")

	// onServer and toBeDownloaded.md should exist on both, with correct content
	log.Printf("Checking onServer and toBeDownloaded.md\n")
	checkDir(fileServerContainer, "/app/storage/onServer")
	checkDir(clientContainer, "/app/sync/onServer")
	checkFile(fileServerContainer, "/app/storage/onServer/toBeDownloaded.md")
	checkFile(clientContainer, "/app/sync/onServer/toBeDownloaded.md")
	checkContent(fileServerContainer, "/app/storage/onServer/toBeDownloaded.md", "download me")
	checkContent(clientContainer, "/app/sync/onServer/toBeDownloaded.md", "download me")
}

func TestColdSync(t *testing.T) {
	ctx := context.Background()

	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	// This client is set up by clientCold.Dockerfile with files in /sync
	coldClient := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "t-cold-client",
		Dockerfile: "clientCold.Dockerfile",
		QueueName:  "sync-queue-1",
	})
	testcontainers.CleanupContainer(t, coldClient)

	// Wait for cold sync to complete.
	// The duration might need adjustment based on actual sync time.
	// A more robust approach would be to check logs for a completion message
	// or check file existence in a loop with a timeout.
	log.Printf("Waiting for cold sync to complete...")
	time.Sleep(5 * time.Second)

	// Helper to check for file or directory existence on the file server
	checkPathOnServer := func(container testcontainers.Container, path string, isDir bool) {
		fullPath := "/app/storage/" + path // Files are stored under /app/storage on the server
		var checkCmd []string
		if isDir {
			checkCmd = []string{"test", "-d", fullPath}
		} else {
			checkCmd = []string{"test", "-f", fullPath}
		}
		log.Printf("Checking for %s on file server at %s", path, fullPath)
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err, fmt.Sprintf("Error executing check for %s on server", fullPath))
		require.Equal(t, 0, exitCode, "Path not found or not correct type on server: %s", fullPath)
	}

	// Assertions: Check if files from the client's /sync directory are on the file server
	checkPathOnServer(fileServerContainer, "one.md", false)
	checkPathOnServer(fileServerContainer, "folder", true)
	checkPathOnServer(fileServerContainer, "folder/two.md", false)

	log.Printf("Cold sync test completed successfully.")
}

func TestWriteDebouncerRemoval(t *testing.T) {
	ctx := context.Background()

	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	publishingClient := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "debouncer-pub",
		Dockerfile: "client.Dockerfile",
		QueueName:  "debouncer-queue-1",
	})
	testcontainers.CleanupContainer(t, publishingClient)

	receivingClient := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "debouncer-rec",
		Dockerfile: "client.Dockerfile",
		QueueName:  "debouncer-queue-2",
	})
	testcontainers.CleanupContainer(t, receivingClient)

	fileName := "edge.md"
	filePath := "/app/sync/" + fileName

	// 1. Publishing client creates a file named "edge.md"
	touchCmd := []string{"touch", filePath}
	exitCode, _, err := publishingClient.Exec(ctx, touchCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Wait for sync
	time.Sleep(3 * time.Second)

	// Assert file exists on file server and receiving client
	checkFile := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "-f", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "File not found: %s", path)
	}
	checkFile(fileServerContainer, "/app/storage/"+fileName)
	checkFile(receivingClient, filePath)

	// 2. Publishing client writes to the file but quickly renames it
	content := "debouncer test 1"
	writeCmd := []string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", content, filePath)}
	renamedFileName := "renamed_edge.md"
	renamedFilePath := "/app/sync/" + renamedFileName
	renameCmd := []string{"mv", filePath, renamedFilePath}

	// Write, then immediately rename
	exitCode, _, err = publishingClient.Exec(ctx, writeCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	exitCode, _, err = publishingClient.Exec(ctx, renameCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Wait for sync
	time.Sleep(10 * time.Second)

	// Assert old file does not exist, new file exists on both
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/"+fileName)
	checkNotExist(t, ctx, receivingClient, filePath)
	checkFile(fileServerContainer, "/app/storage/"+renamedFileName)
	checkFile(receivingClient, renamedFilePath)

	// 3. Publishing client writes to renamed file, then quickly deletes it
	content2 := "debouncer test 2"
	writeCmd2 := []string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", content2, renamedFilePath)}
	deleteCmd := []string{"rm", "-f", renamedFilePath}

	exitCode, _, err = publishingClient.Exec(ctx, writeCmd2)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	exitCode, _, err = publishingClient.Exec(ctx, deleteCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Wait for sync
	time.Sleep(10 * time.Second)

	// Assert file is deleted on both
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/"+renamedFileName)
	checkNotExist(t, ctx, receivingClient, renamedFilePath)
}

func TestInitialClientSynchronization(t *testing.T) {
	ctx := context.Background()

	// 1. Initialize the Core Services
	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	// 2. Simulate an Active User - Start publishing client
	publishingClient := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "publishing-client",
		Dockerfile: "client.Dockerfile",
		QueueName:  "pub-queue",
	})
	testcontainers.CleanupContainer(t, publishingClient)

	// Have publishing client create and modify files
	log.Printf("Publishing client creating files...")

	// Create file1.md
	file1Name := "file1.md"
	file1Path := fmt.Sprintf("/app/sync/%s", file1Name)
	touchCmd := []string{"touch", file1Path}
	exitCode, _, err := publishingClient.Exec(ctx, touchCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Write content to file1.md
	file1Content := "This is the content of file1"
	writeCmd1 := []string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", file1Content, file1Path)}
	exitCode, _, err = publishingClient.Exec(ctx, writeCmd1)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Create file2.md
	file2Name := "file2.md"
	file2Path := fmt.Sprintf("/app/sync/%s", file2Name)
	touchCmd2 := []string{"touch", file2Path}
	exitCode, _, err = publishingClient.Exec(ctx, touchCmd2)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Write content to file2.md
	file2Content := "This is the content of file2"
	writeCmd2 := []string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", file2Content, file2Path)}
	exitCode, _, err = publishingClient.Exec(ctx, writeCmd2)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Wait for publishing client to sync files to server
	log.Printf("Waiting for files to sync to server...")
	time.Sleep(10 * time.Second)

	// Verify files are on the server
	checkFile := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "-f", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "File not found: %s", path)
	}
	checkContent := func(container testcontainers.Container, path, expected string) {
		catCmd := []string{"cat", path}
		_, reader, err := container.Exec(ctx, catCmd)
		require.NoError(t, err)

		var stdout, stderr bytes.Buffer
		_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
		require.NoError(t, err)
		actual := strings.TrimSpace(stdout.String())
		require.Equal(t, expected, actual, "File content mismatch for %s", path)
	}

	checkFile(fileServerContainer, "/app/storage/"+file1Name)
	checkFile(fileServerContainer, "/app/storage/"+file2Name)
	checkContent(fileServerContainer, "/app/storage/"+file1Name, file1Content)
	checkContent(fileServerContainer, "/app/storage/"+file2Name, file2Content)

	// 3. Introduce the New Client - Start consuming client
	log.Printf("Starting consuming client...")
	consumingClient := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "consuming-client",
		Dockerfile: "clientSyncPubsub.Dockerfile",
		QueueName:  "consume-queue",
	})
	testcontainers.CleanupContainer(t, consumingClient)

	// 4. Wait for Initial Sync to complete
	log.Printf("Waiting for initial sync to complete...")
	time.Sleep(8 * time.Second)

	// 5. Success Criteria - File Existence and Content
	log.Printf("Verifying initial sync results...")

	// Assert files exist on consuming client
	checkFile(consumingClient, file1Path)
	checkFile(consumingClient, file2Path)

	// Assert file content matches
	checkContent(consumingClient, file1Path, file1Content)
	checkContent(consumingClient, file2Path, file2Content)

	// 6. Test Real-time Sync - Create a new file on publishing client
	log.Printf("Testing real-time sync...")
	file3Name := "file3.md"
	file3Path := fmt.Sprintf("/app/sync/%s", file3Name)
	file3Content := "This is file3 created after initial sync"

	// Create and write to file3
	touchCmd3 := []string{"touch", file3Path}
	exitCode, _, err = publishingClient.Exec(ctx, touchCmd3)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	writeCmd3 := []string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", file3Content, file3Path)}
	exitCode, _, err = publishingClient.Exec(ctx, writeCmd3)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Wait for real-time sync
	time.Sleep(10 * time.Second)

	// Assert file3 appears on consuming client via real-time sync
	checkFile(consumingClient, file3Path)
	checkContent(consumingClient, file3Path, file3Content)

	log.Printf("Initial client synchronization test completed successfully!")
}

func TestOfflineSync(t *testing.T) {
	ctx := context.Background()

	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	// Setup initial server state with some conflicting files
	_, _, err := fileServerContainer.Exec(ctx, []string{"mkdir", "-p", "/app/storage/nested"})
	require.NoError(t, err)
	_, _, err = fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'old server content' > /app/storage/kappa.md"})
	require.NoError(t, err)
	_, _, err = fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'old server content' > /app/storage/nested/chungus.md"})
	require.NoError(t, err)

	// This client is set up by clientOfflineSync.Dockerfile with files in /app/sync
	offlineClient := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "t-offline-client",
		Dockerfile: "clientOfflineSync.Dockerfile",
		QueueName:  "sync-queue-1",
	})
	testcontainers.CleanupContainer(t, offlineClient)

	// Wait for offline sync to complete
	log.Printf("Waiting for offline sync to complete...")
	time.Sleep(5 * time.Second)

	// Helper functions following existing pattern
	checkPathOnServer := func(container testcontainers.Container, path string, isDir bool) {
		fullPath := "/app/storage/" + path
		var checkCmd []string
		if isDir {
			checkCmd = []string{"test", "-d", fullPath}
		} else {
			checkCmd = []string{"test", "-f", fullPath}
		}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err, fmt.Sprintf("Error executing check for %s on server", fullPath))
		require.Equal(t, 0, exitCode, "Path not found or not correct type on server: %s", fullPath)
	}

	checkContent := func(container testcontainers.Container, path, expected string) {
		catCmd := []string{"cat", path}
		_, reader, err := container.Exec(ctx, catCmd)
		require.NoError(t, err)

		var stdout, stderr bytes.Buffer
		_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
		require.NoError(t, err)
		actual := strings.TrimSpace(stdout.String())
		require.Equal(t, expected, actual, "File content mismatch for %s", path)
	}

	// Assertions: Check if files from the client's /app/sync directory are on the file server
	checkPathOnServer(fileServerContainer, "root_file.md", false)
	checkPathOnServer(fileServerContainer, "nested", true)
	checkPathOnServer(fileServerContainer, "nested/middle_file.md", false)

	// Check that content matches client (local is source of truth)
	checkContent(fileServerContainer, "/app/storage/root_file.md", "This is a root level file for offline sync testing.\nIt contains multiple lines of content.")
	checkContent(fileServerContainer, "/app/storage/nested/middle_file.md", "# Middle File\nThis is a markdown file.")

	// Check that server-only files are removed
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/kappa.md")
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/nested/chungus.md")

	log.Printf("Offline sync test completed successfully.")
}

func TestClientSyncNecoshot(t *testing.T) {
	ctx := context.Background()

	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	// Create files on server
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'filler for compare' > /app/storage/client_win_upload.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'should be gone' > /app/storage/client_win_delete.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'server win' > /app/storage/client_lose_delete.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'tie delete' > /app/storage/client_tie_delete.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'filler for compare' > /app/storage/client_tie_win.md"})

	// Create directories on server
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "mkdir /app/storage/server_subdir"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'Client should download this' > /app/storage/server_subdir/server.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "mkdir /app/storage/server_empty_subdir"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "mkdir /app/storage/client_win_delete_subdir"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "mkdir /app/storage/client_tie_delete_subdir"})

	// Notes for integration tests:
	// client_win_upload.md on client Last modified is 2025-01-01 00:00:01
	// client_win_delete.md on client Last modified is 2025-01-01 00:00:01
	// client_lose_delete.md on client Last modified is 2025-01-01 00:00:00
	// Forge Last modified file dates:
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "touch -d '2025-01-01 00:00:00' /app/storage/client_win_upload.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "touch -d '2025-01-01 00:00:00' /app/storage/client_win_delete.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "touch -d '2025-01-01 00:00:01' /app/storage/client_lose_delete.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "touch -d '2025-01-01 00:00:00' /app/storage/client_win_delete_subdir"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "touch -d '2025-01-01 00:00:00' /app/storage/client_tie_delete.md"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "touch -d '2025-01-01 00:00:00' /app/storage/client_tie_delete_subdir"})
	fileServerContainer.Exec(ctx, []string{"sh", "-c", "touch -d '2025-01-01 00:00:00' /app/storage/client_tie_win.md"})
	// Create clientsync container
	clientContainer := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "clientsync-necoshot",
		Dockerfile: "clientNecoshot.Dockerfile",
		QueueName:  "sync-queue-1",
	})
	testcontainers.CleanupContainer(t, clientContainer)

	// Wait for sync to complete
	time.Sleep(10 * time.Second)

	// Check file server
	checkContent(t, ctx, fileServerContainer, "/app/storage/client_win_upload.md", "client win")
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/client_win_delete.md")
	checkExist(t, ctx, fileServerContainer, "/app/storage/client_lose_delete.md", false)
	checkExist(t, ctx, fileServerContainer, "/app/storage/client_subdir", true)
	checkExist(t, ctx, fileServerContainer, "/app/storage/client_subdir/client_regular_upload.md", false)
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/client_win_delete_subdir")
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/client_tie_delete.md")
	checkNotExist(t, ctx, fileServerContainer, "/app/storage/client_tie_delete_subdir")
	checkContent(t, ctx, fileServerContainer, "/app/storage/client_tie_win.md", "client tie win")
	// Check client
	checkNotExist(t, ctx, clientContainer, "/app/sync/client_win_delete.md")
	checkContent(t, ctx, clientContainer, "/app/sync/client_lose_delete.md", "server win")
	checkExist(t, ctx, clientContainer, "/app/sync/server_subdir", true)
	checkExist(t, ctx, clientContainer, "/app/sync/server_subdir/server.md", false)
}

func TestClientSyncBasicUploadDownload(t *testing.T) {
	ctx := context.Background()

	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	rabbitContainer := setupRabbitMQ(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, rabbitContainer)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	// Setup: Create a file on server that client should download
	_, _, err := fileServerContainer.Exec(ctx, []string{"sh", "-c", "echo 'download me' > /app/storage/server_file.md"})
	require.NoError(t, err)

	// Create clientsync container - it has files from Dockerfile that should be uploaded
	clientContainer := setupClient(ctx, t, netNetwork, ClientConfig{
		ID:         "clientsync-basic",
		Dockerfile: "clientSync.Dockerfile",
		QueueName:  "sync-queue-1",
	})
	testcontainers.CleanupContainer(t, clientContainer)

	// Wait for sync to complete (pubsub runs automatically via Dockerfile)
	time.Sleep(10 * time.Second)

	// Assert: Both files should exist on both client and server
	checkFile := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "-f", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "File not found: %s", path)
	}

	checkContent := func(container testcontainers.Container, path, expected string) {
		catCmd := []string{"cat", path}
		_, reader, err := container.Exec(ctx, catCmd)
		require.NoError(t, err)

		var stdout, stderr bytes.Buffer
		_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
		require.NoError(t, err)
		actual := strings.TrimSpace(stdout.String())
		require.Equal(t, expected, actual, "File content mismatch for %s", path)
	}

	// Check uploaded files (client -> server) - from Dockerfile
	checkFile(fileServerContainer, "/app/storage/file.md")
	checkContent(fileServerContainer, "/app/storage/file.md", "initial content")
	checkFile(fileServerContainer, "/app/storage/another.md")
	checkContent(fileServerContainer, "/app/storage/another.md", "another file")

	// Check downloaded file (server -> client)
	checkFile(clientContainer, "/app/sync/server_file.md")
	checkContent(clientContainer, "/app/sync/server_file.md", "download me")

	// Check original files still exist on client
	checkFile(clientContainer, "/app/sync/file.md")
	checkContent(clientContainer, "/app/sync/file.md", "initial content")
	checkFile(clientContainer, "/app/sync/another.md")
	checkContent(clientContainer, "/app/sync/another.md", "another file")

	log.Printf("Basic upload/download test completed successfully")
}

func checkExist(t *testing.T, ctx context.Context, container testcontainers.Container, path string, isDir bool) {
	checkCmd := []string{"test", "-f", path}
	if isDir {
		checkCmd = []string{"test", "-d", path}
	}
	exitCode, _, err := container.Exec(ctx, checkCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "File not found: %s", path)
}

func checkContent(t *testing.T, ctx context.Context, container testcontainers.Container, path, expected string) {
	catCmd := []string{"cat", path}
	_, reader, err := container.Exec(ctx, catCmd)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
	require.NoError(t, err)
	actual := strings.TrimSpace(stdout.String())
	require.Equal(t, expected, actual, "File content mismatch for %s", path)
}

func checkNotExist(t *testing.T, ctx context.Context, container testcontainers.Container, path string) {
	checkCmd := []string{"test", "-e", path}
	exitCode, _, err := container.Exec(ctx, checkCmd)
	require.NoError(t, err)
	require.Equal(t, 1, exitCode, "Should not exist: %s", path)
}
