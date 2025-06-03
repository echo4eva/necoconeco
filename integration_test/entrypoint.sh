#!/bin/sh
/app/sync
# Keep the container alive for test assertions
exec tail -f /dev/null 