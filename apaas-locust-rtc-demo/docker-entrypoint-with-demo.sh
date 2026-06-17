#!/usr/bin/env bash
set -e

MODE="${1:-both}"

echo "================================================"
echo "APaaS Locust + Go Demo Server"
echo "================================================"
echo "Mode: ${MODE}"
echo ""

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
