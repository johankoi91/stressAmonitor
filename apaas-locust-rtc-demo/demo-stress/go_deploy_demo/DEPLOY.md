# agora_sdk_server 部署文档

## 概述

这是一个基于 Go + Gin 框架的 HTTPS 静态文件服务器，用于托管 web-demo 目录下的前端项目。服务运行在 **8800 端口**，支持预压缩 gzip 静态资源的正确分发。

---

## 目录结构

部署到服务器后的目录结构如下：

```
/root/agora_sdk_server/
├── agora_sdk_server_linux      # 可执行二进制文件（Linux amd64）
├── web-demo/
│   ├── key/
│   │   ├── edge.rtcdevelopers.com.pem      # SSL 证书（有效期至 2027-01-13）
│   │   └── edge.rtcdevelopers.com-key.pem  # SSL 私钥
│   ├── 20260605_5455/          # 灵动会议前端项目
│   │   ├── index.html
│   │   ├── *.js                # 预压缩 gzip 的 JS 文件
│   │   ├── *.css
│   │   ├── assets/
│   │   └── extensions/
│   └── ...                     # 其他 web-demo 项目目录
```

---

## 部署步骤

### 1. 在本地编译 Linux 版本二进制

> 需要本地安装 Go 1.18+，在 macOS 上交叉编译 Linux 版本。

```bash
cd /path/to/agora_sdk_server
GOOS=linux GOARCH=amd64 go build -o agora_sdk_server_linux .
```

### 2. 上传文件到目标服务器

```bash
# 创建远程目录
ssh root@<SERVER_IP> "mkdir -p /root/agora_sdk_server"

# 上传二进制文件
scp agora_sdk_server_linux root@<SERVER_IP>:/root/agora_sdk_server/

# 上传 web-demo 目录（包含静态资源和 SSL 证书）
scp -r web-demo root@<SERVER_IP>:/root/agora_sdk_server/
```

### 3. 启动服务

```bash
ssh root@<SERVER_IP>

cd /root/agora_sdk_server
chmod +x agora_sdk_server_linux
nohup ./agora_sdk_server_linux > server.log 2>&1 &
```

### 4. 验证服务是否启动成功

```bash
# 检查进程
ps aux | grep agora_sdk_server_linux | grep -v grep

# 检查端口监听
ss -tlnp | grep 8800

# 本地测试 HTTPS 响应
curl -sk https://localhost:8800/web-demo/20260605_5455/index.html -o /dev/null -w "%{http_code}"
# 期望返回：200
```

---

## 访问地址

服务启动后，通过以下 URL 访问前端项目：

| 访问方式 | URL |
|---|---|
| 简短地址（自动重定向） | `https://edge.rtcdevelopers.com:8800/20260605_5455/index.html` |
| 完整路径 | `https://edge.rtcdevelopers.com:8800/web-demo/20260605_5455/index.html` |

新增项目只需将目录放到 `web-demo/` 下，按同样的规则访问即可，无需重启服务。

---

## 路由说明

| 路由规则 | 说明 |
|---|---|
| `/web-demo/*` | 直接提供静态文件 |
| `/:tag/*filepath` | 短链接，301 重定向到 `/web-demo/:tag/*filepath` |

---

## SSL 证书

- 证书域名：`*.edge.rtcdevelopers.com`
- 证书路径：`web-demo/key/edge.rtcdevelopers.com.pem`
- 私钥路径：`web-demo/key/edge.rtcdevelopers.com-key.pem`
- 有效期：**2026-01-14 ~ 2027-01-13**

更换证书时，将新证书文件替换到上述路径，重启服务即可。

---

## 停止 / 重启服务

```bash
# 停止
pkill -f agora_sdk_server_linux

# 重启
cd /root/agora_sdk_server
nohup ./agora_sdk_server_linux > server.log 2>&1 &
```

---

## 常见问题排查

**页面空白 / JS SyntaxError**

前端 JS/CSS 文件是预压缩的 gzip 格式，服务器已内置自动检测并设置 `Content-Encoding: gzip` 响应头，浏览器可正常解压。若遇到此问题，检查是否使用的是本文档对应版本的二进制文件。

**端口被占用**

```bash
ss -tlnp | grep 8800
pkill -f agora_sdk_server_linux
```

**DNS 未解析（本地测试）**

在目标机器的 `/etc/hosts` 中添加：

```
127.0.0.1 edge.rtcdevelopers.com
```

**查看运行日志**

```bash
tail -f /root/agora_sdk_server/server.log
```
