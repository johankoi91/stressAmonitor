#!/usr/bin/env python3
import io
import json
import os
import re
import shlex
import subprocess
import sys
import tarfile
import threading
import signal
from datetime import datetime
from http.server import BaseHTTPRequestHandler, HTTPServer
from socketserver import ThreadingMixIn
from urllib.parse import urlparse

BASE_DIR = os.environ.get("MEETING_WORKDIR", "/home/bbt")
PORT = int(os.environ.get("MEETING_CONTROL_PORT", "8088"))
DEFAULTS = {
    "total": 30,
    "token": "f29d4dc623c741a3ad38ccaf27a8d2e2",
    "local_ap": "172.31.11.174",
    "local_ap_cert": "ap.1452738.agora.local",
    "cname": "agora0001",
    "uid_start": 10001,
    "wait": 2,
    "user_per_proc": 6,
    "subscribe_high_count": 0,
    "subscribe_low_count": 0,
    "speaker_start": 0,
    "speaker_end": 0,
    "video_high": "high.h264",
    "video_low": "low.h264",
    "audio": "zxkgf.wav",
    "pub_audio": False,
}
STREAM_DEFAULTS = {
    "video_rate": 15,
    "token": "f29d4dc623c741a3ad38ccaf27a8d2e2",
    "cname": "agora0001",
    "render": 0,
    "uid": 999999,
    "audio_input": "zxkgf.wav",
    "video_input": "ceshi_1080p.h264",
    "ap": "172.31.11.174",
    "ap_cert": "ap.1452738.agora.local",
    "mode": 0,
    "hold": 999999999,
}
STREAM_FORM_DEFAULTS = {k: v for k, v in STREAM_DEFAULTS.items() if k not in {"video_rate", "render", "mode"}}
STREAM_HIDDEN_ARGS = [
    "--a-sub-flag=0",
    "--v-sub-flag=0",
    "--mute-audio=0",
    "--mute-video=0",
    "--use-aut=0",
]
CHANNEL_USERS_DEFAULTS = {
    "channel_count": 3,
    "user_per_process": 2,
    "token": "f29d4dc623c741a3ad38ccaf27a8d2e2",
    "local_ap": "172.31.11.174",
    "local_ap_cert": "ap.1452738.agora.local",
    "video_fps": 15,
    "cname_base": "stress_poc",
    "number_start": 1000,
    "uid_base": 10000,
    "video_high_file": "ceshi_1080p.h264",
    "video_low_file": "test_360p.h264",
    "ip": "",
    "port": "",
    "wait": 2,
}
CHANNEL_USERS_SCRIPT = "stress_jk" + "tu" + "chuan.py"
JOB_LOCK = threading.Lock()
JOB_PROCESS = None
JOB = {"running": False, "startedAt": None, "finishedAt": None, "lastCommand": "", "lastResult": None, "meta": None}
CONSOLE_LOCK = threading.Lock()
CONSOLE_LINES = []
CONSOLE_MAX_LINES = 500

def append_console(line):
    ts = datetime.now().strftime("%H:%M:%S")
    with CONSOLE_LOCK:
        CONSOLE_LINES.append(f"[{ts}] {line}")
        if len(CONSOLE_LINES) > CONSOLE_MAX_LINES:
            del CONSOLE_LINES[:-CONSOLE_MAX_LINES]

def clear_console():
    with CONSOLE_LOCK:
        CONSOLE_LINES.clear()

def console_snapshot():
    with CONSOLE_LOCK:
        return "\n".join(CONSOLE_LINES)

def count_sample_processes(cname=""):
    cmd = "ps -ef | grep sample_send_h264_pcm | grep -v grep"
    if cname:
        cmd += " | grep -a " + shlex.quote(cname)
    cmd += " | wc -l"
    res = run_shell(cmd, timeout=10)
    try:
        return int((res.get("stdout") or "0").strip() or "0")
    except ValueError:
        return 0

def monitor_sample_progress(meta, stop_event):
    target = int(meta.get("expectedProcessCount") or 0)
    cname = str(meta.get("cname") or "")
    last_count = -1
    tick = 0
    while not stop_event.is_set():
        tick += 1
        current = count_sample_processes(cname)
        if current != last_count or tick == 1 or tick % 5 == 0:
            if target > 0:
                append_console(f"压测启动进度：当前 {current}/{target} 个 sample_send_h264_pcm 进程")
            else:
                append_console(f"压测启动进度：当前 {current} 个 sample_send_h264_pcm 进程")
            last_count = current
        if target > 0 and current >= target:
            append_console(f"压测进程数已达到目标：{current}/{target}")
            break
        stop_event.wait(1)

HTML = r'''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Meeting 压测控制台</title><style>
body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Arial,"PingFang SC",sans-serif;background:#f5f7fb;color:#182033}.wrap{max-width:1180px;margin:0 auto;padding:28px}.hero{background:linear-gradient(135deg,#155eef,#7c3aed);color:#fff;border-radius:22px;padding:28px;box-shadow:0 18px 45px #155eef33}.hero h1{margin:0 0 8px;font-size:30px}.hero p{margin:0;opacity:.92}.card{background:#fff;border-radius:18px;padding:18px;margin-top:18px;box-shadow:0 10px 28px #11182712}.grid{display:grid;grid-template-columns:repeat(4,1fr);gap:14px}.field label{font-size:12px;color:#667085;display:block;margin-bottom:6px}.field input,.field select{width:100%;box-sizing:border-box;border:1px solid #d0d5dd;border-radius:11px;padding:10px 12px;font-size:14px}.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:18px}.btn{border:0;border-radius:12px;padding:11px 16px;font-weight:700;cursor:pointer}.primary{background:#155eef;color:white}.danger{background:#d92d20;color:white}.ghost{background:#eef4ff;color:#155eef}.muted{background:#f2f4f7;color:#344054}.status{white-space:pre; background:#0b1220;color:#d6e4ff;border-radius:14px;padding:14px;width:100%;height:420px;max-height:55vh;overflow-y:auto;overflow-x:auto;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;line-height:1.5;box-sizing:border-box}.pill{display:inline-block;background:#ecfdf3;color:#027a48;border-radius:999px;padding:5px 10px;font-size:12px;margin-left:8px}@media(max-width:900px){.grid{grid-template-columns:1fr}}
</style></head><body><div class="wrap"><div class="hero"><h1>Meeting 压测控制台 <span class="pill">container server</span></h1><p>HTTP server 运行在 test 容器内，网页、启动、停止、状态、日志全部在容器内控制。</p><p style="margin-top:12px"><a href="/stream" style="color:#fff;font-weight:700;margin-right:16px">进入发流页面</a><a href="/channel-users" style="color:#fff;font-weight:700">进入频道主发布者与订阅用户页面</a></p></div><div class="card"><h2>压测参数</h2><div class="grid" id="form"></div><div class="actions"><button class="btn primary" onclick="startTask()">启动压测</button><button class="btn danger" onclick="stopTask()">停止全部压测</button><button class="btn ghost" onclick="statusTask()">刷新状态</button><button class="btn muted" onclick="logsTask()">查看最近日志</button><button class="btn muted" onclick="downloadLogs()">打包下载日志</button></div></div><div class="card"><h2>执行输出</h2><div class="status" id="out">等待操作...</div></div></div><script>
const fields=[['total','总用户数','number'],['uid_start','起始 UID','number'],['user_per_proc','每进程用户数','number'],['wait','进程间隔秒','number'],['cname','频道名','text'],['token','Token/AppId','text'],['local_ap','Local AP','text'],['local_ap_cert','AP Cert/SNI','text'],['video_high','大流文件','text'],['video_low','小流文件','text'],['audio','音频文件','text'],['subscribe_high_count','订阅大流数','number'],['subscribe_low_count','订阅小流数','number'],['speaker_start','订阅池起始 UID','number'],['speaker_end','订阅池结束 UID','number'],['pub_audio','是否发音频','select']];
const defaults=__DEFAULTS__;
function render(){let h='';for(const [k,l,t] of fields){h+=`<div class="field"><label>${l} (${k})</label>`;if(t==='select'){h+=`<select id="${k}"><option value="false">false</option><option value="true">true</option></select>`}else{h+=`<input id="${k}" type="${t}" value="${defaults[k]??''}">`}h+='</div>'}document.getElementById('form').innerHTML=h;document.getElementById('pub_audio').value=String(defaults.pub_audio)}
function val(k){let e=document.getElementById(k);if(e.type==='number')return Number(e.value);if(k==='pub_audio')return e.value==='true';return e.value}function payload(){let p={};for(const [k] of fields)p[k]=val(k);return p}let consoleMode=false;function out(x){const el=document.getElementById('out');el.textContent=typeof x==='string'?x:JSON.stringify(x,null,2);el.scrollTop=el.scrollHeight}
async function api(path,body,quiet=false){if(!quiet)out('请求中...');let opt=body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{};let r=await fetch(path,opt);let t=await r.text();let parsed=t;try{parsed=JSON.parse(t)}catch(e){}if(parsed&&typeof parsed==='object'&&typeof parsed.logs==='string'){out(parsed.logs||'暂无控制台日志')}else if(!quiet){out(parsed)}return parsed}
async function consoleLogsTask(){let r=await fetch('/api/console-logs');let data=await r.json();out(data.logs||'暂无控制台日志')}async function startTask(){consoleMode=true;await api('/api/start',payload());setTimeout(consoleLogsTask,500)}function stopTask(){consoleMode=false;api('/api/stop',{})}function statusTask(){consoleMode=false;api('/api/status')}function statusPoll(){if(!consoleMode)api('/api/status',null,true)}function logsTask(){consoleMode=true;consoleLogsTask()}function downloadLogs(){out('正在打包日志...');window.location='/api/download-logs'}render();statusPoll();setInterval(statusPoll,10000);setInterval(()=>{if(consoleMode)consoleLogsTask()},1000);
</script></body></html>'''

STREAM_HTML = r'''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>发流控制台</title><style>
body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Arial,"PingFang SC",sans-serif;background:#f5f7fb;color:#182033}.wrap{max-width:1180px;margin:0 auto;padding:28px}.hero{background:linear-gradient(135deg,#0f766e,#155eef);color:#fff;border-radius:22px;padding:28px;box-shadow:0 18px 45px #155eef33}.hero h1{margin:0 0 8px;font-size:30px}.hero p{margin:0;opacity:.92}.hero a{color:#fff;font-weight:700}.card{background:#fff;border-radius:18px;padding:18px;margin-top:18px;box-shadow:0 10px 28px #11182712}.grid{display:grid;grid-template-columns:repeat(4,1fr);gap:14px}.field label{font-size:12px;color:#667085;display:block;margin-bottom:6px}.field input{width:100%;box-sizing:border-box;border:1px solid #d0d5dd;border-radius:11px;padding:10px 12px;font-size:14px}.hint{font-size:13px;color:#667085;line-height:1.7;background:#f8fafc;border:1px solid #eaecf0;border-radius:12px;padding:12px}.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:18px}.btn{border:0;border-radius:12px;padding:11px 16px;font-weight:700;cursor:pointer}.primary{background:#0f766e;color:white}.danger{background:#d92d20;color:white}.ghost{background:#eef4ff;color:#155eef}.muted{background:#f2f4f7;color:#344054}.status{white-space:pre; background:#0b1220;color:#d6e4ff;border-radius:14px;padding:14px;width:100%;height:420px;max-height:55vh;overflow-y:auto;overflow-x:auto;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;line-height:1.5;box-sizing:border-box}.pill{display:inline-block;background:#ecfdf3;color:#027a48;border-radius:999px;padding:5px 10px;font-size:12px;margin-left:8px}@media(max-width:900px){.grid{grid-template-columns:1fr}}
</style></head><body><div class="wrap"><div class="hero"><h1>发流控制台 <span class="pill">sample_send_h264_pcm</span></h1><p>通过网页配置发流参数并在容器内执行；部分固定参数由后端自动追加。</p><p style="margin-top:12px"><a href="/">返回 Meeting 压测控制台</a></p></div><div class="card"><h2>发流参数</h2><div class="grid" id="form"></div><div class="hint" style="margin-top:14px">后端固定追加参数：视频帧率=15、-R=0、模式=0、--a-sub-flag=0、--v-sub-flag=0、--mute-audio=0、--mute-video=0、--use-aut=0。页面不会提供这些输入框。</div><div class="actions"><button class="btn primary" onclick="startStream()">启动发流</button><button class="btn danger" onclick="stopTask()">停止全部压测/发流</button><button class="btn ghost" onclick="statusTask()">刷新状态</button><button class="btn muted" onclick="logsTask()">查看最近日志</button><button class="btn muted" onclick="downloadLogs()">打包下载日志</button></div></div><div class="card"><h2>执行输出</h2><div class="status" id="out">等待操作...</div></div></div><script>
const fields=[['token','Token/AppId','text'],['cname','频道名','text'],['uid','UID','number'],['audio_input','音频文件','text'],['video_input','视频文件','text'],['ap','Local AP','text'],['ap_cert','AP Cert/SNI','text'],['hold','保持时长','number']];
const defaults=__STREAM_DEFAULTS__;
function drawForm(){let h='';for(const [k,l,t] of fields){h+=`<div class="field"><label>${l} (${k})</label><input id="${k}" type="${t}" value="${defaults[k]??''}"></div>`}document.getElementById('form').innerHTML=h}
function val(k){let e=document.getElementById(k);return e.type==='number'?Number(e.value):e.value}function payload(){let p={};for(const [k] of fields)p[k]=val(k);return p}function out(x){document.getElementById('out').textContent=typeof x==='string'?x:JSON.stringify(x,null,2)}
async function api(path,body){out('请求中...');let opt=body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{};let r=await fetch(path,opt);let t=await r.text();try{out(JSON.parse(t))}catch(e){out(t)}}
function startStream(){api('/api/stream/start',payload())}function stopTask(){api('/api/stop',{})}function statusTask(){api('/api/status')}function logsTask(){api('/api/logs')}function downloadLogs(){out('正在打包发流日志...');window.location='/api/stream/download-logs'}drawForm();statusTask();setInterval(statusTask,10000);
</script></body></html>'''

CHANNEL_USERS_HTML = r'''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>频道主发布者与订阅用户控制台</title><style>
body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Arial,"PingFang SC",sans-serif;background:#f5f7fb;color:#182033}.wrap{max-width:1180px;margin:0 auto;padding:28px}.hero{background:linear-gradient(135deg,#7c2d12,#ea580c);color:#fff;border-radius:22px;padding:28px;box-shadow:0 18px 45px #ea580c33}.hero h1{margin:0 0 8px;font-size:30px}.hero p{margin:0;opacity:.92}.hero a{color:#fff;font-weight:700}.card{background:#fff;border-radius:18px;padding:18px;margin-top:18px;box-shadow:0 10px 28px #11182712}.grid{display:grid;grid-template-columns:repeat(4,1fr);gap:14px}.field label{font-size:12px;color:#667085;display:block;margin-bottom:6px}.field input{width:100%;box-sizing:border-box;border:1px solid #d0d5dd;border-radius:11px;padding:10px 12px;font-size:14px}.hint{font-size:13px;color:#667085;line-height:1.7;background:#fff7ed;border:1px solid #fed7aa;border-radius:12px;padding:12px}.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:18px}.btn{border:0;border-radius:12px;padding:11px 16px;font-weight:700;cursor:pointer}.primary{background:#ea580c;color:white}.danger{background:#d92d20;color:white}.ghost{background:#eef4ff;color:#155eef}.muted{background:#f2f4f7;color:#344054}.status{white-space:pre; background:#0b1220;color:#d6e4ff;border-radius:14px;padding:14px;width:100%;height:420px;max-height:55vh;overflow-y:auto;overflow-x:auto;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;line-height:1.5;box-sizing:border-box}.pill{display:inline-block;background:#fff7ed;color:#9a3412;border-radius:999px;padding:5px 10px;font-size:12px;margin-left:8px}@media(max-width:900px){.grid{grid-template-columns:1fr}}
</style></head><body><div class="wrap"><div class="hero"><h1>频道主发布者与订阅用户控制台 <span class="pill">频道任务</span></h1><p>按频道批量启动任务：每个频道 1 个主发布者，若干订阅用户。</p><p style="margin-top:12px"><a href="/">返回 Meeting 压测控制台</a></p></div><div class="card"><h2>频道与用户参数</h2><div class="grid" id="form"></div><div class="hint" style="margin-top:14px">默认值来自既有频道脚本。启动后会在容器内执行频道任务；停止按钮复用 stopAll.sh。</div><div class="actions"><button class="btn primary" onclick="startChannelUsers()">启动频道主发布者与订阅用户</button><button class="btn danger" onclick="stopTask()">停止全部压测/发流</button><button class="btn ghost" onclick="statusTask()">刷新状态</button><button class="btn muted" onclick="logsTask()">查看最近日志</button><button class="btn muted" onclick="downloadLogs()">打包下载频道任务日志</button></div></div><div class="card"><h2>执行输出</h2><div class="status" id="out">等待操作...</div></div></div><script>
const fields=[['channel_count','频道数量','number'],['user_per_process','每进程用户数','number'],['token','Token/AppId','text'],['local_ap','Local AP','text'],['local_ap_cert','AP Cert/SNI','text'],['video_fps','视频帧率','number'],['cname_base','频道名前缀','text'],['number_start','频道编号起始','number'],['uid_base','UID 基数','number'],['video_high_file','大流视频文件','text'],['video_low_file','低流视频文件','text'],['ip','指定 IP，可空','text'],['port','指定端口，可空','text'],['wait','等待秒数','number']];
const defaults=__CHANNEL_USERS_DEFAULTS__;
function drawForm(){let h='';for(const [k,l,t] of fields){h+=`<div class="field"><label>${l} (${k})</label><input id="${k}" type="${t}" value="${defaults[k]??''}"></div>`}document.getElementById('form').innerHTML=h}
function val(k){let e=document.getElementById(k);return e.type==='number'?Number(e.value):e.value}function payload(){let p={};for(const [k] of fields)p[k]=val(k);return p}let consoleMode=false;function out(x){const el=document.getElementById('out');el.textContent=typeof x==='string'?x:JSON.stringify(x,null,2);el.scrollTop=el.scrollHeight}
async function api(path,body,quiet=false){if(!quiet)out('请求中...');let opt=body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{};let r=await fetch(path,opt);let t=await r.text();let parsed=t;try{parsed=JSON.parse(t)}catch(e){}if(parsed&&typeof parsed==='object'&&typeof parsed.logs==='string'){out(parsed.logs||'暂无控制台日志')}else if(!quiet){out(parsed)}return parsed}
async function consoleLogsTask(){let r=await fetch('/api/console-logs');let data=await r.json();out(data.logs||'暂无控制台日志')}async function startChannelUsers(){consoleMode=true;await api('/api/channel-users/start',payload());setTimeout(consoleLogsTask,500)}function stopTask(){consoleMode=false;api('/api/stop',{})}function statusTask(){consoleMode=false;api('/api/status')}function statusPoll(){if(!consoleMode)api('/api/status',null,true)}function logsTask(){consoleMode=true;consoleLogsTask()}function downloadLogs(){out('正在打包频道任务日志...');window.location='/api/channel-users/download-logs'}drawForm();statusPoll();setInterval(statusPoll,10000);setInterval(()=>{if(consoleMode)consoleLogsTask()},1000);
</script></body></html>'''

def run_shell(cmd, timeout=60):
    p = subprocess.run(cmd, shell=True, cwd=BASE_DIR, universal_newlines=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    return {"exit_code": p.returncode, "stdout": p.stdout, "stderr": p.stderr, "command": cmd}

def run_job_command(cmd, timeout=3600):
    global JOB_PROCESS
    p = subprocess.Popen(cmd, shell=True, cwd=BASE_DIR, universal_newlines=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, preexec_fn=os.setsid)
    with JOB_LOCK:
        JOB_PROCESS = p
    try:
        out, err = p.communicate(timeout=timeout)
        return {"exit_code": p.returncode, "stdout": out, "stderr": err, "command": cmd}
    except subprocess.TimeoutExpired:
        try:
            os.killpg(os.getpgid(p.pid), signal.SIGTERM)
        except Exception:
            pass
        out, err = p.communicate()
        return {"exit_code": -1, "stdout": out, "stderr": (err or "") + "\njob timeout", "command": cmd}
    finally:
        with JOB_LOCK:
            if JOB_PROCESS is p:
                JOB_PROCESS = None

def terminate_current_job():
    global JOB_PROCESS
    with JOB_LOCK:
        p = JOB_PROCESS
    if p is not None and p.poll() is None:
        try:
            os.killpg(os.getpgid(p.pid), signal.SIGTERM)
            return {"terminated": True, "pid": p.pid}
        except Exception as e:
            return {"terminated": False, "pid": p.pid, "error": repr(e)}
    return {"terminated": False, "reason": "no running start process"}

def stop_all():
    term = terminate_current_job()
    res = run_shell("./stopAll.sh; true", timeout=60)
    with JOB_LOCK:
        JOB.update({"running": False, "finishedAt": datetime.now().isoformat(timespec="seconds"), "lastResult": {"terminate": term, "stopAll": res}})
    return {"terminate": term, "stopAll": res}

def to_int(data, key):
    return int(data.get(key, DEFAULTS[key]))

def to_str(data, key):
    return str(data.get(key, DEFAULTS[key])).strip()

def build_start_cmd(data):
    total = to_int(data, "total")
    uid_start = to_int(data, "uid_start")
    user_per_proc = to_int(data, "user_per_proc")
    parts = ["python3", "stress_huge.py", "--total", str(total), "--token", to_str(data,"token"), "--local_ap", to_str(data,"local_ap"), "--local_ap_cert", to_str(data,"local_ap_cert"), "--cname", to_str(data,"cname"), "--uid_start", str(uid_start), "--wait", str(to_int(data,"wait")), "--method", "start", "--user_per_proc", str(user_per_proc), "--subscribe_high_count", str(to_int(data,"subscribe_high_count")), "--subscribe_low_count", str(to_int(data,"subscribe_low_count")), "--speaker_start", str(to_int(data,"speaker_start")), "--speaker_end", str(to_int(data,"speaker_end")), "--audio", to_str(data,"audio"), "--video_high", to_str(data,"video_high"), "--video_low", to_str(data,"video_low")]
    if data.get("pub_audio"):
        parts += ["--pub_audio", "True"]
    meta = {"type": "meeting", "cname": to_str(data, "cname"), "totalUsers": total, "uidRange": {"start": uid_start, "end": uid_start + total - 1}, "userPerProcess": user_per_proc, "expectedProcessCount": (total + user_per_proc - 1) // user_per_proc}
    return " ".join(shlex.quote(x) for x in parts), meta

def build_stream_cmd(data):
    merged = {**STREAM_DEFAULTS, **data}
    parts = [
        "LD_LIBRARY_PATH=./", "nohup", "./sample_send_h264_pcm",
        "--video-rate=%s" % int(merged["video_rate"]),
        "--token=%s" % str(merged["token"]).strip(),
        "-c", str(merged["cname"]).strip(),
        "-R", str(int(merged["render"])),
        "--uid=%s" % int(merged["uid"]),
        "--audio-input=%s" % str(merged["audio_input"]).strip(),
        "--video-input=%s" % str(merged["video_input"]).strip(),
        "--ap=%s" % str(merged["ap"]).strip(),
        "--ap_cert=%s" % str(merged["ap_cert"]).strip(),
        *STREAM_HIDDEN_ARGS,
        "--mode=%s" % int(merged["mode"]),
        "--hold=%s" % int(merged["hold"]),
    ]
    log_name = "stream_%s.log" % int(merged["uid"])
    cmd = " ".join(x if x in {"LD_LIBRARY_PATH=./", "nohup"} else shlex.quote(x) for x in parts) + " > %s 2>&1 & echo $!" % shlex.quote(log_name)
    meta = {"type": "stream", "uid": int(merged["uid"]), "cname": str(merged["cname"]).strip(), "videoInput": str(merged["video_input"]).strip(), "audioInput": str(merged["audio_input"]).strip(), "log": log_name, "hiddenArgs": STREAM_HIDDEN_ARGS}
    return cmd, meta

def start_stream(data):
    cmd, meta = build_stream_cmd(data)
    res = run_shell(cmd, timeout=20)
    with JOB_LOCK:
        JOB.update({"running": res["exit_code"] == 0, "startedAt": datetime.now().isoformat(timespec="seconds"), "finishedAt": None, "lastCommand": cmd, "lastResult": res, "meta": meta})
    return {"accepted": res["exit_code"] == 0, "command": cmd, "meta": meta, "result": res}

def channel_users_to_int(data, key):
    return int(data.get(key, CHANNEL_USERS_DEFAULTS[key]))

def channel_users_to_str(data, key):
    return str(data.get(key, CHANNEL_USERS_DEFAULTS[key])).strip()

def build_channel_users_cmd(data):
    merged = {**CHANNEL_USERS_DEFAULTS, **data}
    parts = [
        "python3", CHANNEL_USERS_SCRIPT,
        "--channel_count", str(channel_users_to_int(merged, "channel_count")),
        "--user_per_process", str(channel_users_to_int(merged, "user_per_process")),
        "--token", channel_users_to_str(merged, "token"),
        "--local_ap", channel_users_to_str(merged, "local_ap"),
        "--local_ap_cert", channel_users_to_str(merged, "local_ap_cert"),
        "--video_fps", str(channel_users_to_int(merged, "video_fps")),
        "--cname_base", channel_users_to_str(merged, "cname_base"),
        "--number_start", str(channel_users_to_int(merged, "number_start")),
        "--uid_base", str(channel_users_to_int(merged, "uid_base")),
        "--video_high_file", channel_users_to_str(merged, "video_high_file"),
        "--video_low_file", channel_users_to_str(merged, "video_low_file"),
        "--wait", str(channel_users_to_int(merged, "wait")),
        "--method", "start",
    ]
    ip = channel_users_to_str(merged, "ip")
    port = channel_users_to_str(merged, "port")
    if ip:
        parts += ["--ip", ip]
    if port:
        parts += ["--port", port]
    channel_count = channel_users_to_int(merged, "channel_count")
    user_per_process = channel_users_to_int(merged, "user_per_process")
    number_start = channel_users_to_int(merged, "number_start")
    cmd = " ".join(shlex.quote(x) for x in parts)
    meta = {
        "type": "channel-users",
        "cname": channel_users_to_str(merged, "cname_base"),
        "channelCount": channel_count,
        "userPerProcess": user_per_process,
        "expectedProcessCount": channel_count if user_per_process <= 1 else channel_count + ((channel_count * (user_per_process - 1) + user_per_process - 1) // user_per_process),
        "channelRange": {"start": number_start, "end": number_start + channel_count - 1},
        "cnameBase": channel_users_to_str(merged, "cname_base"),
    }
    return cmd, meta

def start_channel_users(data):
    cmd, meta = build_channel_users_cmd(data)
    threading.Thread(target=start_worker, args=(cmd, meta), daemon=True).start()
    return {"accepted": True, "meta": meta}

def start_worker(cmd, meta):
    stop_event = threading.Event()
    task_type = meta.get("type")
    if task_type in {"meeting", "channel-users"}:
        clear_console()
        if task_type == "meeting":
            append_console("开始启动 Meeting 压测")
            append_console(f"目标用户数：{meta.get('totalUsers')}，每进程用户数：{meta.get('userPerProcess')}，预计进程数：{meta.get('expectedProcessCount')}")
            append_console(f"频道：{meta.get('cname')}，UID 范围：{meta.get('uidRange', {}).get('start')} - {meta.get('uidRange', {}).get('end')}")
        else:
            append_console("开始启动频道主发布者与订阅用户任务")
            append_console(f"频道数量：{meta.get('channelCount')}，每进程用户数：{meta.get('userPerProcess')}，预计进程数：{meta.get('expectedProcessCount')}")
            append_console(f"频道名前缀：{meta.get('cnameBase')}，频道编号范围：{meta.get('channelRange', {}).get('start')} - {meta.get('channelRange', {}).get('end')}")
        threading.Thread(target=monitor_sample_progress, args=(meta, stop_event), daemon=True).start()
    with JOB_LOCK:
        JOB.update({"running": True, "startedAt": datetime.now().isoformat(timespec="seconds"), "finishedAt": None, "lastCommand": cmd, "lastResult": None, "meta": meta})
    try:
        res = run_job_command(cmd, timeout=3600)
    except Exception as e:
        res = {"exit_code": -1, "stdout": "", "stderr": repr(e), "command": cmd}
    finally:
        stop_event.set()
    if task_type in {"meeting", "channel-users"}:
        final_count = count_sample_processes(str(meta.get("cname") or ""))
        append_console(f"启动命令执行结束，当前 sample_send_h264_pcm 进程数：{final_count}")
        stdout = (res.get("stdout") or "").strip()
        stderr = (res.get("stderr") or "").strip()
        if stdout:
            append_console("启动输出：")
            for line in stdout.splitlines()[-80:]:
                append_console(line)
        if stderr:
            append_console("启动错误输出：")
            for line in stderr.splitlines()[-80:]:
                append_console(line)
        append_console(f"启动命令退出码：{res.get('exit_code')}")
    with JOB_LOCK:
        JOB.update({"running": False, "finishedAt": datetime.now().isoformat(timespec="seconds"), "lastResult": res})

def status_data():
    with JOB_LOCK:
        job_snapshot = dict(JOB)
    if (job_snapshot.get("meta") or {}).get("type") == "channel-users":
        job_snapshot["lastCommand"] = "频道主发布者与订阅用户任务"
    cmd = "echo '=== job ==='; echo '%s'; echo '=== processes ==='; ps -ef | grep sample_send_h264_pcm | grep -v grep || true; echo '=== process count ==='; ps -ef | grep sample_send_h264_pcm | grep -v grep | wc -l; echo '=== stress logs ==='; ls -lh [0-9]*.log stream_*.log case3_fa_*.log 2>/dev/null | tail -40 || true" % shlex.quote(json.dumps(job_snapshot, ensure_ascii=False))
    return run_shell(cmd, timeout=20)

def logs_data():
    return run_shell('latest=$(ls -t [0-9]*.log stream_*.log case3_fa_*.log 2>/dev/null | head -1); echo latest=$latest; [ -n "$latest" ] && tail -160 "$latest" || true', timeout=20)

def console_logs_data():
    with JOB_LOCK:
        running = bool(JOB.get("running"))
        meta = JOB.get("meta")
    return {"running": running, "meta": meta, "logs": console_snapshot()}

def safe_add(tar, path, arcname, added):
    full = os.path.abspath(os.path.join(BASE_DIR, path))
    base = os.path.abspath(BASE_DIR)
    if not full.startswith(base + os.sep) and full != base:
        return False
    if not os.path.isfile(full) or full in added:
        return False
    tar.add(full, arcname=arcname)
    added.add(full)
    return True

def parse_sdk_logs(numbered_logs):
    sdk_logs = set()
    pattern = re.compile(r"Log file name:\s*(\S+)")
    for log_name in numbered_logs:
        try:
            with open(os.path.join(BASE_DIR, log_name), "r", errors="ignore") as f:
                for line in f:
                    m = pattern.search(line)
                    if m:
                        sdk_logs.add(m.group(1).lstrip("./"))
        except OSError:
            pass
    return sorted(sdk_logs)

def package_logs():
    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    archive_name = f"meeting-stress-logs-{ts}.tar.gz"
    archive_path = os.path.join(BASE_DIR, archive_name)
    numbered_logs = sorted([name for name in os.listdir(BASE_DIR) if re.fullmatch(r"\d+\.log", name)], key=lambda x: int(x.split(".")[0]))
    sdk_logs = parse_sdk_logs(numbered_logs)
    optional_logs = ["nohup.out", "meeting-container-server.log"]
    sdk_dir = os.path.join(BASE_DIR, "io.agora.rtc_sdk")
    if os.path.isdir(sdk_dir) and os.path.isfile(os.path.join(sdk_dir, "agoraapi.log")):
        sdk_logs.append("io.agora.rtc_sdk/agoraapi.log")
    with JOB_LOCK:
        job_snapshot = dict(JOB)
    manifest = {
        "createdAt": datetime.now().isoformat(timespec="seconds"),
        "baseDir": BASE_DIR,
        "job": job_snapshot,
        "numberedLogs": numbered_logs,
        "sdkLogs": sorted(set(sdk_logs)),
        "note": "Each numbered log may contain 'Log file name: ./io.agora.rtc_sdk/agorasdk_xxx.log', which maps it to the SDK log file included here.",
    }
    added = set()
    with tarfile.open(archive_path, "w:gz") as tar:
        info = tarfile.TarInfo("manifest.json")
        raw = json.dumps(manifest, ensure_ascii=False, indent=2).encode()
        info.size = len(raw)
        info.mtime = int(datetime.now().timestamp())
        tar.addfile(info, fileobj=io.BytesIO(raw))
        for log_name in numbered_logs:
            safe_add(tar, log_name, f"numbered/{log_name}", added)
        for log_name in sorted(set(sdk_logs)):
            safe_add(tar, log_name, f"sdk/{os.path.basename(log_name)}", added)
        for log_name in optional_logs:
            safe_add(tar, log_name, f"server/{log_name}", added)
    return archive_path, archive_name, manifest

def package_stream_logs():
    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    archive_name = f"stream-logs-{ts}.tar.gz"
    archive_path = os.path.join(BASE_DIR, archive_name)
    stream_logs = sorted([name for name in os.listdir(BASE_DIR) if re.fullmatch(r"stream_\d+\.log", name)])
    sdk_logs = parse_sdk_logs(stream_logs)
    with JOB_LOCK:
        job_snapshot = dict(JOB)
    manifest = {
        "createdAt": datetime.now().isoformat(timespec="seconds"),
        "baseDir": BASE_DIR,
        "job": job_snapshot,
        "streamLogs": stream_logs,
        "sdkLogs": sorted(set(sdk_logs)),
        "note": "This archive only includes logs generated by the stream page: stream_*.log and SDK logs referenced from those stream logs.",
    }
    added = set()
    with tarfile.open(archive_path, "w:gz") as tar:
        info = tarfile.TarInfo("manifest.json")
        raw = json.dumps(manifest, ensure_ascii=False, indent=2).encode()
        info.size = len(raw)
        info.mtime = int(datetime.now().timestamp())
        tar.addfile(info, fileobj=io.BytesIO(raw))
        for log_name in stream_logs:
            safe_add(tar, log_name, f"stream/{log_name}", added)
        for log_name in sorted(set(sdk_logs)):
            safe_add(tar, log_name, f"sdk/{os.path.basename(log_name)}", added)
    return archive_path, archive_name, manifest

def package_channel_users_logs():
    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    archive_name = f"channel-users-logs-{ts}.tar.gz"
    archive_path = os.path.join(BASE_DIR, archive_name)
    with JOB_LOCK:
        job_snapshot = dict(JOB)
    meta = job_snapshot.get("meta") or {}
    expected_count = int(meta.get("expectedProcessCount") or 0)
    if meta.get("type") == "channel-users" and expected_count > 0:
        channel_logs = [f"{i}.log" for i in range(1, expected_count + 1) if os.path.isfile(os.path.join(BASE_DIR, f"{i}.log"))]
    else:
        channel_logs = sorted([name for name in os.listdir(BASE_DIR) if re.fullmatch(r"\d+\.log", name)], key=lambda x: int(x.split(".")[0]))
    sdk_logs = parse_sdk_logs(channel_logs)
    manifest = {
        "createdAt": datetime.now().isoformat(timespec="seconds"),
        "baseDir": BASE_DIR,
        "job": job_snapshot,
        "channelUserLogs": channel_logs,
        "sdkLogs": sorted(set(sdk_logs)),
        "note": "This archive includes numeric logs generated by the latest channel main-publisher/subscriber task and SDK logs referenced from those logs.",
    }
    added = set()
    with tarfile.open(archive_path, "w:gz") as tar:
        info = tarfile.TarInfo("manifest.json")
        raw = json.dumps(manifest, ensure_ascii=False, indent=2).encode()
        info.size = len(raw)
        info.mtime = int(datetime.now().timestamp())
        tar.addfile(info, fileobj=io.BytesIO(raw))
        for log_name in channel_logs:
            safe_add(tar, log_name, f"channel-users/{log_name}", added)
        for log_name in sorted(set(sdk_logs)):
            safe_add(tar, log_name, f"sdk/{os.path.basename(log_name)}", added)
    return archive_path, archive_name, manifest

class ThreadingHTTPServer(ThreadingMixIn, HTTPServer):
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    def send_json(self, data, code=200):
        raw = json.dumps(data, ensure_ascii=False, indent=2).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json; charset=utf-8"); self.send_header("Content-Length", str(len(raw))); self.end_headers(); self.wfile.write(raw)
    def read_json(self):
        n = int(self.headers.get("Content-Length", "0") or 0)
        return json.loads(self.rfile.read(n).decode()) if n else {}
    def send_archive(self, package_func):
        try:
            archive_path, archive_name, _ = package_func()
            with open(archive_path, "rb") as f:
                raw = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "application/gzip")
            self.send_header("Content-Disposition", f"attachment; filename={archive_name}")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
        except Exception as e:
            self.send_json({"error": repr(e)}, 500)
    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/":
            raw = HTML.replace("__DEFAULTS__", json.dumps(DEFAULTS, ensure_ascii=False)).encode()
            self.send_response(200); self.send_header("Content-Type", "text/html; charset=utf-8"); self.send_header("Content-Length", str(len(raw))); self.end_headers(); self.wfile.write(raw)
        elif path in ("/stream", "/case3-fa"):
            raw = STREAM_HTML.replace("__STREAM_DEFAULTS__", json.dumps(STREAM_FORM_DEFAULTS, ensure_ascii=False)).encode()
            self.send_response(200); self.send_header("Content-Type", "text/html; charset=utf-8"); self.send_header("Content-Length", str(len(raw))); self.end_headers(); self.wfile.write(raw)
        elif path == "/channel-users":
            raw = CHANNEL_USERS_HTML.replace("__CHANNEL_USERS_DEFAULTS__", json.dumps(CHANNEL_USERS_DEFAULTS, ensure_ascii=False)).encode()
            self.send_response(200); self.send_header("Content-Type", "text/html; charset=utf-8"); self.send_header("Content-Length", str(len(raw))); self.end_headers(); self.wfile.write(raw)
        elif path == "/api/status": self.send_json(status_data())
        elif path == "/api/logs": self.send_json(logs_data())
        elif path == "/api/console-logs": self.send_json(console_logs_data())
        elif path == "/api/download-logs":
            self.send_archive(package_logs)
        elif path == "/api/stream/download-logs":
            self.send_archive(package_stream_logs)
        elif path == "/api/channel-users/download-logs":
            self.send_archive(package_channel_users_logs)
        else: self.send_json({"error":"not found"}, 404)
    def do_POST(self):
        path = urlparse(self.path).path
        body = self.read_json()
        if path == "/api/start":
            data = {**DEFAULTS, **body}
            cmd, meta = build_start_cmd(data)
            threading.Thread(target=start_worker, args=(cmd, meta), daemon=True).start()
            self.send_json({"accepted": True, "command": cmd, "meta": meta})
        elif path in ("/api/stream/start", "/api/case3-fa/start"):
            data = {**STREAM_DEFAULTS, **body}
            self.send_json(start_stream(data))
        elif path == "/api/channel-users/start":
            data = {**CHANNEL_USERS_DEFAULTS, **body}
            self.send_json(start_channel_users(data))
        elif path == "/api/stop":
            self.send_json(stop_all())
        else: self.send_json({"error":"not found"}, 404)
    def log_message(self, fmt, *args):
        sys.stderr.write("[%s] %s\n" % (datetime.now().strftime("%F %T"), fmt % args))

if __name__ == "__main__":
    os.chdir(BASE_DIR)
    print("Meeting container control server: http://0.0.0.0:%d" % PORT, flush=True)
    print("workdir: %s" % BASE_DIR, flush=True)
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
