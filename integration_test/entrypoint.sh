#!/bin/sh
/app/syncbin
# Keep the container alive for test assertions
exec tail -f /dev/null 