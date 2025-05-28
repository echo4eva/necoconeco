package integration_test

import (
	"context"
	"log"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/compose"
)

func TestMain(t *testing.T) {
	ctx := context.Background()

	stack, err := compose.NewDockerComposeWith(
		compose.StackIdentifier("test"),
		compose.WithStackFiles("../docker-compose.yml"),
	)
	if err != nil {
		log.Printf("Failed to create stack: %s\n", err)
	}
	stack.Up(ctx)
	defer func() {
		err = stack.Down(
			context.Background(),
			compose.RemoveOrphans(true),
			compose.RemoveVolumes(true),
			compose.RemoveImagesLocal,
		)
		if err != nil {
			log.Printf("Failed to stop stack: %s\n", err)
		}
	}()

	serviceNames := stack.Services()
	for _, name := range serviceNames {
		log.Printf("%s\n", name)
	}

	require.NoError(t, err)
}
