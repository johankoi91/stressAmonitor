# RTC Demo 快速启动

## 离线包

```
apaas-locust-rtc-demo-1.0-offline.tar.gz
```

---

## 一、导入镜像

```bash
docker load -i apaas-locust-rtc-demo-1.0-offline.tar.gz
```

导入完成后确认：

```bash
docker images | grep apaas-locust-rtc-demo
```

---

## 二、启动容器

```bash
docker run -d \
  --name rtc-demo \
  --no-healthcheck \
  -p 8800:8800 \
  -p 8089:8089 \
  apaas-locust-rtc-demo:1.0
```

---

## 三、访问地址

| 页面 | 地址 |
|---|---|
| RTC Demo（视频通话） | `https://服务器IP:8800/web-demo/customVideoSource/` |
| 压测控制台 | `https://服务器IP:8800/web-demo/stress-console/index.html` |
| Locust Web UI（控制台启动任务后） | `http://服务器IP:8089/` |

> 浏览器访问 HTTPS 地址时会提示证书不受信任，点击「继续访问」即可。

---

## 四、RTC Demo 使用说明

页面左侧侧边栏填写参数：

| 字段 | 说明 |
|---|---|
| App ID | Agora 项目的 App ID |
| Token | 可选，不填则使用无鉴权模式 |
| 频道名 | 任意字符串，两端填相同频道名即可互通 |
| User ID | 可选，留空自动分配 |
| 视频源 | 摄像头 或 Sample.mp4（需通过 HTTP 服务访问，不支持 file:// 直接打开） |
| AP Server / Domain / Port | 接入点配置，按实际环境填写 |

点击「加入（主播）」推流，另一端点「加入（观众）」收流。

加入成功后，右侧会弹出**实时统计面板**，显示上下行码率、RTT、丢包率、分辨率、帧率等指标，面板可拖拽移动、折叠收起。
