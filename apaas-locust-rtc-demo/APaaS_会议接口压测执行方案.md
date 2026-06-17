# APaaS 会议接口压测执行方案
---

## 1. 新环境部署
### 1.1 前置要求
安装并启动 Docker，检查端口：

| 端口 | 用途 |
|---:|---|
| 8800 | Go Demo Server + 压测控制台 HTTPS 访问端口 |
| 8089 | Locust Web UI 端口，启动 Locust 压测后使用 |


### 1.2 load 镜像

```bash
mkdir -p /data/apaas_scene
apaas-locust-rtc-demo-v1-offline.tar.gz
 放到/data/apaas_scene下面，然后
cd /data/apaas_scene
docker load -i apaas-locust-rtc-demo-v1-offline.tar.gz

```


### 1.3 run 启动控制台容器

推荐启动命令：

```bash
docker run -d \
  --name rtc-demo \
  -p 8800:8800 \
  -p 8089:8089 \
  -p 5557:5557 \
  -v /opt/results:/results \
  apaas-locust-rtc-demo:v1 both
```
---

## 2. 控制台操作说明

### 2.1 执行顺序

打开 https://服务器IP:8800/web-demo/stress-console/ 控制台页面：
注意：
- 如果浏览器提示证书不受信任，选择继续访问

1. 任务类型选择 `Locust 接口压测`
2. 填写 APaaS Host : http://20.1.125.171
3. App ID 和 App Certificate 默认已填好，如目标环境不同可手动修改
4. 设置用户数 `-u` 可以设置 `5000 到 1000 `
5. 设置启动速率 `-r` 1
6. 设置运行时长，默认 `600s` 可以设置 `12000`
7. 点击「启动任务」


## 3. 指标查看

### 3.1 Locust Web UI 指标

启动 Locust 压测任务后，浏览器访问：

```text
http://服务器IP:8089
```

重点关注：

| 指标 | 说明 |
|---|---|
| Requests | 请求总数 |
| Fails | 失败请求数 |
| Median | 中位响应时间 |
| Average | 平均响应时间 |
| Min / Max | 最小 / 最大响应时间 |
| P95 / P99 | 95 / 99 分位响应时间 |
| RPS | 每秒请求数 |
| Failures | 失败接口明细 |
| Exceptions | 脚本异常明细 |

### 3.2 成功标准

Smoke：

```text
SMOKE_RESULT=SUCCESS
```

Locust 建议标准：

| 指标 | 建议标准 |
|---|---|
| 失败请求数 | 0，或在业务可接受阈值内 |
| HTTP 4xx / 5xx | 无明显持续错误 |
| token 获取 | 成功率 100% |
| 进房接口 | 成功率 100% |
| streamUuid | 每个成功进房用户都能拿到 |
| 更新流状态 | 成功率 100% |
| 响应时间 | P95 / P99 符合业务预期 |

### 3.3 结果文件位置

控制台方式的结果保存在容器内 `/results/web-console/{taskId}/`。

因为启动容器时挂载了：

```bash
-v /opt/results:/results
```

所以宿主机上可以在下面目录查看：

```text
/opt/results/web-console/{taskId}/
```

目录内容示例：

```text
/opt/results/web-console/{taskId}/
├── task.json
├── task.log
├── {prefix}_stats.csv
├── {prefix}_stats_history.csv
├── {prefix}_failures.csv
├── {prefix}_exceptions.csv
└── logs-{taskId}.zip
```

日志包内通常包含：

```text
manifest.txt
results/
apaas_scene_logs/
container_logs/
```

---

## 4. 命令行执行方式

最新镜像统一使用：

```text
apaas-locust-rtc-demo:v1
```

如果是离线部署，先导入镜像包：

```bash
docker load -i apaas-locust-rtc-demo-v1-offline.tar.gz
```

确认镜像：

```bash
docker images | grep apaas-locust-rtc-demo
```

预期能看到：

```text
apaas-locust-rtc-demo   v1   0b40a217684e   ...   261MB
```

### 4.1 启动控制台服务

推荐先启动常驻服务，使用 Web 控制台执行 smoke、locust 和 Demo URL 生成等操作。

```bash
docker run -d \
  --name rtc-demo \
  -p 8800:8800 \
  -p 8089:8089 \
  -p 5557:5557 \
  -v /opt/results:/results \
  apaas-locust-rtc-demo:v1 both
```

端口说明：

| 端口 | 说明 |
|---:|---|
| 8800 | RTC Demo 页面和压测控制台 HTTPS 服务 |
| 8089 | Locust Web UI，压测任务启动后访问 |
| 5557 | Locust Worker 通信端口 |

访问地址：

```text
https://服务器IP:8800/web-demo/stress-console/
```

RTC Demo 页面：

```text
https://服务器IP:8800/web-demo/customVideoSource/
```

Locust Web UI：

```text
http://服务器IP:8089/
```

如果已有同名容器，先删除再启动：

```bash
docker rm -f rtc-demo
```

### 4.2 smoke

如果不用控制台，也可以直接通过命令行执行 smoke 单链路验证。

```bash
docker run --rm --network host \
  -e AGORA_APP_ID='你的 app_id' \
  -e AGORA_APP_CERTIFICATE='你的 app_certificate' \
  -e APAAS_HOST='http://目标APaaS入口' \
  -v /opt/results:/results \
  apaas-locust-rtc-demo:v1 smoke
```

说明：

| 环境变量 | 说明 |
|---|---|
| `AGORA_APP_ID` | Agora 项目的 App ID |
| `AGORA_APP_CERTIFICATE` | Agora 项目的 App Certificate |
| `APAAS_HOST` | APaaS 接口入口地址，例如 `http://172.31.x.x:xxxx` |

### 4.3 locust

命令行方式直接执行接口压测：

```bash
docker run --rm --network host \
  -e AGORA_APP_ID='你的 app_id' \
  -e AGORA_APP_CERTIFICATE='你的 app_certificate' \
  -e HOST='http://目标APaaS入口' \
  -e USERS=50 \
  -e SPAWN_RATE=5 \
  -e RUN_TIME=120s \
  -e PREFIX=stress_50u \
  -v /opt/results:/results \
  apaas-locust-rtc-demo:v1 locust
```

说明：

| 环境变量 | 说明 |
|---|---|
| `HOST` | APaaS 接口入口地址 |
| `USERS` | 并发用户数 |
| `SPAWN_RATE` | 每秒启动用户数 |
| `RUN_TIME` | 压测持续时间，例如 `60s`、`5m` |
| `PREFIX` | 结果文件名前缀 |

### 4.4 不同并发规模参考

| 用户数 | SPAWN_RATE | RUN_TIME | 说明 |
|---:|---:|---|---|
| 1 | 1 | 60s | 单用户验证 |
| 10 | 2 | 90s | 小并发验证 |
| 50 | 5 | 120s | 中等并发 |
| 100 | 10 | 180s | 正式压测 |
| 500 | 20 | 300s | 大并发压测 |
| 1000 | 30 | 600s | 极限压测 |

### 4.5 命令行结果文件

挂载 `-v /opt/results:/results` 后，结果直接保存在宿主机目录：

```text
/opt/results/
├── *_stats.csv
├── *_stats_history.csv
├── *_failures.csv
├── *_exceptions.csv
└── logs/
    └── apaas_test_YYYYmmdd_HHMMSS.log
```

控制台方式执行的任务会保存到：

```text
/opt/results/web-console/{taskId}/
```

---

## 5. 压测目标

当前压测主要覆盖 **APaaS 会议后端接口链路**，模拟大量用户通过 API 完成会议相关动作：

1. 创建会议房间
2. 获取用户进房 token
3. 用户进入会议房间
4. 用户上线
5. 更新音视频流状态
6. 保持在线一段时间
7. 统计接口成功率、失败数、响应耗时

当前压测 **不是** 浏览器 UI 压测，也 **不是** RTC 媒体流真实推拉流压测。

---

## 6. 镜像能力

镜像内置：

| 能力 | 内容 |
|---|---|
| Python | 3.8.10 |
| Locust | 2.24.1 |
| Go | 1.21.5，支持容器内 `go build` |
| Demo Server | `/opt/go_deploy_demo/agora_sdk_server_linux` |
| web-demo | `/opt/go_deploy_demo/web-demo/` |
| 压测脚本 | `/opt/apaas_scene/locustfile.py` |
| 冒烟脚本 | `/opt/apaas_scene/apaas_smoke.py` |
| token 代码 | `/opt/apaas_scene/src/` |
| URL 工具 | `/opt/apaas_scene/gen_demo_urls/` |
| 压测控制台 | `/opt/go_deploy_demo/web-demo/stress-console/` |
| 任务持久化 | 任务元数据写入 `/results/web-console/{taskId}/task.json`，容器重启后自动恢复 |

控制台提供：

- Smoke 单链路验证
- Locust 接口压测参数表单
- Locust Web UI 固定模式
- Demo URL 批量生成
- 任务状态查询
- 实时日志查看
- 日志包下载
- 任务取消
- 容器重启后历史任务恢复

---

## 7. 当前限制与注意事项

### 7.1 不是真实 RTC 媒体压测

只压接口，不推拉音视频流。媒体链路压测需额外接入 RTC SDK 或机器人。

### 7.2 不压浏览器 UI

不打开浏览器、不执行前端页面行为。`web-demo` 由 Go server 托管，用于人工访问。

### 7.3 小规模用户的大房间逻辑需要优化

`locustfile.py` 中大房间固定 500 人。小规模压测时可根据需要调整逻辑。

### 7.4 下线和音频流更新默认没有执行

`scene_offline()` 和 `scene_update_audio_stream()` 方法存在但默认未执行。如需压完整进出房链路或音频流更新接口，可按需打开。

---

## 8. 版本信息

| 版本 | 镜像标签 | 离线包 | 说明 |
|---|---|---|---|
| 最新版 | `py38-2.24.1-go1.21.5-console-persist-webui` | `apaas-locust-with-demo-console-persist-webui-offline.tar.gz` | 支持控制台 + 任务持久化 + Locust Web UI（8089） |


---

## 9. 常用运维命令

停止容器：

```bash
docker stop apaas-console
```

启动容器：

```bash
docker start apaas-console
```

重启容器：

```bash
docker restart apaas-console
```

删除旧容器：

```bash
docker rm -f apaas-console
```

重新部署容器：

```bash
docker rm -f apaas-console

docker run -d \
  --name apaas-console \
  --network host \
  -v /opt/results:/results \
  --restart unless-stopped \
  apaas-locust-with-demo:py38-2.24.1-go1.21.5-console-persist-webui
```

---

## 10. 常见问题排查

| 问题 | 排查方式 |
|---|---|
| 控制台打不开 | 确认访问的是 `https://服务器IP:8800/web-demo/stress-console/` |
| 容器没启动 | 执行 `docker ps -a` 和 `docker logs apaas-console` |
| 8800 端口冲突 | 执行 `ss -lntp \| grep 8800`，停止旧进程后重启容器 |
| 8089 打不开 | 需要先在控制台启动 Locust 压测任务，Locust Web UI 才会监听 8089 |
| 任务结果丢失 | 确认启动时已挂载 `-v /opt/results:/results` |
| 容器名称冲突 | 先执行 `docker rm -f apaas-console` 删除旧容器 |
| 镜像找不到 | 执行 `docker images \| grep apaas-locust-with-demo` 确认 load 是否成功 |

---
