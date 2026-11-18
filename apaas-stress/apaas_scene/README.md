# APaaS 压测场景

`locustfile.py` 保持原始内容，Web 控制台通过同目录 `locust_entry.py` 注入项目凭据。用户数、启动速率和运行时长仍由原脚本解析。

手动运行适配入口：

```bash
cd apaas_scene
export AGORA_APP_ID='<测试项目 App ID>'
export AGORA_APP_CERTIFICATE='<测试项目 Certificate>'
locust -f locust_entry.py -u 2000 -r 10 -H https://目标APaaS地址 --autostart --logfile locust.log
```

运行日志在 `logs/apaas_test_*.log`。可搜索“大房间创建成功”获取房间 ID，搜索“房间已满员”查看满员状态。

完整依赖见 `requirements.lock`。Docker 构建和服务器部署说明见项目根目录 `README.md`。
