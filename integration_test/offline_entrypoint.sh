#!/bin/sh

echo "Starting offline sync process..."
# Run the offlinesync process
/app/offlinesync

echo "Offline sync completed. Keeping container running..."
# Keep the container running
tail -f /dev/null 