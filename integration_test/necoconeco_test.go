package integration_test

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
		network.WithNetwork([]string{"client"}, netNetwork),
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

	// Create a file in the first client's sync directory
	fileName := "testfile.md"
	touchCmd := []string{"touch", fmt.Sprintf("/app/%s", fileName)}
	exitCode, _, err := firstClient.Exec(ctx, touchCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Wait for sync to propagate
	time.Sleep(10 * time.Second)

	// Check file exists in file server's working directory (/app/data or /app)
	checkFileCmd := []string{"test", "-f", fmt.Sprintf("/app/storage/%s", fileName)}
	exitCode, _, err = fileServerContainer.Exec(ctx, checkFileCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "File not found in file server")

	// Check file exists in second client's sync directory (/app)
	checkFileCmd = []string{"test", "-f", fmt.Sprintf("/app/%s", fileName)}
	exitCode, _, err = secondClient.Exec(ctx, checkFileCmd)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "File not found in second client")
}
