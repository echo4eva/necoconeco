#!/bin/sh

# Start the coldsync process in the background
/app/coldsync &

# Keep the container running
tail -f /dev/null
