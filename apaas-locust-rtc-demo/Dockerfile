# ============================================================
# Stage 1: 编译 Go server
# ============================================================
FROM golang:1.21-alpine AS go-builder

WORKDIR /build

COPY demo-stress/go_deploy_demo/go.mod .
COPY demo-stress/go_deploy_demo/go.sum .
COPY demo-stress/go_deploy_demo/vendor ./vendor
COPY demo-stress/go_deploy_demo/main.go .

RUN go build -mod=vendor -o agora_sdk_server_linux .

# ============================================================
# Stage 2: 运行时镜像
# ============================================================
FROM python:3.8-slim

LABEL description="APaaS Locust + Go Demo Server"

ENV DEBIAN_FRONTEND=noninteractive

RUN pip install --no-cache-dir \
        locust==2.24.1 \
        supervisor==4.2.5 \
    && mkdir -p \
        /etc/supervisor/conf.d \
        /var/log \
        /results \
        /opt/go_deploy_demo \
        /opt/apaas_scene

# Go server 二进制 + 静态资源
COPY --from=go-builder /build/agora_sdk_server_linux /opt/go_deploy_demo/
COPY demo-stress/go_deploy_demo/web-demo /opt/go_deploy_demo/web-demo

# APaaS 压测工程
COPY src/             /opt/apaas_scene/src/
COPY locustfile.py    /opt/apaas_scene/
COPY apaas_smoke.py   /opt/apaas_scene/

# gen_demo_urls 工具脚本
COPY demo-stress/gen_demo_urls/ /opt/apaas_scene/gen_demo_urls/
RUN chmod +x /opt/apaas_scene/gen_demo_urls/*.sh 2>/dev/null || true

# supervisor 配置
COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# entrypoint
COPY docker-entrypoint-with-demo.sh /opt/docker-entrypoint-with-demo.sh
RUN chmod +x /opt/go_deploy_demo/agora_sdk_server_linux \
             /opt/docker-entrypoint-with-demo.sh

EXPOSE 8800

ENTRYPOINT ["/opt/docker-entrypoint-with-demo.sh"]
CMD ["both"]
