# RTC Demo 快速启动

本文档只说明 RTC Demo 页面访问与使用：

```text
https://服务器IP:8800/web-demo/customVideoSource/
```

---

## 一、离线镜像包

离线包文件：

```text
apaas-locust-rtc-demo-v1-offline.tar.gz
```

对应镜像：

```text
apaas-locust-rtc-demo:v1
```

---

## 二、导入镜像

在服务器上执行：

```bash
docker load -i apaas-locust-rtc-demo-v1-offline.tar.gz
```

导入完成后确认镜像：

```bash
docker images | grep apaas-locust-rtc-demo
```

预期能看到：

```text
apaas-locust-rtc-demo   v1   0b40a217684e   ...   261MB
```

---

## 三、启动 RTC Demo 服务

如果服务器上已有同名容器，先删除旧容器：

```bash
docker rm -f rtc-demo
```

启动新容器：

```bash
docker run -d \
  --name rtc-demo \
  -p 8800:8800 \
  -v /opt/results:/results \
  apaas-locust-rtc-demo:v1 both
```

说明：

| 参数 | 说明 |
|---|---|
| `--name rtc-demo` | 容器名称 |
| `-p 8800:8800` | 对外暴露 RTC Demo HTTPS 服务 |
| `-v /opt/results:/results` | 保留运行结果目录，便于后续排查 |
| `apaas-locust-rtc-demo:v1` | 当前使用的镜像版本 |
| `both` | 启动内置 Go Demo Server |

---

## 四、确认服务状态

查看容器状态：

```bash
docker ps -a --filter name=rtc-demo
```

正常情况下应看到容器处于 `Up` 状态，并且端口映射包含：

```text
0.0.0.0:8800->8800/tcp
```

查看启动日志：

```bash
docker logs --tail 100 rtc-demo
```

正常日志应包含：

```text
APaaS Locust + Go Demo Server
Mode: both
Starting Go demo server and stress console...
success: go-demo-server entered RUNNING state
```

在服务器本机验证页面是否可访问：

```bash
curl -k -I https://127.0.0.1:8800/web-demo/customVideoSource/
```

预期结果：

```text
HTTP/2 200
content-type: text/html; charset=utf-8
```

---

## 五、浏览器访问

浏览器打开：

```text
https://服务器IP:8800/web-demo/customVideoSource/
```

示例：

```text
https://172.31.11.228:8800/web-demo/customVideoSource/
```

> 浏览器访问 HTTPS 地址时，可能会提示证书不受信任。测试环境下这是正常现象，选择「继续访问」即可。

---

## 六、RTC Demo 页面使用说明

页面左侧侧边栏填写参数：

| 字段 | 说明 |
|---|---|
| App ID | Agora 项目的 App ID |
| Token | 可选，不填则使用无鉴权模式；如果项目开启鉴权，需要填写有效 Token |
| 频道名 | 任意字符串，两端填相同频道名即可互通 |
| User ID | 可选，留空自动分配 |
| 视频源 | 摄像头或 Sample.mp4；自定义视频源需通过 HTTP/HTTPS 地址访问，不支持 `file://` |
| AP Server / Domain / Port | 接入点配置，按实际环境填写 |

使用步骤：

1. 打开两个浏览器窗口，或使用两台设备访问同一个 RTC Demo 页面。
2. 两端填写相同的 `App ID` 和 `频道名`。
3. 如需鉴权，两端填写各自有效的 `Token`。
4. 一端点击「加入（主播）」进行推流。
5. 另一端点击「加入（观众）」进行收流。
6. 加入成功后，页面右侧会显示实时统计面板。

实时统计面板会展示：

- 上行码率
- 下行码率
- RTT
- 丢包率
- 分辨率
- 帧率
- 音视频状态

面板支持拖拽移动和折叠收起。

---

## 七、停止服务

停止并删除容器：

```bash
docker rm -f rtc-demo
```

如果只是临时停止：

```bash
docker stop rtc-demo
```

再次启动：

```bash
docker start rtc-demo
```

---

## 八、常见问题

### 1. 页面打不开

先确认容器是否运行：

```bash
docker ps -a --filter name=rtc-demo
```

再确认端口是否监听：

```bash
ss -ltnp | grep ':8800'
```

如果 `8800` 被其他服务占用，需要先停止占用端口的服务或更换端口映射。

### 2. HTTPS 证书不受信任

测试环境证书可能不是浏览器信任的正式证书，浏览器会提示风险，选择继续访问即可。

### 3. 加入频道失败

重点检查：

- `App ID` 是否正确
- 项目是否开启 Token 鉴权
- 如果开启鉴权，`Token` 是否有效、是否过期
- 两端频道名是否完全一致
- `AP Server / Domain / Port` 是否和当前测试环境一致

### 4. 看不到对端视频

重点检查：

- 一端是否以「主播」加入
- 另一端是否以「观众」加入
- 两端是否在同一个频道
- 浏览器是否允许摄像头权限
- 网络是否能访问当前 AP 接入点

### 5. 查看容器日志

```bash
docker logs -f rtc-demo
```

如需只看最近日志：

```bash
docker logs --tail 200 rtc-demo
```
