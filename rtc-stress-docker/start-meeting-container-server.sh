#!/bin/bash
set -euo pipefail

cd /home/bbt
PORT="${MEETING_CONTROL_PORT:-8088}"
PID_FILE="/home/bbt/meeting-container-server.pid"
LOG_FILE="/home/bbt/meeting-container-server.log"

if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" >/dev/null 2>&1; then
  echo "服务已运行: pid=$(cat "$PID_FILE")"
  echo "访问地址: http://172.31.11.174:$PORT"
  exit 0
fi

nohup python3 /home/bbt/meeting_container_server.py > "$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"

echo "服务已启动: pid=$(cat "$PID_FILE")"
echo "访问地址: http://172.31.11.174:$PORT"
echo "日志文件: $LOG_FILE"
