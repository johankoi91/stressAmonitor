#!/bin/bash
set +e
cd /home/bbt 2>/dev/null || true
if [ -x /home/bbt/start-meeting-container-server.sh ]; then
  /home/bbt/start-meeting-container-server.sh >/home/bbt/meeting-entrypoint-start.log 2>&1 || true
fi
set -e
if [ "$#" -eq 0 ]; then
  exec /sbin/init nopti
fi
exec "$@"
