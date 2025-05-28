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

func TestMain(t *testing.T) {
	ctx := context.Background()

	log.Printf("MAKING NETWORK\n")
	netNetwork, err := network.New(ctx)
	require.NoError(t, err)
	testcontainers.CleanupNetwork(t, netNetwork)

	log.Printf("MAKING RABBIT CONTAINER\n")
	rabbitContainer, err := rabbitmq.Run(
		ctx,
		"rabbitmq:4.0-management",
		rabbitmq.WithAdminUsername("admin"),
		rabbitmq.WithAdminPassword("password"),
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
	if err != nil {
		log.Printf("Failed to start rabbitmq container: %s", err)
	}
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, rabbitContainer)

	log.Printf("MAKING FILE SERVER CONTAINER\n")
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
	testcontainers.CleanupContainer(t, fileServerContainer)
	require.NoError(t, err)

	log.Printf("MAKING CONTAINER-1\n")
	firstContainer, err := testcontainers.Run(
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
				"CLIENT_ID":              "t-client-1",
				"RABBITMQ_ADDRESS":       "amqp://admin:password@rabbitmq:5672/",
				"RABBITMQ_EXCHANGE_NAME": "exchange",
				"RABBITMQ_QUEUE_NAME":    "queue-1",
				"RABBITMQ_ROUTING_KEY":   "routing.key",
				"SYNC_DIRECTORY":         "/app",
				"SYNC_SERVER_URL":        "file-server:8080",
			},
		),
		testcontainers.WithLogConsumers(&TestLogConsumer{
			prefix: "container-1",
		}),
	)
	testcontainers.CleanupContainer(t, firstContainer)
	require.NoError(t, err)

	// stack, err := compose.NewDockerComposeWith(
	// 	compose.StackIdentifier("test"),
	// 	compose.WithStackFiles("../docker-compose.yml"),
	// )
	// if err != nil {
	// 	log.Printf("Failed to create stack: %s\n", err)
	// }
	// err = stack.Up(ctx, compose.Wait(true))
	// if err != nil {
	// 	log.Printf("Failed to start stack: %s\n", err)
	// }
	// defer func() {
	// 	err = stack.Down(
	// 		context.Background(),
	// 		compose.RemoveOrphans(true),
	// 		compose.RemoveVolumes(true),
	// 		compose.RemoveImagesLocal,
	// 	)
	// 	if err != nil {
	// 		log.Printf("Failed to stop stack: %s\n", err)
	// 	}
	// }()

	// serviceNames := stack.Services()
	// for _, name := range serviceNames {
	// 	log.Printf("%s\n", name)
	// }

	// rabbitC, err := stack.ServiceContainer(context.Background(), "rabbitmq")
	// if err != nil {
	// 	log.Printf("Failed to get rabbitmq container: %s\n", err)
	// }
	// log.Printf("%s\n", rabbitC.ID)

	require.NoError(t, err)
}

func TestCreate(t *testing.T) {
}
