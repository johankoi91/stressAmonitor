# Go SDK 音频压测

版本 0.5.0 使用用户提供的 Agora Golang Server SDK 2.6.6，底层 Linux SDK 为 4.6.31.135。原始下载目录保留不变，应用使用 backend/third_party/agora-go 中的本地副本。

## 接收方式

每个 UID 对应一个 RtcConnection。开启 AutoSubscribeAudio、AudioRecvEncodedFrame，关闭视频、音频设备和 EnableAudioRecordingOrPlayout，只注册 AudioEncodedFrameObserver。回调累计编码包、编码字节和来源 UID，随后丢弃数据；不注册 AudioFrameObserver，不启动 Opus/FFmpeg 解码或播放。

发送 UID 同样订阅另一名发送者。A 每频道共接收 4 路，B/C 每频道各接收 2 路；200 频道分别为 800、400、400 路。仅接收数据意味着不能据此评估解码输出或可听音质。

收到的来源覆盖数 receivedStreams 是累计值；activeStreams 是最近 5 秒收到过包的来源数。两者均按每个 Connection 的远端 UID 分别计数。只有入频道或 PCM 发送成功，不能证明接收成功。

## 进程与 Service

本次指定配置为 A 20 进程×30 UID，B/C 各 10 进程×20 UID，总共 40 进程、40 个 Service、1000 条连接。A 每进程承载 10 个完整频道，每频道三人；每个进程只有一个 Service，管理进程内全部 Connection。

SDK 的 Go 包是单例，实际探针进一步确认其 C agora_service_create() 和公开 C++ createAgoraService() 也会返回相同底层实例。因此，这个版本无法完成“同进程多个独立 Service”的比较。页面保留该布局的禁用提示，API 拒绝启动该布局，不将多个 Go 包装对象统计为多个 Service。无需 C++ 适配层，不改二进制或虚表。

本地副本增加受进程锁保护的 Service 生命周期入口与连接归属，限制每进程同时存在一个 Service；创建第二个实例会在初始化前失败。修正编码音频 observer 的清理范围，使相同远端 UID 在不同 Connection 中不会互相注销；退出先清理 Connection，再销毁 Service。PCM 发送缓存上限为 20 帧。

## 发送与时序

所有 UID 先加入频道。三台机器都已加入后，中心下发统一素材开始时间，第二路默认延迟 5000 毫秒，随后持续并发发送。Go 每 10 毫秒通过 AudioPcmDataSender 推送 PCM，无 FIFO 输入链路。发送轨道仅在对应素材时间到达后发布。上传文件、不同测试音和格式校验复用现有音频管理。

rtc-sdk-worker 独立子进程输出 SDK 日志，FD 3 单独传递结构化事件；stdout 里的 SDK 调试信息不会破坏控制协议。子进程受任务停止与父进程退出管理，连接逐进程确认加入后才启动下一个进程。

## 构建部署

中心/Agent 主程序依然 CGO_ENABLED=0。rtc-sdk-worker 必须用 Linux amd64、CGO 和 C 编译器构建，并配套精确版本的动态库。deploy/fetch-agora-sdk.sh 校验固定 SDK 包 SHA256 后解压本地副本所需头文件和动态库。make sdk-worker 使用 deploy/Dockerfile.worker，在兼容生产运行时的 Ubuntu 18 基础镜像内编译，输出 dist/rtc-sdk-worker；make image 将 helper、sdk/*.so 和前端打入应用镜像。

helper 的 LD_LIBRARY_PATH 指向 /opt/rtc-stress-console/sdk，旧 sample 仍使用 /home/bbt 的旧动态库，允许查看旧任务。任务实际引擎保存在配置快照中。Netdata 仍由同一个应用容器管理，监控时间轴保持 5 秒粒度。

## 指标解释

sdk-metrics.jsonl 每秒记录 PID、Service 数、Connection 数、进程 RSS、Go heap、线程数、PCM 输入量、编码接收量、包数、来源覆盖和发送错误。页面总览显示每进程 Service 数和 RSS，任务报告保留全部配置与最新状态。

PCM 输入字节是未压缩数据，编码接收字节是压缩后的负载，两者不能直接对比。整机网络指标包含协议开销、重传以及其他进程流量。编码接收模式可能不产生远端解码统计，ReceiveBitrateKbps 为 0 时应依据编码回调字节差分计算实际接收负载。

当前版本关闭运行日志链路：Worker 标准输出/错误输出送入空设备，SDK 显式使用 LogLevel=0（LOG_LEVEL_NONE），不写 sdk-metrics.jsonl。结构化状态、错误和统计继续同步，详见 README。
