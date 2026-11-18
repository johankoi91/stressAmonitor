#!/bin/bash
# RTC 压测容器一键部署脚本
# 用法：
#   ./deploy.sh                        # 加载镜像并启动
#   ./deploy.sh --name my-stress       # 指定容器名（默认 rtc-stress）
#   ./deploy.sh --port 9088            # 指定端口（默认 8088）
#   ./deploy.sh --build                # 重新 build 镜像后启动（需要已有基础镜像）

set -e

CONTAINER_NAME="rtc-stress"
PORT=8088
DO_BUILD=0
IMAGE_TAG="rtc_stress:latest"

while [[ $# -gt 0 ]]; do
    case $1 in
        --name) CONTAINER_NAME="$2"; shift 2 ;;
        --port) PORT="$2"; shift 2 ;;
        --build) DO_BUILD=1; shift ;;
        *) echo "未知参数: $1"; exit 1 ;;
    esac
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ── 方式 A：直接加载已打包的镜像 ───────────────────────────────────────────
load_image() {
    ARCHIVE=$(ls "$SCRIPT_DIR"/../rtc_stress*.tar.gz 2>/dev/null | sort -r | head -1 || true)
    if [ -z "$ARCHIVE" ]; then
        echo "[ERROR] 找不到镜像包 rtc_stress*.tar.gz，请把镜像包放到仓库根目录同级"
        echo "        或者先在有网络的环境执行: docker build -t rtc_stress:latest rtc-stress-docker/"
        exit 1
    fi
    echo "[1/3] 加载镜像: $ARCHIVE"
    docker load < "$ARCHIVE"
    IMAGE_TAG=$(docker images --format "{{.Repository}}:{{.Tag}}" | grep rtc_stress | head -1)
    echo "      镜像: $IMAGE_TAG"
}

# ── 方式 B：本地 build（需要基础镜像已存在）──────────────────────────────────
build_image() {
    BASE=$(docker images --format "{{.Repository}}:{{.Tag}}" | grep -E "rtc_hybrid_tools|rtc_stress" | head -1 || true)
    if [ -z "$BASE" ]; then
        echo "[ERROR] 找不到可用的基础镜像"
        echo "        请先执行: docker load < <基础镜像包>"
        exit 1
    fi
    IMAGE_TAG="rtc_stress:$(date +%Y%m%d-%H%M%S)"
    echo "[1/3] 构建镜像 $IMAGE_TAG (基础镜像: $BASE)"
    sed "s|FROM .*|FROM $BASE|" "$SCRIPT_DIR/Dockerfile.local" > /tmp/Dockerfile.deploy
    docker build -t "$IMAGE_TAG" -f /tmp/Dockerfile.deploy "$SCRIPT_DIR/"
    rm -f /tmp/Dockerfile.deploy
}

# ── 停止旧容器 ────────────────────────────────────────────────────────────────
stop_old() {
    echo "[2/3] 停止旧容器 $CONTAINER_NAME (如有)..."
    docker stop "$CONTAINER_NAME" 2>/dev/null && docker rm "$CONTAINER_NAME" 2>/dev/null || true
}

# ── 启动新容器 ────────────────────────────────────────────────────────────────
start_container() {
    echo "[3/3] 启动容器 $CONTAINER_NAME ..."
    docker run -d \
        --name "$CONTAINER_NAME" \
        --restart=unless-stopped \
        --privileged \
        --pid=host \
        --net=host \
        --ulimit core=-1 \
        --security-opt seccomp=unconfined \
        "$IMAGE_TAG"

    echo "      等待服务启动..."
    sleep 6

    HOST_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "127.0.0.1")
    HTTP_CODE=$(curl -sS -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/" 2>/dev/null || echo "000")

    if [ "$HTTP_CODE" = "200" ]; then
        echo ""
        echo "===========================================" 
        echo " 部署成功！"
        echo " Meeting 控制台:  http://$HOST_IP:$PORT/"
        echo " 频道任务控制台:  http://$HOST_IP:$PORT/channel-users"
        echo "==========================================="
    else
        echo "[WARN] 服务未就绪 (HTTP $HTTP_CODE)，请稍后手动访问 http://$HOST_IP:$PORT/"
        echo "       查看日志: docker logs $CONTAINER_NAME"
    fi
}

if [ "$DO_BUILD" = "1" ]; then
    build_image
else
    if docker images --format "{{.Repository}}:{{.Tag}}" | grep -q "^rtc_stress:"; then
        echo "[1/3] 检测到本地已有 rtc_stress 镜像，直接使用"
        IMAGE_TAG=$(docker images --format "{{.Repository}}:{{.Tag}}" | grep "^rtc_stress:" | head -1)
    else
        load_image
    fi
fi

stop_old
start_container
