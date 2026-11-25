# FIFO 输入链路排查

本次只进行代码、二进制发送路径和已有记录分析；不运行测试套件，不新建 RTC 压测任务。

## 已确认的问题与修复

`openFIFO` 原先使用 `syscall.Open(O_WRONLY|O_NONBLOCK)`，未设置 close-on-exec。Go 1.24 的 `os.NewFile` 不会补设该标记，后续 `exec.Command` 启动的 Native 因而可能继承先前发布者的 FIFO 写端，影响句柄归属及 EOF/关闭行为。现改为在 open 时原子设置 `O_CLOEXEC`。

供数完成一次阻塞写后，Ticker 可能仍交付一个较早的时间点；原实现用该时间点判断音频窗口。现使用当前时间判断窗口，避免进入已过期的轮换窗口。

这两项是代码层面确定的缺陷，不代表已经证明它们导致 20 GiB 内存占用。尚未进行运行验证。

## 供数路径与证据边界

- Native 的 SampleSendAudioTask 在打开输入文件前初始化 PacerInfo，随后用 fread 读取一帧，再由 waitBeforeNextSend 按累计 10 ms 目标节奏等待。
- 48 kHz、双声道、PCM16 的每帧大小为 1920 字节，每路目标为 192000 字节/秒。
- Go 在等待入频道和第二路起始延迟期间保持写端打开但不写 PCM，Native 的 fread 因此可能阻塞。这和普通音频文件的可读行为不同。
- Go 另用 10 ms Ticker 供数；写入或调度较慢时 Ticker 会丢弃节拍，原实现没有记录丢失量。双层节奏本身不足以证明发送会降至 2%，也不足以证明内存泄漏。
- Go 共享两份素材数组，每个发布者只持有一帧输入缓冲，没有无界 PCM 列表。FIFO 有内核容量上限。OOM 时 Go 和 Netdata 各约 25 MiB，而主要 RSS 在 Native，不能把 20 GiB 解释为 Go 侧 PCM 排队。
- 旧任务缺少每路 FIFO 等待、容量和积压记录，无法事后确定当时是读端迟缓还是供数/调度迟缓。

## 下一次正常任务自动记录

发流角色所在容器的路径：

```
/home/bbt/console-runs/<task-id>/A/pcm-input.jsonl
```

每路约每 5 秒记录一次，写入长时间受阻时也记录。日志使用独立的有界异步队列（512 条）和缓冲写，不阻塞 PCM 供数；发生丢弃会输出 dropped 计数。每任务最多约 64 MiB，达到上限后输出 truncated 标记。

| 字段 | 含义 |
|---|---|
| at/process/channel/uid/source | 采样时间、进程、频道、用户和音频源 |
| phase | opening、waiting、active、scheduled_pause、closed |
| writtenBytes | 成功写入 FIFO 的累计字节，包含部分写入 |
| expectedBytes/activeMs | 按音频时序计算的应写字节和有效时长，不包含入频道等待和源偏移 |
| bufferedBytes/capacityBytes | Linux 内核 FIFO 当前排队字节和容量；不是 SDK 队列 |
| readBytesApprox | writtenBytes-bufferedBytes；包含 Native libc 的预读，不代表 SDK 已发布或网络已发送 |
| openWaitMs | FIFO 写端等待 Native 读端打开的耗时 |
| writeWaitMs/backpressureCount | 写满后累计等待时间、EAGAIN 次数 |
| missedTickEstimate | 活跃阶段根据 Ticker 时间点间隔估算的丢失节拍 |
| error/pipeError | 写入或内核状态读取错误 |

判读：writtenBytes 远低于 expectedBytes 且 bufferedBytes 长期接近容量、writeWaitMs 持续增加，指向 Native 读端迟缓；管道长期空、等待很少而丢失节拍增加，指向供数/调度迟缓。waiting 阶段 writtenBytes=0 而 Native 内存继续增长，说明增长在 PCM 实际输入之前已发生。任何一项都不能独立作为 SDK 内部泄漏的证明。

此次保留原有节奏与场景模型，未改变进程/UID 分配、角色、静音或音频启动规则。未自动补发错过的音频，不通过批量追赶造成发送突发。
