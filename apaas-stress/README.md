# APaaS 压测

独立管理 APaaS 会议接口冒烟测试、Locust 压测、会议入会链接生成和任务日志。服务入口为 `main.go`，默认 HTTPS 8801 端口。

```text
main.go / main_test.go / Dockerfile / go.mod / go.sum / vendor/
apaas_scene/
  locustfile.py          # 原始压测逻辑，保留原文件内容
  locust_entry.py        # 读取外部凭据，更新配置并重建房间池
  apaas_smoke.py
  requirements.lock     # Locust 完整依赖的固定版本
  src/                  # 场景所需的 Token 工具
scripts/gen_demo_urls/  # APaaS 会议入会链接生成
web-demo/
  stress-console/       # 任务控制台
  key/                  # HTTPS 证书，构建时移至 /opt/certs
```

## 构建和启动

```bash
docker build --platform linux/amd64 -t apaas-stress:v1 .
mkdir -p /opt/apaas-results
docker run -d --name apaas-stress --restart unless-stopped \
  -p 8801:8801 -v /opt/apaas-results:/results \
  apaas-stress:v1
```

| 功能 | 地址 |
|---|---|
| 控制台 | `https://服务器IP:8801/`，自动进入 `/web-demo/stress-console/` |
| Locust 指标 | `/locust/`，在控制台启动压测任务后使用 |
| 任务 API | `/api/stress/tasks` |
| 健康检查 | `/healthz` |

Locust 仅在容器内部监听 127.0.0.1:8089，由 Go 代理。任务结果保存在 `/results/web-console/{taskId}/`，按示例挂载后宿主机位置为 `/opt/apaas-results/web-console/{taskId}/`。

## 原脚本适配

`locustfile.py` 内容保持不变。Go 使用 `-f apaas_scene/locust_entry.py` 启动 Locust，通过 `AGORA_APP_ID`、`AGORA_APP_CERTIFICATE` 传入控制台凭据。适配入口更新原脚本的 `CONFIG` 并重建缓存了 Token 的房间池；用户数、速率和时长仍由原脚本解析。Smoke 和 Locust 的工作目录为 `apaas_scene/`。

原脚本 SHA-256：`8d20eacfc6000d1689d94baf25acf87d3749d0f8d719e6d81eb3dd9a6416705b`。

入会链接生成器使用目标 APaaS 的 Token 接口，Base URL 应填写已部署的会议前端地址。

## 离线导出及检查

```bash
mkdir -p dist
docker save apaas-stress:v1 | gzip > dist/apaas-stress-v1-linux-amd64-offline.tar.gz
go test -mod=vendor -race ./...
docker logs --tail 100 apaas-stress
docker inspect --format '{{.State.Health.Status}}' apaas-stress
```

直接本地启动需先安装 `apaas_scene/requirements.lock` 中的依赖，再运行 `go run -mod=vendor .`。

HTTPS 可通过 `TLS_CERT_FILE`、`TLS_KEY_FILE` 挂载替换，也支持 `TLS_CERT`、`TLS_KEY` base64 环境变量。`TLS_ENABLED=false` 可用于 HTTP 调试，`PORT` 可覆盖监听端口。
