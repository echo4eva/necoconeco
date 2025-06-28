package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

type Config struct {
	ClientID             string `json:"client_id"`
	RabbitMQAddress      string `json:"rabbitmq_address"`
	RabbitMQExchangeName string `json:"rabbitmq_exchange_name"`
	RabbitMQQueueName    string `json:"rabbitmq_queue_name,omitempty"`
	RabbitMQRoutingKey   string `json:"rabbitmq_routing_key"`
	SyncDirectory        string `json:"sync_directory"`
	SyncServerURL        string `json:"sync_server_url,omitempty"`
	Port                 string `json:"port,omitempty"`
}

func LoadConfig() (*Config, error) {
	config := &Config{}

	execPath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	execDir := filepath.Dir(execPath)

	// Retrieve config if it exists
	configPath := filepath.Join(execDir, "config.json")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		log.Printf("Using config.json\n")
		configBytes, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(configBytes, config); err != nil {
			return nil, err
		}
	}

	godotenv.Load()
	// Overwrite and use .env for config if it exists
	if val := os.Getenv("CLIENT_ID"); val != "" {
		config.ClientID = val
	}
	if val := os.Getenv("RABBITMQ_ADDRESS"); val != "" {
		config.RabbitMQAddress = val
	}
	if val := os.Getenv("RABBITMQ_EXCHANGE_NAME"); val != "" {
		config.RabbitMQExchangeName = val
	}
	if val := os.Getenv("RABBITMQ_QUEUE_NAME"); val != "" {
		config.RabbitMQQueueName = val
	}
	if val := os.Getenv("RABBITMQ_ROUTING_KEY"); val != "" {
		config.RabbitMQRoutingKey = val
	}
	if val := os.Getenv("SYNC_DIRECTORY"); val != "" {
		config.SyncDirectory = val
	}
	if val := os.Getenv("SYNC_SERVER_URL"); val != "" {
		config.SyncServerURL = val
	}
	if val := os.Getenv("PORT"); val != "" {
		config.Port = val
	}

	return config, nil
}
