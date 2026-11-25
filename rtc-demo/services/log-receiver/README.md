# RTC SDK 日志接收服务

部署在 `111.230.192.59:6444`，用于接收原版 Agora RTC Web SDK 私有化日志上传。

- 上传地址：`https://ap.edge.rtcdevelopers.com:6444/upload/v1`
- 客户端 hosts：`111.230.192.59 ap.edge.rtcdevelopers.com`
- 健康检查：`https://ap.edge.rtcdevelopers.com:6444/healthz`
- 容器：`rtc-log-receiver`
- 服务数据：`/opt/rtc-log-receiver/data/` → 容器 `/data`
- TLS：复用 RTC Demo 的 `*.edge.rtcdevelopers.com` 证书，挂载为只读。

## 接口与存储

`POST /upload/v1` 返回 HTTP 200，正文严格为两个字符 `OK`，符合 SDK 的响应检查。支持跨域 OPTIONS 预检，单次请求最大 32 MiB。

每次上传单独保存：

```text
{id}.raw             # 原始请求体，保持字节不变
{id}.request.json    # 时间、来源、请求头、字节数、SHA-256、SDK 版本和进程 ID
{id}.decoded.json    # 解析后的 JSON，SDK payload 字符串展开为数组
{id}.readable.txt    # 便于查看的展开内容
```

支持普通 JSON 和 gzip 压缩 JSON。解析失败时仍保存原始请求和元数据。服务仅提供上传与健康接口，不通过 HTTP 暴露日志文件。

接收时间的机器字段使用带 Z 的 UTC；客户端 SDK 原始时间前缀不修改。

## SDK 配置

```js
client.setLocalAccessPointsV2({
  accessPoints: {
    serverList: ["ap.edge.rtcdevelopers.com"],
    domain: "edge.rtcdevelopers.com",
    port: 443
  },
  log: {
    hostname: ["ap.edge.rtcdevelopers.com"],
    port: 6444
  }
});
AgoraRTC.enableLogUpload();
```

SDK 4.24.9 的旧日志器在进入 `client.join` 流程后调度开始上传；只创建客户端并调用 `enableLogUpload` 会先收集日志，不一定立即产生 POST。服务启动后已收到此前浏览器补传的日志。

## 构建与部署

```bash
go test -race ./...
docker build --platform linux/amd64 -t rtc-sdk-log-receiver:v1 .
docker run -d --name rtc-log-receiver --restart unless-stopped \
  --read-only --memory 256m --cpus 0.5 \
  -p 6444:6444 \
  -v /opt/rtc-log-receiver/certs:/certs:ro \
  -v /opt/rtc-log-receiver/data:/data \
  rtc-sdk-log-receiver:v1
```

证书文件名为 `server.pem`、`server-key.pem`。服务 Go 二进制在 scratch 镜像运行，无外部依赖。

```bash
docker logs --tail 100 rtc-log-receiver
docker inspect --format '{{.State.Health.Status}}' rtc-log-receiver
ls -lh /opt/rtc-log-receiver/data/
```

本地取回的原始文件与汇总位于 `data/`，已从 Git 和镜像构建中排除。
