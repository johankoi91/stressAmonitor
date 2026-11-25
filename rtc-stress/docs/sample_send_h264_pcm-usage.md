# sample_send_h264_pcm 使用说明

整理日期：2026-11-23。适用于当前 rtc-stress 的 Linux amd64 二进制，以及 81.71.150.26 上的 rtc-stress 容器。

本文按五类场景整理，说明收发模型、可复制的命令、参数和进程数量。完整参数、默认值、短参数及核对证据见 [参数核对文档](sample_send_h264_pcm-parameters.md)。

## 1. 场景总览

| 场景 | 使用入口 | 收发模型 | 本文示例规模 |
|---|---|---|---|
| 单用户发流 | 直接运行 sample_send_h264_pcm | 一个 UID 发音视频，不订阅 | 1 进程、1 频道、1 连接 |
| 多频道 1v1 | 直接运行 sample_send_h264_pcm，以 -O 组合连接 | 每频道两人发视频，其中一人发音频，双方互相订阅 | 1 进程、2 频道、4 连接 |
| 单频道大量观众 | stress_huge.py | 多个观众只接收，需要另有发布端 | 10 进程、1 频道、200 连接 |
| 多频道一人发布、多人接收 | stress_jktuchuan.py | 每频道一人发音视频，其余用户关闭发布，所有用户订阅音视频 | 15 进程、15 频道、75 连接 |
| 多频道五人收发 | stress_special_2.py | 每频道一人发音视频、四人只发视频，所有用户订阅音视频 | 15 进程、15 频道、75 连接 |

这里的连接数按“频道＋UID”统计。人数和进程数是命令计划值，实际加入频道和收发成功情况需要检查运行结果。

## 2. 运行准备

先进入部署服务器，再进入容器：

~~~bash
docker exec -it rtc-stress bash
cd /home/bbt
export LD_LIBRARY_PATH="$PWD"
~~~

独立运行时，将工作目录切换到二进制、SDK 动态库和媒体文件所在目录，再设置 LD_LIBRARY_PATH。主要依赖包括 libagora_rtc_sdk.so、libaosl.so、libagora-fdkaac.so。

每次运行前，设置本次目标 RTC 环境的凭据：

~~~bash
export RTC_TOKEN='<目标 RTC 环境的 Token/AppId>'
~~~

下文保留各场景示例中的 AP、证书域名和 VOS 地址，Token 用变量代入。这些地址来自不同环境，RTC_TOKEN 必须与本次目标环境匹配。给接收端配套的发布端，应使用相同的 RTC 环境和频道；不要直接把不同环境的两条示例配成收发对。

命令分行时，每个续行末尾的反斜杠必须保留，且后面不能有空格。直接调用 Native 和调用 Python 编排脚本的参数语法不同：

~~~text
Native：--uid=10001             长参数使用等号
Native：-R 0、-c agora0001      短参数和值分开
Python：--total 200             Python 脚本按 argparse 的参数形式传值
~~~

## 3. 先理解进程、频道和 UID

一个 sample_send_h264_pcm 进程可以管理多条 RTC 连接，每条连接有自己的频道、UID、角色和收发配置。

~~~text
一个 sample_send_h264_pcm 进程
├── agora1001
│   ├── UID 10001
│   └── UID 10002
└── agora1002
    ├── UID 10001
    └── UID 10002
~~~

上图是 1 个进程、2 个频道、4 条连接。UID 10001 在两个频道中分别创建连接，不能只按 UID 值去重统计。

- 使用 -O 或 --next-channel 开始下一条连接，不要求切换到不同频道。
- 每条连接重新从默认配置开始，不继承前一条的 Token、频道、AP、媒体和订阅设置。
- 一条命令包含 N 条连接时，应有 N−1 个 -O，末尾不要再追加 -O。
- 帧率、采样率、声道数、mode、hold 为进程级参数，由进程中的所有连接共用。
- 角色、发布、订阅、媒体文件、AP、VOS 等为连接级配置，应为每条连接分别填写。

## 4. 单用户发流

一个 UID 发音视频，不订阅。示例加入 agora0001，UID 为 999999，视频使用 ceshi_1080p.h264、15 FPS，音频使用 zxkgf.wav。

~~~bash
export LD_LIBRARY_PATH=./
nohup ./sample_send_h264_pcm \
  --video-rate=15 \
  --token="$RTC_TOKEN" -c agora0001 -R 0 --uid=999999 \
  --audio-input=zxkgf.wav --video-input=ceshi_1080p.h264 \
  --ap=172.31.11.174 --ap_cert=ap.1452738.agora.local \
  --a-sub-flag=0 --v-sub-flag=0 \
  --mute-audio=0 --mute-video=0 --use-aut=0 \
  --mode=0 --hold=999999999 \
  > single_publisher.log 2>&1 &
printf '%s\n' "$!" > single_publisher.pid
~~~

| 配置 | 含义 |
|---|---|
| -R 0 | 主播角色，可以发布和订阅 |
| mute-audio=0、mute-video=0 | 音视频不静音 |
| a-sub-flag=0、v-sub-flag=0 | 不订阅音视频 |
| 未指定 ip/port | 由目标接入环境分配媒体服务 |

hold 的单位是毫秒。999999999 毫秒约为 11.57 天；短时验证可改为 60000，表示主循环约运行 60 秒，完整进程时间还包含初始化和等待。默认 hold=0 不表示无限运行。

## 5. 多频道 1v1

每个频道中，两个 UID 都发视频，其中 UID 10001 发音频，UID 10002 关闭音频发布；双方都订阅音视频。

下面一条命令在同一个进程中运行两个频道的四条连接，使用三个 -O：

~~~bash
export LD_LIBRARY_PATH=./
nohup ./sample_send_h264_pcm \
  --video-rate=15 --mode=0 --hold=999999999 \
  --token="$RTC_TOKEN" -c agora1001 -R 0 --uid=10001 \
  --audio-input=zxkgf.wav --video-input=ceshi_720p.h264 \
  --ip=55.145.1.200 --port=4001 \
  --ap=55.145.1.200 --ap_cert=ap.1145326.agora.local \
  --a-sub-flag=1 --v-sub-flag=1 \
  --mute-audio=0 --mute-video=0 --use-aut=0 \
  -O \
  --token="$RTC_TOKEN" -c agora1001 -R 0 --uid=10002 \
  --video-input=ceshi_720p.h264 \
  --ip=55.145.1.200 --port=4001 \
  --ap=55.145.1.200 --ap_cert=ap.1145326.agora.local \
  --a-sub-flag=1 --v-sub-flag=1 \
  --mute-audio=1 --mute-video=0 --use-aut=0 \
  -O \
  --token="$RTC_TOKEN" -c agora1002 -R 0 --uid=10001 \
  --audio-input=zxkgf.wav --video-input=ceshi_720p.h264 \
  --ip=55.145.1.200 --port=4001 \
  --ap=55.145.1.200 --ap_cert=ap.1145326.agora.local \
  --a-sub-flag=1 --v-sub-flag=1 \
  --mute-audio=0 --mute-video=0 --use-aut=0 \
  -O \
  --token="$RTC_TOKEN" -c agora1002 -R 0 --uid=10002 \
  --video-input=ceshi_720p.h264 \
  --ip=55.145.1.200 --port=4001 \
  --ap=55.145.1.200 --ap_cert=ap.1145326.agora.local \
  --a-sub-flag=1 --v-sub-flag=1 \
  --mute-audio=1 --mute-video=0 --use-aut=0 \
  > pair_1001_1002.log 2>&1 &
printf '%s\n' "$!" > pair_1001_1002.pid
~~~

每频道的配置和预期流向：

| UID | 发布音频 | 发布视频 | 接收对方媒体 |
|---|---|---|---|
| 10001 | 是 | 是 | 视频 |
| 10002 | 否 | 是 | 音频和视频 |

两个 UID 都是 -R 0，主播角色同样可以订阅。未开启 dual，两个频道的媒体各自独立；每频道按配置有 1 路音频发布和 2 路视频发布。

规模扩展：

- 70 个频道、每频道 2 条连接，按每进程 2 个频道装载，为 35 个进程、140 条连接。
- 如果需要 70 个进程且每进程 2 条连接，应每进程只装一组 1v1，对应 70 个频道、140 条连接。
- 多进程运行时分配不同频道和日志文件，不要直接重复启动相同频道、相同 UID 的整条命令。

## 6. 单频道大量观众：stress_huge.py

同一个频道放入大量观众，所有用户只接收，需要另外准备发布端。以下沿用你的示例：200 个观众，每进程 20 人，启动间隔为 0。

~~~bash
python3 stress_huge.py \
  --total 200 \
  --token "$RTC_TOKEN" \
  --local_ap 175.178.45.250 \
  --local_ap_cert ap.1145326.agora.local \
  --cname agora0001 --uid_start 10001 \
  --wait 0 --method start \
  --user_per_proc 20 \
  --subscribe_high_count 1
~~~

实际生成结果：

| 项目 | 数量或值 |
|---|---|
| 频道 | agora0001，一个频道 |
| 观众 UID | 10001～10200，共 200 条连接 |
| 每进程连接数 | 20 |
| Native 进程数 | ceil(200/20) = 10 |
| Native 角色 | 固定 -R 1，跳过发送初始化和发送线程 |
| 音视频订阅 | 都开启 |
| 接入 | AP 175.178.45.250；未指定 vos_info，未固定 VOS |
| 日志 | 1.log～10.log，每进程一份 |

进程与 UID 分组：

~~~text
进程 1：UID 10001～10020
进程 2：UID 10021～10040
...
进程 10：UID 10181～10200
~~~

主要参数：

| 参数 | 含义 |
|---|---|
| --total | 观众总数 |
| --user_per_proc | 每进程最大连接数 |
| --cname、--uid_start | 统一频道名、起始 UID |
| --wait | 每次启动 Native 进程后的间隔，单位秒；这里设为 0 |
| --local_ap、--local_ap_cert | 接入地址与证书域名 |
| --vos_info IP:端口 | 可选，固定 VOS，例如 175.178.45.250:4001 |
| --subscribe_high_count、--subscribe_low_count | 上层生成大小流订阅名单的数量参数，需要结合当前实现限制理解 |
| --speaker_start、--speaker_end | 生成订阅名单时使用的主播 UID 范围 |
| --method start/stop | 启动或停止该频道相关进程；必须显式指定 |

当前实现限制：

- --subscribe_high_count 1 **不能保证只订阅一名主播的大流**：脚本生成的大流名单被 UserBase 丢弃，最终没有传入 Native 的 v-high-sub-uids。
- 这条示例没有设置主播 UID 范围，小流名单也为空，Native 因此进入全部音视频订阅分支。
- 若目标就是全部接收，可把 subscribe_high_count 和 subscribe_low_count 都显式设为 0，语义更清楚；精确订阅需要先修正脚本的名单传递逻辑。
- 即使脚本写入媒体素材和 dual 参数，role=1 也不会因此变成发布端。它不等于“200 路纯音频”，因为视频订阅同样开启。
- 配套发布端必须与这些观众处于同一 RTC 环境、同一频道，并使用不冲突的 UID。

## 7. 多频道一人发布、多人接收：stress_jktuchuan.py

每频道一个主发布者发音视频，其余用户关闭音视频发布。主发布者和其他用户均开启音视频订阅。

下面以 15 个频道、每频道 1 个主发布者加 4 个接收用户为例。显式使用现有素材文件：

~~~bash
python3 stress_jktuchuan.py \
  --channel_count 15 --user_per_process 5 \
  --token "$RTC_TOKEN" \
  --local_ap 55.145.1.200 \
  --local_ap_cert ap.1145326.agora.local \
  --video_fps 15 \
  --cname_base stress_poc --number_start 1000 --uid_base 10000 \
  --video_high_file ceshi_1080p.h264 \
  --video_low_file ceshi_360p.h264 \
  --method start \
  --ip 55.145.1.20 --port 4001
~~~

每个频道的配置：

| 用户 | UID | 发音频 | 发视频 | 订阅 |
|---|---|---|---|---|
| 主发布者 | 固定 999999 | 是 | 是，video_high_file | 音视频 |
| 其他用户 | uid_base × 频道编号 + 1、+2…… | 否 | 否 | 音视频 |

其他用户的 Native 角色仍然是 -R 0，通过 mute-audio=1、mute-video=1 关闭发布；这与 stress_huge.py 中 role=1 的观众初始化路径不同。video_low_file 虽然被写入参数，但这组用户关闭了视频发布。两组文件名也不表示开启了单用户双流。

本例规模：

| 项目 | 数量或值 |
|---|---|
| 频道范围 | stress_poc_1000～stress_poc_1014，共 15 个 |
| 每频道连接数 | 1 + 4 = 5 |
| 总连接数 | 15 × 5 = 75 |
| 主发布者阶段 | 15 条连接，每进程 5 条，共 3 个进程 |
| 接收用户阶段 | 60 条连接，每进程 5 条，共 12 个进程 |
| 总 Native 进程数 | 3 + 12 = 15 |
| 日志 | 1.log～15.log，每进程一份 |

第一个频道 stress_poc_1000 的其他 UID 为 10000001～10000004。不同频道复用主发布者 UID 999999 是有效的，频道内不要出现重复 UID。

启动分两个阶段：先启动所有频道的主发布者并刷出剩余连接，再额外等待 0.5 秒，最后启动其他用户。每次启动 Native 进程后也固定等待 0.5 秒，包括每阶段最后一个进程。固定等待不表示已经确认主发布者入频道或发流成功。

### 人数与进程数的参数关系

当前脚本把 user_per_process 同时用于两个地方：

~~~text
每进程最多连接数 = user_per_process
每频道其他用户数 = user_per_process − 1
每频道总连接数 = user_per_process
~~~

因此修改 user_per_process 不仅改变进程密度，也会改变每频道人数和总负载。它不是“只调整每进程人数”的独立参数。

设 C=channel_count、P=user_per_process，且两者均大于等于 1：

~~~text
总连接数 = C × P
总进程数 = ceil(C/P) + ceil(C×(P−1)/P)
~~~

两个阶段分别分组，主发布者阶段的剩余进程空间不继续装其他用户；进程内可以跨频道。P=1 时只有主发布者，没有其他用户。

其他参数注意点：

- --wait 虽然可以传入，但当前脚本没有实际使用，进程间隔仍固定为 0.5 秒。
- --ip 与 --port 需同时填写，否则不会追加固定 VOS 参数。示例中 AP 为 55.145.1.200，VOS 为 55.145.1.20:4001，两者是不同地址。
- video_high_file 控制主发布者视频，不要把它与 Native 的 v-high-sub-uids 订阅名单混淆。
- 当前网页的预计进程数采用另一条公式，可能高估；应以实际生成的进程计划为准。例如 C=15、P=5，脚本为 15 个进程，而网页预计 27 个。

## 8. 多频道五人收发：stress_special_2.py

每个频道固定五名用户：一个主发布者发音频和视频，另外四人只发视频；五人均订阅音视频。与上一节相比，其他四人开启视频发布，会形成多方视频收发流量。

~~~bash
python3 stress_special_2.py \
  --channel_count 15 --user_per_process 5 \
  --token "$RTC_TOKEN" \
  --local_ap 55.145.1.200 \
  --local_ap_cert ap.1145326.agora.local \
  --video_fps 15 \
  --cname_base stress_poc --number_start 1000 --uid_base 10000 \
  --video_high_file ceshi_1080p.h264 \
  --video_low_file ceshi_360p.h264 \
  --wait 2 --method start \
  --ip 55.145.1.20 --port 4001
~~~

| 用户 | UID | 发音频 | 发视频 | 订阅 |
|---|---|---|---|---|
| 主发布者 | 每频道 999999 | 是 | 是，video_high_file | 音视频 |
| 另外四人 | uid_base × 频道编号 + 1～4 | 否 | 是，video_low_file | 音视频 |

五个人的 Native 角色都是 -R 0。两组用户使用不同的视频素材，没有开启单用户的双流发布。主发布者也订阅另外四人的视频。

本例为 15 个频道、75 条连接：主发布者阶段 15 条连接分成 3 个进程，其他用户阶段 60 条连接分成 12 个进程，总共 15 个进程。频道范围为 stress_poc_1000～stress_poc_1014，进程日志为 1.log～15.log。

启动时先发出全部主发布者的启动命令，额外等待 10 秒，再启动其他用户。每次启动进程后按 --wait 等待，本例为 2 秒，包括每阶段最后一次启动。固定等待不代表已经确认实际发流。

这里 user_per_process **只控制每进程最多连接数**，每频道其他用户固定为 4，不会随该参数改变。设 C=channel_count、P=user_per_process：

~~~text
总连接数 = C × 5
总进程数 = ceil(C/P) + ceil(4C/P)
~~~

该示例显式指定 ceshi_1080p.h264 与 ceshi_360p.h264。AP 和 VOS 沿用你提供的不同地址，运行前应匹配目标部署。此场景与上一节复用了频道前缀和 UID 规则，分别运行时可调整前缀以区分任务。

## 9. Native 参数速查

### 进程级配置

| 参数 | 默认值 | 含义 |
|---|---|---|
| --video-rate= | 30 | 视频发送 FPS，整个进程共用 |
| --audio-rate= | 48000 | 音频采样率，应与素材匹配 |
| --audio-channels= | 2 | 音频声道数，应与素材匹配 |
| --mode= | 0 | 本文场景使用 0 |
| --hold= | 0 | 主循环持续时间，单位毫秒；0 不表示无限运行 |

zxkgf.wav 的文件头为 48 kHz、双声道、16 bit PCM；sample 对 WAV 文件头的处理尚未完整核对。不同连接需要不同帧率等全局配置时，应拆成不同进程。

### 连接级配置

| 参数 | 含义 |
|---|---|
| --token=、--cname=、--uid= | 凭据、频道、用户 ID |
| --role=0 / -R 0 | 主播，可发布和订阅；R 表示角色 |
| --role=1 / -R 1 | 观众，只接收，跳过发送线程 |
| --audio-input=、--video-input= | 音频素材和 H.264 视频素材 |
| --mute-audio=0/1、--mute-video=0/1 | 初始静音开关：0 不静音，1 静音 |
| --a-sub-flag=0/1、--v-sub-flag=0/1 | 音频、视频订阅开关 |
| --a-sub-uids=10001,10002 | 指定音频订阅 UID |
| --v-high-sub-uids=10001 | 指定视频大流订阅 UID |
| --v-low-sub-uids=10002 | 指定视频小流订阅 UID |
| --dual=1、--dual-video-file= | 开启发布双流并指定小流文件；大流使用 video-input |
| --ap=、--ap_cert= | Local AP 与证书域名；ap_cert 不是证书文件路径 |
| --ip=、--port= | 固定 VOS/媒体服务地址，不表示本机绑定地址 |
| --use-aut=0/1 | AUT 相关配置开关，具体网络行为需结合环境验证 |

订阅开关为 0 时关闭对应订阅。音频开关为 1、音频 UID 名单为空时进入全部音频订阅；视频开关为 1、大小流名单都为空时进入全部视频订阅。名单非空时按名单订阅。开启订阅不代表对端已经发布相应媒体。

这些是 Native 参数，total、channel_count、user_per_proc、user_per_process 等属于 Python 编排参数，不能直接传给 sample_send_h264_pcm。

## 10. 查看、停止与结果确认

单进程例子已将 PID 写入独立文件，可查看日志：

~~~bash
tail -f single_publisher.log
~~~

停止单用户示例之前，先核实 PID 对应的命令，再发送 SIGINT：

~~~bash
sample_pid=$(cat single_publisher.pid)
ps -p "$sample_pid" -o pid=,args=
# 确认仍为目标任务后执行：
kill -INT "$sample_pid"
~~~

停止一个 Native 进程会同时结束它管理的所有连接。批量脚本的进程输出为编号.log，不同任务应使用独立工作目录，避免重用日志名或误停其他任务。

stress_huge.py 的 --method stop 按进程名和频道文本匹配停止相关进程；它不是精确的任务 ID 匹配，使用重叠频道名或多任务时需核实匹配范围。stress_jktuchuan.py 的 --method stop 及 stopAll.sh 按进程名停止当前可见范围内的 sample，不能仅停止某个频道或某个 UID；stopAll.sh 还会清理日志和缓存。

确认结果时分别检查：进程启动、成功加入频道、实际发布、实际接收。脚本退出成功或进程数量达到目标，不能单独证明媒体收发成功。

## 11. 常见问题与验证范围

| 现象 | 优先检查 |
|---|---|
| Unknown Commands | Native 长参数是否使用等号；不要用 Python 参数形式调用 Native；该版本没有可用的 --help |
| Parse Fail Commands | 数字格式、负值，或 0/1 开关误传 true/false |
| Cname not found or token not found | 每个 -O 后是否填写 Token 和频道；命令末尾是否多写 -O |
| 进程存在但没有发送 | Native 角色、媒体文件路径、静音设置、实际连接结果与 SDK 日志 |
| 观众没有收到媒体 | 发布端是否在相同环境和频道，是否已经发布，订阅开关和名单是否正确 |
| 实际订阅用户多于预期 | 上层是否丢弃名单；非法 UID 被解析器忽略后是否变成空名单 |
| 全部连接帧率相同 | video-rate 为全局参数；重复赋值以最后一次为准 |
| 缺少动态库 | 工作目录、LD_LIBRARY_PATH 和 SDK 依赖是否完整 |

文档中的规模依据源码及拦截生成的命令核对；Native 参数语法和配置作用域已做离线核对。命令解析不等于 SDK 配置、鉴权或真实收发已验证。VOCS 格式、video-type、AUT 加密组合、其他 mode 和私有 SDK 参数的完整语义，见参数核对文档中的验证范围。
