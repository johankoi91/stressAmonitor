# RTC / RTM Demo

独立托管 RTC 功能示例、自定义视频源、RTM Demo 和本地 RTC Token API。服务入口为 `main.go`，默认 HTTPS 8800 端口。

```text
main.go / main_test.go / Dockerfile / go.mod / go.sum / vendor/
scripts/rtc_token.py    # RTC Token 计算工具，由 Go 接口调用
token_lib/             # RTC 使用的通用 AccessToken2 / Packer 库
web-demo/
  rtc-examples/        # 功能示例及私有化连接设置
    example/advanced/customVideoSource/ # 摄像头或内置视频推流
  rtm/                 # 已打包的 RTM 2.2.0-2 Demo、许可证及 source map
  assets/              # 本地 SDK 及共享资源
  key/                 # HTTPS 证书，构建时移至 /opt/certs
```

## 构建和启动

```bash
docker build --platform linux/amd64 -t rtc-demo:v1 .
docker run -d --name rtc-demo --restart unless-stopped -p 8800:8800 rtc-demo:v1
```

| 功能 | 地址 |
|---|---|
| 首页 / 快速开始 | `/rtc-examples/example/quickStart/` |
| 基础通话 | `/rtc-examples/example/basic/` |
| 进阶功能 | `/rtc-examples/example/advanced/` |
| 插件 | `/rtc-examples/example/extension/` |
| 其他 | `/rtc-examples/example/others/` |
| 自定义视频源 | `/rtc-examples/example/advanced/customVideoSource/index.html` |
| RTM 实时消息 | `/rtc-examples/example/others/rtm/index.html`，也可访问 `/rtm/` |
| RTC Token 接口 | `POST /api/rtc/token` |
| 健康检查 | `/healthz` |

访问 `/`、`/rtc-examples/` 或旧的 `/rtc-examples/index.html` 会进入“快速开始”。五个分类各自有独立 URL，页面只展示当前组的模块；点击分类、直接打开、刷新、前进和返回均保留所在分类。已移除“全部”和“框架”。

模块链接位于所属分类路径下，例如 `/rtc-examples/example/basic/basicVideoCall/index.html`、`/rtc-examples/example/advanced/customVideoSource/index.html`。示例页的返回入口回到所属分类，设置使用当前分类地址加 `#settings`。

自定义视频源页面直接读取项目设置中保存的 App ID，并显示为只读；可通过旁边的“修改设置”链接调整。URL 显式传入 `appid` 时优先使用该值。

RTM 位于“其他”组，沿用其独立的 App ID、Token、私有化配置和 SDK 操作界面。Go 将分类内的页面与脚本请求映射到 `web-demo/rtm/`，不需要 npm 构建；原始 RTM JavaScript 包保持不变。

RTC SDK、插件、WASM 和页面静态资源随镜像提供。填写证书后，Go 调用 `scripts/rtc_token.py`，使用 `token_lib/` 在容器内生成 RTC Token。Python 工具只使用标准库，镜像运行无需安装 Locust 或其他 Python 包。

`token_lib/AccessToken2.py` 和 `Packer.py` 来自原项目的通用 Token 实现，保留版权声明。它们提供 RTC 所需的 Token 编解码功能。

## 离线导出及检查

```bash
mkdir -p dist
docker save rtc-demo:v1 | gzip > dist/rtc-demo-v1-linux-amd64-offline.tar.gz
go test -mod=vendor -race ./...
docker logs --tail 100 rtc-demo
docker inspect --format '{{.State.Health.Status}}' rtc-demo
```

直接本地启动需要 Go 和 Python 3，运行 `go run -mod=vendor .`。

HTTPS 可通过 `TLS_CERT_FILE`、`TLS_KEY_FILE` 挂载替换，也支持 `TLS_CERT`、`TLS_KEY` base64 环境变量。`TLS_ENABLED=false` 可用于 HTTP 调试，`PORT` 可覆盖监听端口。摄像头、屏幕共享等功能需要浏览器认可的安全上下文；实际通话需能连接所配置的 RTC 服务。

## 无头浏览器功能探测

`probes/custom-video/` 是独立的文件流探测器，已提取自定义视频源页面的 SDK 初始化、接入点、文件捕获、加入发布、订阅和清理流程。容器自带 Chromium、SDK 和测试视频，不依赖本机 Chrome 或线上网页，不采集摄像头和麦克风。

```bash
cd probes/custom-video
docker compose run --build --rm rtc-probe
```

默认离线逻辑自检；原版 SDK 本地文件轨道检查也在用例内。真实 RTC 模式需显式配置 App ID、接入点、两端 UID/Token 并启用网络。详细说明见 [探测说明](probes/custom-video/README.md)。探测器从应用镜像构建上下文中排除，应用镜像保持独立。

## 私有化 SDK 日志接收

独立接收服务见 [日志接收说明](services/log-receiver/README.md)，部署于 `https://ap.edge.rtcdevelopers.com:6444/upload/v1`。原始请求及解析内容保存在服务器 `/opt/rtc-log-receiver/data/`，不通过 HTTP 暴露文件。
