'''
单机运行, 适应于大频道测试场景
仅运行观众, 观众支持订阅
所有订阅参数都是一个字符串, 多个uid用逗号隔开, 空表示不订阅

Example:
python3 stress_special_1.py --total 3 --token aab8b8f5a8cd4469a63042fcfafe7063 --local_ap 10.72.0.29 --cname zjmmm-test-1 --uid_start 10001 --audio_subscribe 2 --video_low_subscribe 1,3 --video_high_subscribe 2
'''
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
        self.speaker_start_: int
        self.speaker_end_: int
        self.sub_high_count_: int
        self.sub_low_count_: int

        # 流状态配置
        self.video_fps_: int
        self.audio_file_: str
        self.video_high_file_: str
        self.video_low_file_: str
        self.pub_audio_: bool

        # 强制加入同一个vos
        self.vos_info_: str

        # 频道配置
        self.cname_: str
        self.token_: str
        self.ap_: str

        # 脚本控制
        self.case_wait: int

    def ToDebugPrint(self):
        Print("GlobalConfig:\n{}".format(self.__dict__))


g_config = StressConfig()

class UserBase:
    def __init__(self,
                 cname,
                 uid,
                 sub_audio,
                 sub_video_high,
                 sub_video_low):
        self._cname = cname
        self._role = 1
        self._uid = uid
        # 走分配
        self._use_aut = 0
        self._sub_audio = ""
        self._sub_video_high = ""
        self._sub_video_low = sub_video_low

    def __eq__(self, other):
        return self._cname == other._cname and self._use_aut == other._use_aut

    def __lt__(self, other):
        return self._use_aut > other._use_aut or (
            self._use_aut == other._use_aut and self._cname < other._cname)

    def __gt__(self, other):
        return self._use_aut < other._use_aut or (
            self._use_aut == other._use_aut and self._cname > other._cname)

def GenerateSubscribeString(self_uid, speaker_start, speaker_end, sub_count, except_list=[]):
    numbers = list(range(speaker_start, speaker_end + 1))
    random.shuffle(numbers)
    ret_str = ""
    speaker_count = 0
    ret_list = []
    Print(numbers)
    for i in range(len(numbers)):
        if i >= len(numbers):
            break
        if numbers[i] == self_uid or numbers[i] in except_list:
            continue
        ret_str += str(numbers[i]) + ","
        ret_list.append(numbers[i])
        speaker_count+=1
        if speaker_count == sub_count:
            break
    if len(ret_str) > 0:
        ret_str = ret_str[:-1]
    return ret_str, ret_list


class CommandBuilder:

    def __init__(self):
        self._process_number = 1
        self._count = 0
        self._command_line = None

        # 命令行Base string
        self._start_line = "export LD_LIBRARY_PATH=./ && nohup ./sample_send_h264_pcm --video-rate={video_fps} "
        self._base_line_with_vos = "--token={token} -c {cname} -R {role} --uid={uid} \
--ap={local_ap} --ap_cert={local_ap_cert} \
{vosinfo}\
--audio-input={a_in} --video-input={vh_in} --dual-video-file={vl_in} \
--a-sub-flag={sub_audio} --v-sub-flag={sub_video} \
{subscribe_detail}\
--mute-audio={mute_audio} --mute-video={mute_video} --dual={dual} "

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
            sub_detail = ""
            if user._sub_audio != "":
                sub_detail += "--a-sub-uids={} ".format(user._sub_audio)
            if user._sub_video_high != "":
                sub_detail += "--v-high-sub-uids={} ".format(user._sub_video_high)
            if user._sub_video_low != "":
                sub_detail += "--v-low-sub-uids={} ".format(user._sub_video_low)

            self._command_line += self._base_line_with_vos.format(
                token=g_config.token_,
                cname=user._cname,
                role=user._role,
                uid=user._uid,
                a_in = g_config.audio_file_,
                vh_in = g_config.video_high_file_,
                vl_in = g_config.video_low_file_,
                local_ap=g_config.local_ap_,
                local_ap_cert=g_config.local_ap_cert_,
                vosinfo=g_config.vos_info_,
                sub_audio=1,
                sub_video=1,
                subscribe_detail = sub_detail,
                mute_audio=0 if g_config.pub_audio_ else 1,
                mute_video=0,
                dual=1,
                pub_low_video=0)
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

    def AddUser(self, cname, uid):
        if g_config.sub_high_count_ or g_config.sub_low_count_ > 0:
            high_sub_detail, high_sub_list = GenerateSubscribeString(uid,
                                                                     g_config.speaker_start_,
                                                                     g_config.speaker_end_,
                                                                     g_config.sub_high_count_)
            low_sub_detail, _ = GenerateSubscribeString(uid,
                                                        g_config.speaker_start_,
                                                        g_config.speaker_end_,
                                                        g_config.sub_low_count_,
                                                        high_sub_list)
            self._user_list.append(UserBase(cname, uid, "", high_sub_detail, low_sub_detail))
        else:
            self._user_list.append(UserBase(cname, uid, "", "", ""))

        self._count += 1
        if self._count == g_config.user_per_proc_:
            self.BuildAndRunCommand()

    def CheckFinalCommand(self):
        # 最终可能有剩余command line没有执行 需要check
        if len(self._user_list) != 0:
            self.BuildAndRunCommand()

def Run(total, user_per_proc, token, local_ap, local_ap_cert, cname, uid_start, sub_high_count, sub_low_count,
        speaker_start, speaker_end, audio, video_high, video_low,
        vos_info, wait, pub_audio):
    # 设置一些基本参数
    g_config.user_per_proc_ = user_per_proc
    g_config.video_fps_ = 30
    g_config.case_wait = wait

    command_builder = CommandBuilder()

    g_config.token_ = token
    g_config.local_ap_ = local_ap
    g_config.local_ap_cert_ = local_ap_cert

    g_config.speaker_start_ = speaker_start
    g_config.speaker_end_ = speaker_end
    g_config.sub_high_count_ = sub_high_count
    g_config.sub_low_count_ = sub_low_count

    g_config.audio_file_ = audio
    g_config.video_high_file_ = video_high
    g_config.video_low_file_ = video_low
    g_config.pub_audio_ = pub_audio

    if vos_info != "":
        g_config.vos_info_ = "--ip={vosip} --port={vosport} ".format(
            vosip = vos_info.split(":")[0], vosport = vos_info.split(":")[1]
        )
    else:
        g_config.vos_info_ = ""

    for i in range(total):
        uid = uid_start + i
        command_builder.AddUser(cname, uid)
    command_builder.CheckFinalCommand()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="--Huge channel test--")
    parser.add_argument('--total', type=int, default=1)
    parser.add_argument('--user_per_proc', type=int, default=1)
    parser.add_argument('--token', type=str, default="")
    parser.add_argument('--local_ap', type=str, default="")
    parser.add_argument('--local_ap_cert', type=str, default="")
    parser.add_argument('--cname', type=str, default="")
    parser.add_argument('--uid_start', type=int, default=10001)
    parser.add_argument('--subscribe_high_count', type=int, default=0)
    parser.add_argument('--subscribe_low_count', type=int, default=0)
    parser.add_argument('--speaker_start', type=int, default=0)  # 全局主播uid起始值
    parser.add_argument('--speaker_end', type=int, default=0)  # 全局主播uid结束值
    parser.add_argument('--audio', type=str, default="zxkgf.wav")
    parser.add_argument('--video_high', type=str, default="high_300.h264")
    parser.add_argument('--video_low', type=str, default="high_300.h264")
    parser.add_argument('--vos_info', type=str, default="")
    parser.add_argument('--wait', type=int, default=2)
    parser.add_argument('--pub_audio', type=bool, default=False)
    parser.add_argument('--method', type=str, default="stop")
    args = parser.parse_args()

    if args.method == "stop":
        RunShellAndGetOutput('sudo kill `ps -ef | grep sample_send_h264_pcm | grep -a ' + args.cname + ' | grep -v grep | awk \'{print $2}\'`')
    elif args.method == "start":
        Run(args.total, args.user_per_proc, args.token, args.local_ap, args.local_ap_cert, args.cname, args.uid_start,
            args.subscribe_high_count, args.subscribe_low_count, args.speaker_start,
            args.speaker_end, args.audio, args.video_high, args.video_low,
            args.vos_info, args.wait, args.pub_audio)
