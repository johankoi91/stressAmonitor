# RTC 文件流探测器

将自定义视频源页面的集成流程提取为独立运行时代码，Node.js 通过 Playwright 调用，不再打开或点击线上页面。

来源流程：`createClient` → `setLocalAccessPointsV2` → 视频文件 `play/captureStream` → 自定义音频/视频轨道 → `join/publish` → 远端 `subscribe/play` → `stop/close/leave`。

## 浏览器是否依赖本机

**Docker 镜像自带 Chromium，运行机器只需要 Docker，不需要安装 Chrome、Node.js、摄像头或声卡。**

SDK 4.24.9 和默认 VP8/Opus 视频均随镜像提供。Node 启动只监听容器内部 `127.0.0.1` 的临时资源服务，浏览器从中读取脚本和媒体；不需要暴露端口，也不依赖原网页服务器。

本机直接用 Node 开发时，需要安装 Playwright 浏览器，或者用 `--executable` 指定已安装的浏览器。这是可选开发方式。

## 目录

```text
run.mjs                    # Node 探测执行器与报告
server.mjs                 # 内部静态资源服务，支持媒体 Range 请求
browser/integration.js     # 独立 RTC 文件流集成逻辑
browser/runtime.html       # 最小浏览器运行环境
sdk/AgoraRTC_N.js           # 包内原版 Agora SDK 4.24.9
sdk-adapter.js             # 逻辑自检使用的可控 SDK 响应
fixtures/probe.webm        # 默认视频，包含音频和视频
config.example.json        # 离线逻辑模式示例
config.live.example.json   # 真实 RTC 模式示例
```

集成代码仅使用视频文件创建媒体轨道。没有 `createCameraVideoTrack`、`createMicrophoneAudioTrack` 调用；不申请媒体设备权限，并拦截设备采集 API，将调用次数写入报告。

## 默认：离线逻辑自检

```bash
docker compose run --build --rm rtc-probe
```

Compose 默认使用 `network_mode: none`。执行 8 项自检：

| 用例 | 内容 |
|---|---|
| `parameter-validation` | App ID、频道、UID 等参数校验 |
| `file-publish` | 文件播放、捕获双轨、加入和发布 |
| `subscriber-flow` | 订阅方加入，不创建本地采集轨道 |
| `remote-subscription` | 基于文件流的远端发布、订阅、播放与统计 |
| `media-cleanup` | 离开、轨道关闭、文件暂停 |
| `join-failure-cleanup` | 失败时清理轨道并可再次加入 |
| `repeat-session` | 同一实例重复加入、离开，不累积事件与播放器 |
| `bundled-sdk-check` | 原版 SDK 创建真实自定义文件轨道并清理，不入会 |

前 7 项使用 SDK 适配器，HTML 视频播放、`captureStream` 和原生 `MediaStreamTrack` 实际执行。最后一项加载包内原版 SDK，验证文件轨道 API。默认模式不连接真实 RTC，也不验证真实码率、丢包或时延。

失败恢复和资源清理属于新提取运行时的处理逻辑；原网页的按钮、设置和界面状态不再作为测试对象，原网页业务代码未改动。

## 使用自己的视频

文件需包含浏览器可解码的音频和视频轨道。WebM（VP8/Opus）可直接用于包内 Chromium；MP4 是否可播放取决于浏览器的编解码支持。

```bash
docker run --rm --init --network none --shm-size=1g \
  -v "$PWD/results:/results" \
  -v /path/to/sample.webm:/media/input.webm:ro \
  -e PROBE_MEDIA=/media/input.webm \
  rtc-demo-probe:v1
```

也可在配置文件中指定 `mediaPath`。留空则使用镜像内默认素材。

## 真实 RTC 模式

复制 `config.live.example.json` 为 `config.local.json`，填写 App ID、接入点、发布端和订阅端 UID/Token。需要绑定特定频道的 Token 时同时填写 `channel`；频道留空则生成独立探测频道。

```bash
PROBE_NETWORK_MODE=bridge docker compose run --rm \
  -v "$PWD/config.local.json:/config/config.json:ro" \
  rtc-probe --config /config/config.json
```

该模式启动两个独立浏览器上下文：订阅端先加入，发布端使用文件流加入并推送，再检查远端订阅和收流统计。Token 从配置读取，不写入报告。

**真实连接模式已提供配置与流程，但本轮未使用真实凭据验证。** 缺少 App ID、接入点或合法双端 UID 时会直接失败，不尝试连接。

## 本机开发

```bash
npm ci
npx playwright install chromium
npm run probe
```

或者显式使用本机浏览器：

```bash
node run.mjs --executable '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
```

选择用例、输出目录及配置：

```bash
node run.mjs --cases file-publish,media-cleanup --output ./results
node run.mjs --config config.example.json
```

## 报告与退出码

输出保存在挂载的 `results/{runId}/`，包含 `report.html`、`report.json`、`browser-log.jsonl` 和失败截图。JSON 包含运行模式、SDK/媒体校验值、媒体状态、事件、设备采集调用次数和实际连接标记。

- `0`：全部所选用例通过。
- `1`：执行完成，但有用例或真实连接失败。
- `2`：配置、浏览器或执行器初始化失败。

详细实测结果见 [VALIDATION.md](VALIDATION.md)。
