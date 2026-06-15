# RTC 压测容器 (rtc-stress-docker)

基于 Agora RTC SDK 的压测工具容器，提供 HTTP 接口控制压测任务的启动/停止，支持多路推流场景。

---

## 目录结构

```
rtc-stress-docker/
├── Dockerfile                  # 标准构建文件（FROM daocloud 镜像源）
├── Dockerfile.local            # 离线构建文件（基于已有本地镜像）
├── deploy.sh                   # 一键部署脚本
├── meeting_container_server.py # HTTP 控制服务（8088 端口）
├── stress_test.py              # 压测主脚本
├── stress_meeting.py           # 会议场景压测
├── stress_huge.py              # 大规模压测
├── stress_special_2.py         # 特殊场景压测
├── stress_jktuchuan.py         # 极客推流压测
├── case1_250.py                # Case1: 250 路推流
├── case3_200au.py              # Case3: 200 路纯音频
├── sample_send_h264_pcm        # Agora 推流可执行文件
├── libagora_rtc_sdk.so         # Agora RTC SDK 动态库
├── libaosl.so                  # Agora OSL 动态库
├── libagora-fdkaac.so          # FDK-AAC 编码库
├── ceshi_1080p.h264            # 1080p 测试视频
├── ceshi_720p.h264             # 720p 测试视频
├── ceshi_360p.h264             # 360p 测试视频
├── ceshi_180p.h264             # 180p 测试视频
├── low.h264                    # 低分辨率测试视频
└── zxkgf.wav                   # 测试音频文件
```

---

## 快速部署

### 方式 A：从源码 build（推荐，适合有网络的机器）

```bash
git clone https://github.com/johankoi91/stressAmonitor.git
cd stressAmonitor/RTC/rtc-stress-docker

# 内网环境自动使用 daocloud 镜像源拉取 ubuntu:18.04
docker build -t rtc_stress:latest .

docker run -d --name rtc-stress --restart=unless-stopped \
  --privileged --pid=host --net=host \
  --ulimit core=-1 --security-opt seccomp=unconfined \
  rtc_stress:latest
```

> 内网无法访问 Docker Hub 时，Dockerfile 中 `FROM` 已配置为 `docker.m.daocloud.io/library/ubuntu:18.04`，可直接使用。

---

### 方式 B：加载现有镜像包（适合完全离线的新环境）

```bash
# 1. 从已有环境导出镜像
docker save rtc_stress:latest | gzip -1 > rtc_stress_$(date +%Y%m%d).tar.gz

# 2. 传到新环境
scp rtc_stress_*.tar.gz root@<目标机IP>:/data/

# 3. 在新环境上加载并启动
docker load < /data/rtc_stress_*.tar.gz
docker run -d --name rtc-stress --restart=unless-stopped \
  --privileged --pid=host --net=host \
  --ulimit core=-1 --security-opt seccomp=unconfined \
  rtc_stress:latest
```

---

### 方式 C：使用 deploy.sh 一键部署

```bash
# 自动检测本地镜像 / 加载镜像包 / 启动容器
bash deploy.sh

# 可选参数
bash deploy.sh --name my-stress   # 指定容器名（默认 rtc-stress）
bash deploy.sh --port 9088        # 指定端口（默认 8088）
bash deploy.sh --build            # 强制重新 build 后启动
```

---

## 更新代码后重新部署

```bash
# 1. 本地修改代码，push 到 GitHub
git add -A && git commit -m "改动描述" && git push

# 2. 在部署机上同步代码，重新 build
git pull
docker build -t rtc_stress:latest .

# 3. 重启容器
docker stop rtc-stress && docker rm rtc-stress
docker run -d --name rtc-stress --restart=unless-stopped \
  --privileged --pid=host --net=host \
  --ulimit core=-1 --security-opt seccomp=unconfined \
  rtc_stress:latest
```

---

## HTTP 接口

服务启动后监听 `8088` 端口。

| 接口 | 方法 | 说明 |
|------|------|------|
| `/` | GET | 控制台页面 |
| `/channel-users` | GET | 查看当前频道任务 |
| `/start` | POST | 启动压测任务 |
| `/stop` | POST | 停止压测任务 |
| `/stopAll` | POST | 停止所有任务 |

访问地址：`http://<服务器IP>:8088/`

---

## 注意事项

- 二进制文件（`.so`、`.h264`、`.wav`、`sample_send_h264_pcm`）直接存储在 Git 仓库中，不使用 Git LFS，确保 `docker build` 时 `COPY` 能拿到真实文件内容
- 容器使用 `--net=host` 网络模式，不需要额外映射端口
- `--privileged` 和 `--pid=host` 用于支持压测场景下的系统级操作
