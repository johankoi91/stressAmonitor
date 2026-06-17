# 新机器 Git 拉代码构建并打包镜像

本文档说明在一台全新的 Linux 服务器上，如何从 Git 仓库拉取代码，构建 `apaas-locust-rtc-demo:v1` 镜像，并导出离线镜像包。

目标产物：

```text
apaas-locust-rtc-demo:v1
apaas-locust-rtc-demo-v1-offline.tar.gz
```

---

## 一、环境要求

服务器需要提前具备：

| 依赖 | 说明 |
|---|---|
| Git | 用于拉取代码 |
| Docker | 用于构建和运行镜像 |
| 网络访问 GitHub | 用于 clone 仓库 |
| 磁盘空间 | 建议至少预留 5GB |

检查命令：

```bash
git --version
```

```bash
docker --version
```

如果当前用户不是 `root`，需要确保能执行 Docker：

```bash
docker ps
```

如果提示权限不足，可以临时使用 `sudo docker ...`，或将用户加入 `docker` 组。

---

## 二、拉取代码

进入工作目录，例如：

```bash
cd /opt
```

拉取仓库：

```bash
git clone https://github.com/johankoi91/stressAmonitor.git
```

进入镜像构建目录：

```bash
cd /opt/stressAmonitor/apaas-locust-rtc-demo
```

确认关键文件存在：

```bash
ls -lh
```

需要能看到：

```text
Dockerfile
docker-entrypoint-with-demo.sh
supervisord.conf
locustfile.py
apaas_smoke.py
src/
demo-stress/
```

确认 Go vendor 目录存在：

```bash
ls -lh demo-stress/go_deploy_demo/vendor
```

当前 Dockerfile 使用 vendor 方式构建 Go 服务，所以 `vendor` 目录必须存在。

---

## 三、构建 Docker 镜像

在 `apaas-locust-rtc-demo` 目录下执行：

```bash
docker build -f Dockerfile -t apaas-locust-rtc-demo:v1 .
```

说明：

- `Dockerfile` 会先编译 Go demo server。
- Go 依赖使用仓库内的 `vendor` 目录，不需要构建时在线拉 Go module。
- Python 运行时基于 `python:3.8-slim` 镜像源构建。
- Python 包安装使用清华 PyPI 源。

构建完成后查看镜像：

```bash
docker images | grep apaas-locust-rtc-demo
```

预期输出类似：

```text
apaas-locust-rtc-demo   v1   <IMAGE_ID>   ...   261MB
```

---

## 四、运行验证

如果已有同名容器，先删除：

```bash
docker rm -f rtc-demo
```

启动容器：

```bash
docker run -d \
  --name rtc-demo \
  -p 8800:8800 \
  -p 8089:8089 \
  -p 5557:5557 \
  -v /opt/results:/results \
  apaas-locust-rtc-demo:v1 both
```

查看容器状态：

```bash
docker ps -a --filter name=rtc-demo
```

正常情况下应看到：

```text
rtc-demo   apaas-locust-rtc-demo:v1   Up ...
```

查看日志：

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

验证 RTC Demo 页面：

```bash
curl -k -I https://127.0.0.1:8800/web-demo/customVideoSource/
```

预期结果：

```text
HTTP/2 200
content-type: text/html; charset=utf-8
```

浏览器访问：

```text
https://服务器IP:8800/web-demo/customVideoSource/
```

验证压测控制台页面：

```bash
curl -k -I https://127.0.0.1:8800/web-demo/stress-console/
```

预期结果：

```text
HTTP/2 200
content-type: text/html; charset=utf-8
```

浏览器访问：

```text
https://服务器IP:8800/web-demo/stress-console/
```

---

## 五、打包离线镜像

验证通过后，导出离线镜像包：

```bash
docker save apaas-locust-rtc-demo:v1 | gzip > apaas-locust-rtc-demo-v1-offline.tar.gz
```

查看文件大小：

```bash
ls -lh apaas-locust-rtc-demo-v1-offline.tar.gz
```

也可以使用：

```bash
stat apaas-locust-rtc-demo-v1-offline.tar.gz
```

离线包文件：

```text
apaas-locust-rtc-demo-v1-offline.tar.gz
```

---

## 六、在其他机器导入离线包

将离线包复制到目标机器后执行：

```bash
docker load -i apaas-locust-rtc-demo-v1-offline.tar.gz
```

确认镜像：

```bash
docker images | grep apaas-locust-rtc-demo
```

启动容器：

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

## 七、常见问题

### 1. `git clone` 失败或很慢

可能是服务器访问 GitHub 不稳定。可以尝试：

```bash
git clone --depth 1 https://github.com/johankoi91/stressAmonitor.git
```

如果仍然失败，可以在网络较好的机器上 clone 后打包上传到服务器。

### 2. 构建时报 `vendor` 目录不存在

当前 Dockerfile 使用：

```dockerfile
COPY demo-stress/go_deploy_demo/vendor ./vendor
RUN go build -mod=vendor -o agora_sdk_server_linux .
```

所以必须确保仓库中存在：

```text
demo-stress/go_deploy_demo/vendor/
```

如果缺失，需要在有 Go 环境的机器上执行：

```bash
cd demo-stress/go_deploy_demo
go mod vendor
```

然后重新提交或复制 `vendor` 目录。

### 3. `docker build` 卡在 `pip install`

Dockerfile 中已经使用清华 PyPI 源：

```text
https://pypi.tuna.tsinghua.edu.cn/simple
```

如果仍然很慢，优先检查服务器网络和 DNS。

### 4. 端口被占用

如果启动容器时报端口占用，检查：

```bash
ss -ltnp | grep -E ':(8800|8089|5557)\b'
```

或者：

```bash
docker ps --format "{{.Names}} {{.Ports}}"
```

停止占用端口的进程或容器后再启动。

### 5. 页面 HTTPS 证书不受信任

测试环境证书可能不是浏览器信任的正式证书，浏览器提示风险时选择继续访问即可。

---

## 八、完整命令汇总

```bash
cd /opt

git clone https://github.com/johankoi91/stressAmonitor.git

cd /opt/stressAmonitor/apaas-locust-rtc-demo

docker build -f Dockerfile -t apaas-locust-rtc-demo:v1 .

docker rm -f rtc-demo || true

docker run -d \
  --name rtc-demo \
  -p 8800:8800 \
  -p 8089:8089 \
  -p 5557:5557 \
  -v /opt/results:/results \
  apaas-locust-rtc-demo:v1 both

curl -k -I https://127.0.0.1:8800/web-demo/customVideoSource/

curl -k -I https://127.0.0.1:8800/web-demo/stress-console/

docker save apaas-locust-rtc-demo:v1 | gzip > apaas-locust-rtc-demo-v1-offline.tar.gz
```
