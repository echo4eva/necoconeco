#!/bin/bash
# This script runs the necoconeco client applications.

# Get the directory where the script is located, so it can find the executables.
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR"

echo "Starting one-time sync..."
./necoconeco-clientsync

echo "Starting real-time sync client..."
./necoconeco-client