#!/bin/bash
set -euo pipefail

PID_FILE="/home/bbt/meeting-container-server.pid"
if [ ! -f "$PID_FILE" ]; then
  echo "服务未运行: 找不到 $PID_FILE"
  exit 0
fi
PID="$(cat "$PID_FILE")"
if kill -0 "$PID" >/dev/null 2>&1; then
  kill "$PID"
  echo "已停止服务: pid=$PID"
else
  echo "服务进程不存在: pid=$PID"
fi
rm -f "$PID_FILE"
