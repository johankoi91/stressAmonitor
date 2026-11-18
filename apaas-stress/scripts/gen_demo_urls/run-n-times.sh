#!/bin/bash

N=${1:-10}

APP_ID="f29d4dc623c741a3ad38ccaf27a8d2e2"
HOST="http://20.1.125.171:18080"
ROOM_UUID="1167199453839361951"

echo "Start running $N tasks (parallel)..."
echo ""

for ((i=1;i<=N;i++))
do
  (
    echo "[START] Task #$i"
    ./build-apaas-meeting-url.sh \
      --app-id "$APP_ID" \
      --host "$HOST" \
      --room-uuid "$ROOM_UUID"
    echo ""   # 空行分隔
  ) &
done

wait

echo ""
echo "All done."
