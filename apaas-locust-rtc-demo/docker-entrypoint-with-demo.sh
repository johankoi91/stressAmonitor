#!/usr/bin/env bash
set -e

MODE="${1:-both}"

echo "================================================"
echo "APaaS Locust + Go Demo Server"
echo "================================================"
echo "Mode: ${MODE}"
echo ""

# 从环境变量注入 SSL 证书（base64 编码）
# 用法：
#   docker run -e TLS_CERT="$(base64 < cert.pem)" -e TLS_KEY="$(base64 < key.pem)" IMAGE
# 若环境变量未设置，则跳过（Go server 启动时会因找不到证书而报错）
TLS_DIR="/opt/go_deploy_demo/web-demo/key"
TLS_CERT_PATH="${TLS_DIR}/edge.rtcdevelopers.com.pem"
TLS_KEY_PATH="${TLS_DIR}/edge.rtcdevelopers.com-key.pem"

if [ -n "${TLS_CERT}" ] && [ -n "${TLS_KEY}" ]; then
    mkdir -p "${TLS_DIR}"
    echo "${TLS_CERT}" | base64 -d > "${TLS_CERT_PATH}"
    echo "${TLS_KEY}"  | base64 -d > "${TLS_KEY_PATH}"
    echo "[entrypoint] SSL 证书已从环境变量写入 ${TLS_DIR}"
elif [ ! -f "${TLS_CERT_PATH}" ] || [ ! -f "${TLS_KEY_PATH}" ]; then
    echo "[entrypoint] 警告: 未找到 SSL 证书文件，且未设置 TLS_CERT / TLS_KEY 环境变量"
    echo "             Go demo server 将无法启动 HTTPS 服务"
    echo "             请通过以下方式传入证书："
    echo "               -e TLS_CERT=\"\$(base64 < cert.pem)\""
    echo "               -e TLS_KEY=\"\$(base64 < key.pem)\""
fi

case "${MODE}" in
  locust|stress)
    echo "Starting Locust stress testing only..."
    exec /opt/entrypoint.sh
    ;;

  demo|server)
    echo "Starting Go demo server only..."
    echo "Listening on port 8800 (HTTPS)"
    cd /opt/go_deploy_demo
    exec ./agora_sdk_server_linux
    ;;

  both)
    echo "Starting Go demo server and stress console..."
    echo "Go demo server will run on port 8800 (HTTPS)"
    echo "Locust will be started on demand from the web console"
    echo "Console: /web-demo/stress-console/index.html"
    echo ""
    exec supervisord -n -c /etc/supervisor/conf.d/supervisord.conf
    ;;

  smoke)
    echo "Running smoke test only..."
    exec /opt/entrypoint.sh smoke
    ;;

  bash|shell)
    echo "Starting interactive bash shell..."
    exec /bin/bash
    ;;

  *)
    echo "ERROR: Unknown mode '${MODE}'"
    echo ""
    echo "Usage: docker run [options] IMAGE [MODE]"
    echo ""
    echo "Available modes:"
    echo "  both       - Run Go demo server + stress console (default; Locust starts from web console)"
    echo "  demo       - Run Go demo server only"
    echo "  locust     - Run Locust stress testing only"
    echo "  smoke      - Run smoke test"
    echo "  bash       - Interactive bash shell"
    echo ""
    exit 1
    ;;
esac
