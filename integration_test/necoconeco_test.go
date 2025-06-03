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
				Dockerfile: "server.Dockerfile",
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

func setupClientContainer(ctx context.Context, t *testing.T, netNetwork *testcontainers.DockerNetwork, clientID, queueName string) testcontainers.Container {
	container, err := testcontainers.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(
			testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "client.Dockerfile",
			},
		),
		testcontainers.WithEnv(
			map[string]string{
				"CLIENT_ID":              clientID,
				"RABBITMQ_ADDRESS":       "amqp://guest:guest@rabbitmq:5672/",
				"RABBITMQ_EXCHANGE_NAME": "exchange",
				"RABBITMQ_QUEUE_NAME":    queueName,
				"RABBITMQ_ROUTING_KEY":   "routing.key",
				"SYNC_DIRECTORY":         "/app",
				"SYNC_SERVER_URL":        "file-server:8080",
			},
		),
		testcontainers.WithLogConsumers(&TestLogConsumer{
			prefix: clientID,
		}),
		network.WithNetwork([]string{clientID}, netNetwork),
	)
	require.NoError(t, err)
	return container
}

func setupBareClientContainer(ctx context.Context, t *testing.T, netNetwork *testcontainers.DockerNetwork, clientID, queueName string) testcontainers.Container {
	container, err := testcontainers.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(
			testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "integration_test/clientBare.Dockerfile",
			},
		),
		testcontainers.WithEnv(
			map[string]string{
				"CLIENT_ID":              clientID,
				"RABBITMQ_ADDRESS":       "amqp://guest:guest@rabbitmq:5672/",
				"RABBITMQ_EXCHANGE_NAME": "exchange",
				"RABBITMQ_QUEUE_NAME":    queueName,
				"RABBITMQ_ROUTING_KEY":   "routing.key",
				"SYNC_DIRECTORY":         "/app",
				"SYNC_SERVER_URL":        "file-server:8080",
			},
		),
		testcontainers.WithLogConsumers(&TestLogConsumer{
			prefix: clientID,
		}),
		network.WithNetwork([]string{clientID}, netNetwork),
	)
	require.NoError(t, err)
	return container
}

func setupColdSyncClientContainer(ctx context.Context, t *testing.T, netNetwork *testcontainers.DockerNetwork, clientID string) testcontainers.Container {
	container, err := testcontainers.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(
			testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: "integration_test/clientCold.Dockerfile",
			},
		),
		testcontainers.WithEnv(
			map[string]string{
				"CLIENT_ID":       clientID,
				"SYNC_DIRECTORY":  "/sync",
				"SYNC_SERVER_URL": "file-server:8080",
			},
		),
		testcontainers.WithLogConsumers(&TestLogConsumer{
			prefix: clientID,
		}),
		network.WithNetwork([]string{clientID}, netNetwork),
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
	firstContainer := setupClientContainer(ctx, t, netNetwork, "t-client-1", "queue-1")
	testcontainers.CleanupContainer(t, firstContainer)

	log.Printf("MAKING CONTAINER-2\n")
	secondContainer := setupClientContainer(ctx, t, netNetwork, "t-client-2", "queue-2")
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

	firstClient := setupClientContainer(ctx, t, netNetwork, "t-client-1", "queue-1")
	testcontainers.CleanupContainer(t, firstClient)

	secondClient := setupClientContainer(ctx, t, netNetwork, "t-client-2", "queue-2")
	testcontainers.CleanupContainer(t, secondClient)

	// 1. Create a file in the first client's sync directory
	fileName := "testfile.md"
	filePath := fmt.Sprintf("/app/%s", fileName)
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
	newFilePath := fmt.Sprintf("/app/%s", newFileName)
	renameCmd := []string{"mv", filePath, newFilePath}
	exitCode, _, err = firstClient.Exec(ctx, renameCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	time.Sleep(3 * time.Second)

	// Assert new file exists, old does not
	checkFile(fileServerContainer, "/app/storage/"+newFileName)
	checkFile(secondClient, newFilePath)
	// Old file should not exist
	checkNotExist := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "!", "-f", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "File should not exist: %s", path)
	}
	checkNotExist(fileServerContainer, "/app/storage/"+fileName)
	checkNotExist(secondClient, filePath)

	// 4. Create a directory
	dirName := "mydir"
	dirPath := fmt.Sprintf("/app/%s", dirName)
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
	checkNotExist(fileServerContainer, "/app/storage/"+newFileName)
	checkNotExist(secondClient, newFilePath)

	// 6. Rename the directory
	newDirName := "renameddir"
	newDirPath := fmt.Sprintf("/app/%s", newDirName)
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
	checkNotExist(fileServerContainer, fmt.Sprintf("/app/storage/%s/%s", dirName, newFileName))
	checkNotExist(secondClient, movedFilePath)
	checkNotExist(fileServerContainer, "/app/storage/"+dirName)
	checkNotExist(secondClient, dirPath)

	// 7. Delete the directory
	rmDirCmd := []string{"rm", "-rf", newDirPath}
	exitCode, _, err = firstClient.Exec(ctx, rmDirCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	time.Sleep(3 * time.Second)

	// Assert directory and file are gone
	checkNotExist(fileServerContainer, "/app/storage/"+newDirName)
	checkNotExist(secondClient, newDirPath)
	checkNotExist(fileServerContainer, fmt.Sprintf("/app/storage/%s/%s", newDirName, newFileName))
	checkNotExist(secondClient, fmt.Sprintf("%s/%s", newDirPath, newFileName))
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
	clientContainer := setupBareClientContainer(ctx, t, netNetwork, "sync-client", "sync-queue-1")
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
	checkNotExist := func(container testcontainers.Container, path string) {
		checkCmd := []string{"test", "!", "-e", path}
		exitCode, _, err := container.Exec(ctx, checkCmd)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "Should not exist: %s", path)
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
	checkFile(clientContainer, "/app/same.md")
	checkContent(fileServerContainer, "/app/storage/same.md", "")
	checkContent(clientContainer, "/app/same.md", "")

	// different.md
	log.Printf("Checking different.md\n")
	checkFile(fileServerContainer, "/app/storage/different.md")
	checkFile(clientContainer, "/app/different.md")
	checkContent(fileServerContainer, "/app/storage/different.md", "server content")
	checkContent(clientContainer, "/app/different.md", "server content")

	// onClient and toBeDeleted.md should NOT exist on either
	log.Printf("Checking onClient and toBeDeleted.md\n")
	checkNotExist(fileServerContainer, "/app/storage/onClient")
	checkNotExist(clientContainer, "/app/onClient")
	checkNotExist(fileServerContainer, "/app/storage/onClient/toBeDeleted.md")
	checkNotExist(clientContainer, "/app/onClient/toBeDeleted.md")

	// onServer and toBeDownloaded.md should exist on both, with correct content
	log.Printf("Checking onServer and toBeDownloaded.md\n")
	checkDir(fileServerContainer, "/app/storage/onServer")
	checkDir(clientContainer, "/app/onServer")
	checkFile(fileServerContainer, "/app/storage/onServer/toBeDownloaded.md")
	checkFile(clientContainer, "/app/onServer/toBeDownloaded.md")
	checkContent(fileServerContainer, "/app/storage/onServer/toBeDownloaded.md", "download me")
	checkContent(clientContainer, "/app/onServer/toBeDownloaded.md", "download me")
}

func TestColdSync(t *testing.T) {
	ctx := context.Background()

	netNetwork := setupNetwork(ctx, t)
	testcontainers.CleanupNetwork(t, netNetwork)

	fileServerContainer := setupFileServer(ctx, t, netNetwork)
	testcontainers.CleanupContainer(t, fileServerContainer)

	// This client is set up by clientCold.Dockerfile with files in /sync
	coldClient := setupColdSyncClientContainer(ctx, t, netNetwork, "t-cold-client")
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
