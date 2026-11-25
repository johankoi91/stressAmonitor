# rtc-stress 本地适配

基于用户提供的 Agora Golang Server SDK 2.6.6，原始参考目录不修改。匹配 native 4.6.31.135，固定下载与哈希见 console/deploy/fetch-agora-sdk.sh。

- service_instance.go 增加受锁保护的单 Service 生命周期入口和连接归属。C/C++ 创建接口实际均为进程单例，禁止重复创建独立 Service。
- Connection、媒体工厂、PCM sender 使用所属 Service；C 回调从进程注册表找到对应 Connection。
- 自定义 PCM 使用 direct track；缓存上限 20 个 10 ms 帧，不设置原来的 100000 帧上限。
- 编码音频 observer 的删除同时匹配 Connection 和远端 UID，防止同一 UID 在不同连接互相注销。Connection 退出主动注销 observer。
- !avcodec 的 opus_decoder_pure.go 仅保留原有 Go 占位实现，移除多余的 FFmpeg pkg-config/CGO 声明；本程序不开启 avcodec，不使用解码 API。

此目录不包含任何二进制补丁、vtable 替换或 C++ Service 句柄适配层。SDK 的原有 singleton API 保留供原示例使用；rtc-sdk-worker 只使用新的受保护入口，不混用两套生命周期。

- 0.7.0: Service 初始化后显式调用公开的 agora_service_set_log_filter，确保 LOG_LEVEL_NONE=0 生效；C 初始化函数只复制大于零的日志级别。
