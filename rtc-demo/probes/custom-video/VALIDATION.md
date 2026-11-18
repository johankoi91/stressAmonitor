# 独立文件流运行时验证

日期：2026-11-18（Asia/Shanghai）。

| 环境 | 浏览器 | 结果 |
|---|---|---|
| 本机开发（显式指定 Chrome） | 157.0.8103.79 | 8/8 通过 |
| Docker Linux arm64，network=none | 141.0.7390.37 | 8/8 通过 |

镜像：`rtc-demo-probe:v1`。容器只挂载报告目录，没有挂载宿主机浏览器或设备；没有传入摄像头、麦克风权限，也没有使用虚拟设备启动参数。

- 每个用例的 `deviceCaptureCalls` 均为 0。
- 不访问线上页面，SDK 和媒体均从容器内部临时资源服务加载。
- 默认逻辑模式验证文件音视频轨道、加入发布、订阅、重复会话、失败恢复和轨道清理。
- `bundled-sdk-check` 使用包内原版 Agora Web SDK 4.24.9，真实创建和关闭自定义文件轨道，但不调用加入频道。
- 内部媒体服务的 Range 206、非法 Range 416 和未知资源 404 已验证。
- 真实 RTC 模式提供双端配置与调用流程；未使用真实凭据执行连通性验证。缺少配置时在资源服务和浏览器启动前返回退出码 2。

新版本提取的是集成核心，不再操作原网页按钮或项目设置。旧页面探测的 7/8 结果属于历史报告，原网页业务代码没有因此被修改。

- [本机报告](results/standalone-local/2026-11-18T04-43-36-189Z-67175/report.html)
- [断网容器报告](results/standalone-container-final/2026-11-18T04-48-58-477Z-7/report.html)
- [断网容器 JSON](results/standalone-container-final/2026-11-18T04-48-58-477Z-7/report.json)

包内 SDK SHA-256：`63a04a823e6e589a6dd4384e2c175a8e6f70c313f6f58aa88fd92e436ced55fe`。
