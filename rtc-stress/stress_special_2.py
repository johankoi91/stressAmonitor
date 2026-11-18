'''
Example:
python3 stress_special_2.py --channel_count 20 --user_per_process 5 --token {TOKEN} --local_ap {LOCAL_AP_IP} --local_ap_cert {CERT} --cname_base stress_poc_meeting --number_start 1 \
    --video_high_file output-1080p.h264 --video_low_file output-360p.h264 --method start
'''
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

stop_command = 'sudo kill `ps -ef | grep sample | grep -v grep | awk \'{print $2}\'`'

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
        time.sleep(g_config.case_wait)

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
    time.sleep(10)
    for i in range(channel_count):
        sub_number = i + number_start
        now_channel = "{}_{}".format(cname_base, sub_number)
        now_uid = uid_base * sub_number
        now_uid+=1
        for _ in range(4):
            command_builder.AddUser(now_channel, now_uid, "zxkgf.wav", video_low_file, False, True)
            now_uid+=1
    command_builder.CheckFinalCommand()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="--Multi Meeting Test--")
    parser.add_argument('--channel_count', type=int, default=1)
    parser.add_argument('--user_per_process', type=int, default=1)
    parser.add_argument('--token', type=str, default="")
    parser.add_argument('--local_ap', type=str, default="")
    parser.add_argument('--local_ap_cert', type=str, default="")
    parser.add_argument('--cname_base', type=str, default="")
    parser.add_argument('--number_start', type=int, default=1)  # 当前机器运行的频道编号, 机器间隔离
    parser.add_argument('--uid_base', type=int, default=10000)
    parser.add_argument('--video_fps', type=int, default=30)
    parser.add_argument('--video_high_file', type=str, default="test_1080p.h264")
    parser.add_argument('--video_low_file', type=str, default="test_360p.h264")
    parser.add_argument('--ip', type=str, default="")
    parser.add_argument('--port', type=str, default="")
    # parser.add_argument('--audio_subscribe', type=str, default="")
    # parser.add_argument('--video_low_subscribe', type=str, default="")
    # parser.add_argument('--video_high_subscribe', type=str, default="")
    parser.add_argument('--wait', type=int, default=2)
    parser.add_argument('--method', type=str, default="stop")
    args = parser.parse_args()

    if args.method == "stop":
        RunShellAndGetOutput('sudo kill `ps -ef | grep sample_send | grep -v grep | awk \'{print $2}\'`')
    elif args.method == "start":
        Run(args.channel_count, args.user_per_process, args.token, args.local_ap, args.local_ap_cert, args.cname_base, args.number_start, args.uid_base,\
            args.video_fps, args.wait, args.video_high_file, args.video_low_file, args.ip, args.port)
