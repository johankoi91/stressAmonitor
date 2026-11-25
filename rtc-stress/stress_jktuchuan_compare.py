import argparse
import time
import sys
import subprocess
import time

import codecs

sys.stdout = codecs.getwriter("utf-8")(sys.stdout.detach())

def RunShellAndGetOutput(shell):
    cmd = shell
    process = subprocess.Popen(cmd,
                               shell=True,
                               stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE)
    try:
        outs, errs = process.communicate(timeout=30)
    except subprocess.TimeoutExpired:
        process.kill()
        try:
            outs, errs = process.communicate(timeout=30)
        except subprocess.TimeoutExpired:
            outs, errs = 0, 0
    outs = str(outs)[2:-1]  # 去除b'xxxxxxxx'前后
    errs = str(errs)[2:-1]
    # logger.info("Get shell output end.")
    return outs, errs
    return 0,0


def Print(ele):
    print(ele, flush=True)

# ————Global Info ———— #
uid_info = "--uid={}"
uid = 1
task_number = 0



# ————Global Info End———— #


class StressConfig:
    def __init__(self):
        Print("Init stress config")
        # 用户配置
        self.user_per_proc_: int

        # 流状态配置
        self.video_fps_: int

        # 频道配置
        self.cname_: str
        self.token_: str
        self.local_ap_: str
        self.local_ap_cert_: str
        self.ip_: str
        self.port_: str

        # 脚本控制
        self.case_wait: int

    def ToDebugPrint(self):
        Print("GlobalConfig:\n{}".format(self.__dict__))


g_config = StressConfig()

class UserBase:
    def __init__(self,
                 cname,
                 uid,
                 audio_file,
                 video_file,
                 pub_audio,
                 pub_video):
        self._cname = cname
        self._role = 0
        self._uid = uid
        # 走分配
        self._use_aut = 0
        self._is_sub_audio = 1
        self._is_sub_video = 1
        self._audio_file = audio_file
        self._video_file = video_file
        self._pub_audio = pub_audio
        self._pub_video = pub_video

    def __eq__(self, other):
        return self._cname == other._cname and self._use_aut == other._use_aut

    def __lt__(self, other):
        return self._use_aut > other._use_aut or (
            self._use_aut == other._use_aut and self._cname < other._cname)

    def __gt__(self, other):
        return self._use_aut < other._use_aut or (
            self._use_aut == other._use_aut and self._cname > other._cname)

class CommandBuilder:

    def __init__(self):
        self._process_number = 1
        self._count = 0
        self._command_line = None

        # 命令行Base string
        self._start_line = "export LD_LIBRARY_PATH=./ && nohup ./sample_send_h264_pcm --video-rate={video_fps} "
        self._base_line_with_vos = "--token={token} -c {cname} -R {role} --uid={uid} \
--audio-input={audio_file} --video-input={video_file} \
--ap={local_ap} --ap_cert={local_ap_cert} \
{ipinfo} \
--a-sub-flag={sub_audio} --v-sub-flag={sub_video} \
--mute-audio={mute_audio} --mute-video={mute_video} "

        self._end_line = "--mode={mode} --hold={hold} 1>{number}.log 2>&1 &"
        self._user_list = []

    def BuildAndRunCommand(self):
        global g_config
        self._user_list.sort()
        ipinfo = ""
        if g_config.ip_ != "" and g_config.port_ != "":
            ipinfo = "--ip={ip} --port={port}".format(ip=g_config.ip_, port=g_config.port_)
        for user in self._user_list:
            if self._command_line == None:
                self._command_line = self._start_line.format(
                    video_fps=g_config.video_fps_)
            else:
                self._command_line += "-O "
            self._command_line += self._base_line_with_vos.format(
                token=g_config.token_,
                cname=user._cname,
                role=user._role,
                uid=user._uid,
                audio_file=user._audio_file,
                video_file=user._video_file,
                local_ap=g_config.local_ap_,
                local_ap_cert=g_config.local_ap_cert_,
                ipinfo=ipinfo,
                sub_audio=user._is_sub_audio,
                sub_video=user._is_sub_video,
                mute_audio=0 if user._pub_audio else 1,
                mute_video=0 if user._pub_video else 1)
            if user._use_aut != 0:
                self._command_line += "--use-aut=1 "

        self._command_line += self._end_line.format(
            mode=0, hold=999999999, number=self._process_number)
        Print("run command : {}\nwith process number : {}".format(
            self._command_line, self._process_number))

        _, _ = RunShellAndGetOutput(self._command_line)
        # clear these vars
        self._process_number += 1
        self._count = 0
        self._command_line = None
        self._user_list = []
        time.sleep(0.5)

    def AddUser(self, cname, uid, audio_file, video_file, pub_audio, pub_video):
        self._user_list.append(UserBase(cname, uid, audio_file, video_file, pub_audio, pub_video))

        self._count += 1
        if self._count == g_config.user_per_proc_:
            self.BuildAndRunCommand()

    def CheckFinalCommand(self):
        # 最终可能有剩余command line没有执行 需要check
        if len(self._user_list) != 0:
            self.BuildAndRunCommand()

def Run(channel_count, user_per_process, token, local_ap, local_ap_cert, cname_base, number_start, uid_base, video_fps, wait, video_high_file, video_low_file, ip, port):
    # 设置一些基本参数
    g_config.user_per_proc_ = user_per_process
    g_config.video_fps_ = video_fps
    g_config.case_wait = wait

    command_builder = CommandBuilder()

    g_config.token_ = token
    g_config.local_ap_ = local_ap
    g_config.local_ap_cert_ = local_ap_cert
    g_config.ip_ = ip
    g_config.port_ = port
    for i in range(channel_count):
        sub_number = i + number_start
        now_channel = "{}_{}".format(cname_base, sub_number)
        now_uid = uid_base * sub_number
        command_builder.AddUser(now_channel, 999999, "zxkgf.wav", video_high_file, True, True)
    command_builder.CheckFinalCommand()
    time.sleep(0.5)
    for i in range(channel_count):
        sub_number = i + number_start
        now_channel = "{}_{}".format(cname_base, sub_number)
        now_uid = uid_base * sub_number
        now_uid+=1
        for _ in range(user_per_process - 1):
            command_builder.AddUser(now_channel, now_uid, "zxkgf.wav", video_low_file, False, False)
            now_uid+=1
    command_builder.CheckFinalCommand()



# Comparison adapter: keep the original CLI template and regular-file logging,
# but pack the requested three-person channels and own every process we start.
import os
import re
import json
import math
import struct
import signal
import datetime
import urllib.request
from pathlib import Path

class ComparisonBuilder(CommandBuilder):
    def __init__(self, options, output):
        super().__init__()
        self.options = options
        self.output = output
        self.processes = []
        self._start_line = "export LD_LIBRARY_PATH=./ && nohup setsid ./sample_send_h264_pcm --video-rate={video_fps} "

    def AddUser(self, cname, uid, audio_file, video_file, pub_audio, pub_video):
        user = UserBase(cname, uid, audio_file, video_file, pub_audio, pub_video)
        user._is_sub_video = 0
        self._user_list.append(user)
        self._count += 1
        if self._count == g_config.user_per_proc_:
            self.BuildAndRunCommand()

    def BuildAndRunCommand(self):
        self._user_list.sort()
        command = self._start_line.format(video_fps=30)
        if self.options.audio_only_service:command=command.replace('nohup setsid','LD_PRELOAD=/home/bbt/libaudio_only_service.so nohup setsid')
        if self.options.share_service:command=command.replace('nohup setsid','LD_PRELOAD=/home/bbt/libshared_service.so nohup setsid')
        if self.options.heap_profile:command=command.replace('nohup setsid','LD_PRELOAD=/home/bbt/libheap_probe.so HEAP_PROBE_DIR='+str(self.output)+' nohup setsid')
        for index, user in enumerate(self._user_list):
            if index: command += "-O "
            command += self._base_line_with_vos.format(token=g_config.token_,cname=user._cname,
                role=user._role,uid=user._uid,audio_file=user._audio_file,video_file=user._video_file,
                local_ap=g_config.local_ap_,local_ap_cert=g_config.local_ap_cert_,ipinfo="",
                sub_audio=1,sub_video=int(self.options.video_subscribe),mute_audio=0 if user._pub_audio else 1,mute_video=1)
        log = self.output / ("native-%03d.log" % self._process_number)
        command += "--mode=0 --hold=999999999 1>%s 2>&1 & echo $!" % log
        outs, errs = RunShellAndGetOutput(command)
        match = re.search(r"[0-9]+",outs)
        if not match: raise RuntimeError("Native launch did not return PID: "+errs)
        pid = int(match.group())
        for _ in range(20):
            if os.getpgid(pid) == pid: break
            time.sleep(0.05)
        if os.getpgid(pid) != pid: raise RuntimeError("Native did not create an isolated process group")
        item = {"pid":pid,"process":self._process_number,"log":str(log),"uids":len(self._user_list),
            "command":command.replace(g_config.token_,"[redacted]")}
        self.processes.append(item)
        (self.output/'processes.json').write_text(json.dumps(self.processes,ensure_ascii=False,indent=2))
        Print("Started process %d PID %d UIDs %d" % (self._process_number,pid,len(self._user_list)))
        self._process_number += 1
        self._count = 0; self._command_line = None; self._user_list = []
        time.sleep(0.5)

    def stop_owned(self):
        for item in self.processes:
            try: os.killpg(item['pid'],signal.SIGTERM)
            except ProcessLookupError: pass
        time.sleep(3)
        for item in self.processes:
            try: os.killpg(item['pid'],signal.SIGKILL)
            except ProcessLookupError: pass


def comparison_main():
    parser = argparse.ArgumentParser(description="Original Python launcher comparison; no Go/FIFO control path")
    parser.add_argument('--settings',default='/var/lib/rtc-stress-console/settings.json')
    parser.add_argument('--channels',type=int,default=200)
    parser.add_argument('--user-per-process',type=int,default=60)
    parser.add_argument('--duration',type=int,default=120)
    parser.add_argument('--fixed-publisher-uids',action='store_true')
    parser.add_argument('--legacy-grouped',action='store_true')
    parser.add_argument('--heap-profile',action='store_true')
    parser.add_argument('--share-service',action='store_true')
    parser.add_argument('--audio-only-service',action='store_true')
    parser.add_argument('--video-subscribe',action='store_true')
    parser.add_argument('--prefix',default='pycmp_'+datetime.datetime.now().strftime('%m%d_%H%M%S'))
    options = parser.parse_args()
    if options.channels<1 or options.user_per_process<3 or options.user_per_process%3 or not re.fullmatch(r'[A-Za-z0-9_]{1,40}',options.prefix):
        raise ValueError('Invalid channel/process parameters')
    os.umask(0o077)
    settings=json.loads(Path(options.settings).read_text(encoding='utf-8'))['config']
    g_config.token_=settings['token'];g_config.local_ap_=settings['ap'];g_config.local_ap_cert_=settings['apCert']
    g_config.ip_='';g_config.port_='';g_config.video_fps_=30;g_config.user_per_proc_=options.user_per_process
    output=Path('/home/bbt/console-runs')/options.prefix
    output.mkdir(mode=0o700,parents=True,exist_ok=False)
    for source,hz in enumerate([440,660]):
        data=bytearray()
        for frame in range(48000*5):
            sample=int(math.sin(2*math.pi*hz*frame/48000)*6000)
            data.extend(struct.pack('<hh',sample,sample))
        (output/('source-%d.pcm'%source)).write_bytes(data)
    builder=ComparisonBuilder(options,output)
    joined=set();offsets={};started=time.monotonic();ready_at=None;observations=[];last_sample=started
    def active_console_task():
        with urllib.request.urlopen('http://127.0.0.1:8092/api/tasks',timeout=5) as response:
            return any(t['state'] not in ['completed','failed','stopped','interrupted'] for t in json.load(response))
    if active_console_task(): raise RuntimeError('Console task already running')
    try:
        for channel in range(options.channels):
            name='%s_%d'%(options.prefix,1000+channel)
            builder.AddUser(name,10001 if options.fixed_publisher_uids else 10001+channel,str(output/'source-0.pcm'),'',True,False)
            builder.AddUser(name,20001 if options.fixed_publisher_uids else 20001+channel,str(output/'source-1.pcm'),'',True,False)
            if not options.legacy_grouped:
                builder.AddUser(name,30001+channel,str(output/'source-0.pcm'),'',False,False)
        builder.CheckFinalCommand()
        if options.legacy_grouped:
            time.sleep(0.5)
            for channel in range(options.channels):
                name='%s_%d'%(options.prefix,1000+channel)
                builder.AddUser(name,30001+channel,str(output/'source-0.pcm'),'',False,False)
            builder.CheckFinalCommand()
        while True:
            processes=[];tx=0
            for item in builder.processes:
                pid=item['pid'];status={}
                try:
                    status={line.split(':',1)[0]:line.split(':',1)[1].strip() for line in Path('/proc/%d/status'%pid).read_text().splitlines() if ':' in line}
                    processes.append({'pid':pid,'rssMiB':round(int(status['VmRSS'].split()[0])/1024,1),'threads':int(status['Threads'])})
                except (OSError,KeyError):
                    raise RuntimeError('Owned Native exited unexpectedly: %d'%pid)
                path=Path(item['log'])
                if path.exists():
                    with path.open(errors='replace') as stream:
                        stream.seek(offsets.get(str(path),0))
                        for line in stream:
                            match=re.search(r'onConnected: id [0-9]+, channelId ([^, ]+), localUserId ([0-9]+), reason',line)
                            if match:joined.add((match.group(1),int(match.group(2))))
                            disconnected=re.search(r'onDisconnected: id [0-9]+, channelId ([^, ]+), localUserId ([0-9]+), reason',line)
                            if disconnected:joined.discard((disconnected.group(1),int(disconnected.group(2))))
                            traffic=re.search(r'conn:[0-9]+ audio\(tx,rx\): ([0-9]+),([0-9]+)',line)
                            if traffic:tx+=int(traffic.group(1))
                        offsets[str(path)]=stream.tell()
            memory=int(Path('/sys/fs/cgroup/memory/memory.usage_in_bytes').read_text())
            current_sample=time.monotonic()
            rate=tx/max(0.001,current_sample-last_sample);last_sample=current_sample
            row={'pcmTxMBps':round(rate/1e6,3),'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'elapsed':round(time.monotonic()-started,1),'memoryGiB':round(memory/1024**3,3),'joined':len(joined),'expected':options.channels*3,'processes':processes}
            if len(joined)==options.channels*3 and ready_at is None:ready_at=time.monotonic()
            if ready_at is not None:row['readySeconds']=round(time.monotonic()-ready_at,1)
            observations.append(row)
            with (output/'memory.jsonl').open('a') as log:log.write(json.dumps(row)+'\n')
            compact={k:v for k,v in row.items() if k!='processes'}
            compact['rssRangeMiB']=[min(p['rssMiB'] for p in processes),max(p['rssMiB'] for p in processes)]
            Print(json.dumps(compact))
            if memory>16*1024**3:raise RuntimeError('Memory guard exceeded 16 GiB')
            if active_console_task():raise RuntimeError('Console task started; yielding resources')
            if ready_at is not None and time.monotonic()-ready_at>=options.duration:break
            if ready_at is None and time.monotonic()-started>300:raise RuntimeError('Join timeout')
            time.sleep(5)
        (output/'result.json').write_text(json.dumps({'state':'completed','observations':len(observations),'joined':len(joined)},indent=2))
    except BaseException as error:
        (output/'result.json').write_text(json.dumps({'state':'stopped','error':str(error)},indent=2))
        raise
    finally:
        builder.stop_owned()
        Print('Stopped only the Native groups owned by '+options.prefix)

if __name__=='__main__':
    comparison_main()
