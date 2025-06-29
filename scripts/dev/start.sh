#!/bin/bash

# Find the directory where the script is located.
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

# The project root is one directory above the script's directory.
PROJECT_ROOT="$SCRIPT_DIR/.."

# --- Now, use the PROJECT_ROOT to find the files ---

# Load environment variables from the project root.
ENV_FILE="$PROJECT_ROOT/.env"
if [ -f "$ENV_FILE" ]; then
  echo "Loading configuration from $ENV_FILE"
  export $(cat "$ENV_FILE" | sed 's/#.*//g' | xargs)
fi

echo "Starting initial sync..."
# Run the executable from the project root.
"$PROJECT_ROOT/clientsync"

echo "Initial sync complete."
echo "Starting real-time file watcher..."
# Run the second executable from the project root.
"$PROJECT_ROOT/client"