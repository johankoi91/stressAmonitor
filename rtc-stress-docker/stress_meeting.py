import argparse
from audioop import add
import time
import sys
import os
import uuid
import json
import http.client
import random
import re
import threading
import subprocess
import time
import logging
import random
import copy

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
                 video_high_file,
                 video_low_file,
                 pub_audio,
                 pub_video_high,
                 pub_video_low,):
        self._cname = cname
        self._role = 0
        self._uid = uid
        # 走分配
        self._use_aut = 1
        self._audio_file = audio_file
        self._video_high_file = video_high_file
        self._video_low_file = video_low_file
        self._pub_audio = pub_audio
        self._pub_video_high = pub_video_high
        self._pub_video_low = pub_video_low

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
--audio-input={audio_file} --video-input={video_file} --dual-video-file={video_low_file} \
--a-sub-flag={sub_audio} --v-sub-flag={sub_video} \
--mute-audio={mute_audio} --mute-video={mute_video} --dual={pub_low_video} "

        self._end_line = "--mode={mode} --hold={hold} 1>{number}.log 2>&1 &"
        self._user_list = []

    def BuildAndRunCommand(self):
        global g_config
        self._user_list.sort()
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
                video_file=user._video_high_file,
                video_low_file=user._video_low_file,
                sub_audio=0,
                sub_video=0,
                mute_audio=user._pub_audio ^ 1,
                mute_video=user._pub_video_high ^ 1,
                pub_low_video=user._pub_video_low)
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

    def AddUser(self, cname, uid, audio, video_high, video_low, pub_audio, pub_video_high, pub_video_low):
        self._user_list.append(UserBase(cname, int(uid), audio, video_high, video_low, pub_audio, pub_video_high, pub_video_low))

        self._count += 1
        if self._count == g_config.user_per_proc_:
            self.BuildAndRunCommand()

    def CheckFinalCommand(self):
        # 最终可能有剩余command line没有执行 需要check
        if len(self._user_list) != 0:
            self.BuildAndRunCommand()

def Run(config_path):
    # 设置一些基本参数
    g_config.user_per_proc_ = 10
    g_config.video_fps_ = 30
    g_config.case_wait = 2

    if config_path == "":
        Print("Config is empty.")
        return
    command_builder = CommandBuilder()
    try:
        with open(config_path) as f:
            config = json.load(f)
            g_config.token_ = config["token"]
            g_config.cname_ = config["channel"]
            default_audio = config["default"]["audio"]
            default_vide_high = config["default"]["video_high"]
            default_vide_low = config["default"]["video_low"]

            for user_info in config["users"]:
                uid = int(user_info["userid"])
                pub_audio = 0 if "audio" in user_info and user_info["audio"] == "" else 1
                pub_video_high = 0 if "video_high" in user_info and user_info["video_high"] == "" else 1
                pub_video_low = 0 if "video_low" in user_info and user_info["video_low"] == "" else 1
                audio = default_audio if "audio" not in user_info or user_info["audio"] == "" else user_info["audio"]
                video_high = default_vide_high if "video_high" not in user_info or user_info["video_high"] == "" else user_info["video_high"]
                video_low = default_vide_low if "video_low" not in user_info or user_info["video_low"] == "" else user_info["video_low"]
                if pub_audio:
                    command_builder.AddUser(g_config.cname_, uid, audio, video_high, video_low, pub_audio, 0, 0)
                if pub_video_high or pub_video_low:
                    command_builder.AddUser("{}_{}".format(g_config.cname_, uid), uid, 0, video_high, video_low, 0, pub_video_high, pub_video_low)
            command_builder.CheckFinalCommand()

    except Exception as e:
        Print("Load config failed. Error: {}".format(e))
        return


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="--Meeting Test--")
    parser.add_argument('--config', type=str, default="")
    parser.add_argument('--method', type=str, default="start")
    args = parser.parse_args()

    if args.method == "stop":
        RunShellAndGetOutput('sudo kill `ps -ef | grep sample | grep -v grep | awk \'{print $2}\'`')
    elif args.method == "start":
        Run(args.config)