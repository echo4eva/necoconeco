#!/bin/bash

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Change to script directory
cd "$SCRIPT_DIR"

echo "Starting necoconeco sync..."
./necoconeco-clientsync &

echo "Starting necoconeco client..."
./necoconeco-client &

echo "Necoconeco started successfully!"