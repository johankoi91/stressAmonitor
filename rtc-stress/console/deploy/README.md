# Docker Compose 部署（0.12.3）

目标机器为 Linux amd64，安装 Docker Engine 和 Docker Compose **v2.23.1 或更新版本**。执行 `docker compose version` 确认插件可用。部署使用 Compose 原生的 profiles、configs 和 volumes。

每台物理机器只启用一个角色：中心选 `center`，执行端选 `agent`。中心自带本机执行节点 A；Netdata v2.13.0 始终运行在各应用容器内部。

## 1. 离线导入镜像

解开 `dist/offline/` 中的最新发布包，进入解压后的目录：

```sh
tar -xf rtc-stress-console-offline-0.12.3-20261125-linux-amd64.tar
cd rtc-stress-console-offline-0.12.3-20261125-linux-amd64
sha256sum -c SHA256SUMS
docker load --input images/rtc-stress-console.tar.gz
docker image inspect rtc-stress-console:0.12.3 --format '{{.Id}}'
cat image-id.txt
```

两处镜像 ID 应相同。Compose 已设置 `pull_policy: never`，启动只使用本地已导入镜像。

后面的操作在包含 `compose.yaml` 的目录中执行。使用项目源文件时，这个目录是 `console/deploy/`。

## 2. 启动中心 A

```sh
cp .env.example .env
```

在 `.env` 中设置：

```dotenv
COMPOSE_PROFILES=center
CONSOLE_PORT=8092
CENTER_DATA_DIR=./data/center
```

```sh
docker compose config --quiet
docker compose up -d
docker compose ps
```

访问 `http://中心IP:8092`。首次启动自动载入 A/B/C/D 的机器配置和两份 Opus 音源，不会自动启动压测。已有数据目录的设置继续保留。执行端按唯一节点 ID 自动接入，无需节点口令。

默认配额为 A/B 各创建 100 个频道、每频道 2 发 1 收，C/D 各加入全部 200 个频道、每频道 1 收，合计 A300、B300、C200、D200 UID。默认两路 Opus 为 128 kbps、48 kHz、单声道、20 ms/帧，第二路延迟 5 秒开始。实际机器地址和 RTC 环境可在页面调整。

## 3. 接入执行机器 B/C/D 或更多机器

在新机器上导入镜像，执行 `cp .env.example .env`。

编辑 `.env`：

```dotenv
COMPOSE_PROFILES=agent
NODE_ID=D
CENTER_URL=ws://114.132.190.201:8092/ws/agent
```

```sh
docker compose config --quiet
docker compose up -d
docker compose ps
```

Compose 根据 `.env` 直接生成只读的执行端配置；无需口令文件、手写 JSON 或初始化脚本。每个执行端使用唯一的节点 ID，A 保留给中心。执行端主动连接中心，在“执行机器”页面确认上线，再配置任务配额。

执行机器需要访问中心 TCP 8092 和目标 RTC 网络。同一物理机器只运行一套中心或执行端，以避免内部 Netdata 的 19999 端口冲突。

## 4. 日常管理与数据

```sh
docker compose ps
docker compose stop
docker compose up -d
docker compose restart
docker compose logs --tail 30
```

日志命令用于排查容器启动或节点连接错误，应用不收集、上传压测运行日志。停止压测请使用页面的“停止任务”。

| 内容 | 配置 / 数据位置 |
| --- | --- |
| 镜像版本、机器角色、节点 ID、中心地址 | `.env` |
| 中心场景、音频、采样历史 | `CENTER_DATA_DIR`，默认 `./data/center` |
| 执行端配置 | Compose `configs` 自动生成 |
| 执行缓存 | `RUNS_VOLUME` 命名卷 |
| Netdata 数据 | `NETDATA_LIB_VOLUME`、`NETDATA_CACHE_VOLUME` 命名卷 |

命名卷由 Compose 自动创建。升级保持 `.env` 的数据路径、卷名不变，导入新镜像并修改 `RTC_IMAGE` 后执行 `docker compose up -d`。移除容器可使用 `docker compose down`；保留数据时不要加 `-v`。

## 5. 从已有容器迁移

先在页面停止压测，备份 `.env` 和中心数据目录，再用 `docker inspect 原容器名` 记录 Mounts 中的宿主机目录与命名卷名称。将 `.env` 中的 `CENTER_DATA_DIR`、`RUNS_VOLUME`、`NETDATA_LIB_VOLUME`、`NETDATA_CACHE_VOLUME` 指向原来的数据。

原执行端 `config.json` 中的 node、center 分别对应 `NODE_ID`、`CENTER_URL`。旧的节点口令文件不再使用。

停止旧容器并改名保留，然后执行 `docker compose up -d`。确认节点上线和页面配置完整后再处理旧容器。Compose 使用固定容器名，已有同名容器时会报冲突，不能同时运行两份。

复用原来由其他工具创建的命名卷时，在同目录添加 `compose.override.yaml`，让 Compose 将它们作为外部卷使用（卷名仍取自 `.env`）：

```yaml
volumes:
  runs:
    external: true
  netdata-lib:
    external: true
  netdata-cache:
    external: true
```

## 6. 构建镜像

在项目根目录执行 `make image`。构建工具为 Go、Node.js、Docker、curl、unzip 和 SHA256 工具。SDK 下载脚本使用 Shell 和 unzip。

`Dockerfile` 构建完整镜像，`Dockerfile.worker` 编译 Go SDK Worker，`Dockerfile.update` 用于在完整基础镜像上更新应用。镜像版本取自 `frontend/package.json`；`defaults/settings.json` 与 `../res/` 中两份 Opus 会一并打入镜像。

标准 Docker 镜像导出命令：

```sh
docker image save rtc-stress-console:0.12.3 | gzip > rtc-stress-console-0.12.3.tar.gz
```

将镜像与 `compose.yaml`、`.env.example` 和本说明一起交付，即可按以上步骤离线启动。
