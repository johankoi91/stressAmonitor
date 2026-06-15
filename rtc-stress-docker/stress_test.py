# 通用发送端脚本
'''
AUT
python3 stress_test.py \
--broadcaster_specify_vos 0 --broadcaster_specify_vos_aut 0 \
--broadcaster_allocation 2 --broadcaster_allocation_aut 0 \
--audience_specify_vos 0 --audience_specify_vos_aut 0 \
--audience_allocation 0 --audience_allocation_aut 0 \
--total_count 1 --user_per_proc 10 \
--uid_start 1 --uid_delta 10000 \
--sub_audio 1 --sub_video 1 --pub_audio 1 --pub_video 1 \
--pub_low_video 1 \
--random_sub_audio_count 1 \
--random_sub_video_high_count 0 \
--random_sub_video_low_count 1 \
--audio_file zxkgf.wav --video_file high_1000.h264 \
--video_low_file high_300.h264 \
--video_fps 30 \
--broadcaster_vos_list 1.1.1.1:4001:4010 \
--broadcaster_vosaut_list 2.2.2.2:4011:4020 \
--audience_vos_list 3.3.3.3:4001:4010 \
--audience_vosaut_list 4.4.4.4:4011:4020 \
--cname_base hybrid_test \
--token qaq \
--method start --persist 1 --case_wait 2
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

def GetLen(obj):
    if obj:
        return len(obj)
    return 0

# ————Global Info ———— #
uid_info = "--uid={}"
uid = 1
task_number = 0

stop_command = 'sudo kill `ps -ef | grep sample | grep -v grep | awk \'{print $2}\'`'

# ————Global Info End———— #


class StressConfig:
    def __init__(self):
        Print("Init stress config")
        # 模型控制
        self.broadcaster_specify_vos_: int
        self.broadcaster_specify_vos_aut_: int
        self.broadcaster_allocation_: int
        self.broadcaster_allocation_aut_: int
        self.audience_specify_vos_: int
        self.audience_specify_vos_aut_: int
        self.audience_allocation_: int
        self.audience_allocation_aut_: int

        # 用户配置
        self.total_count_: int
        self.user_per_proc_: int
        self.uid_start_: int
        self.uid_delta_: int

        # 流状态配置
        self.sub_audio_: int
        self.sub_video_: int
        self.sub_audio_users_: list
        self.sub_video_high_users_: list
        self.sub_video_low_users_: list
        self.random_sub_audio_count: int
        self.random_sub_video_high_count: int
        self.random_sub_video_low_count: int
        self.pub_audio_: int
        self.pub_video_: int
        self.pub_video_low_: int
        self.audio_file_: str
        self.video_file_: str
        self.video_low_file_: str
        self.video_fps_: int

        # server配置
        self.broadcaster_vos_list_: list
        self.broadcaster_vosaut_list_: list
        self.audience_vos_list_: list
        self.audience_vosaut_list_: list
        self.ap: str
        self.ap_cert: str

        # 频道配置
        self.cname_base_: str
        self.cname_suffix_start_: int
        self.private_static_token_: str
        self.public_token_: str

        # 脚本控制
        self.case_wait: int

    def ToDebugPrint(self):
        Print("GlobalConfig:\n{}".format(self.__dict__))


g_config = StressConfig()

# ————Global Info End————


class UserBase:

    def __init__(self,
                 cname,
                 role,
                 uid,
                 vosip,
                 vosport,
                 use_aut,
                 is_local,
                 sub_audio=[],
                 sub_high_video=[],
                 sub_low_video=[]):
        self._cname = cname
        self._role = role
        self._uid = uid
        self._vosip = vosip
        self._vosport = vosport
        self._use_aut = use_aut
        self._sub_audio = sub_audio
        self._sub_video_high = sub_high_video
        self._sub_video_low = sub_low_video
        self._is_local = is_local

    def __eq__(self, other):
        return self._cname == other._cname and self._use_aut == other._use_aut

    def __lt__(self, other):
        return self._use_aut > other._use_aut or (
            self._use_aut == other._use_aut and self._cname < other._cname)

    def __gt__(self, other):
        return self._use_aut < other._use_aut or (
            self._use_aut == other._use_aut and self._cname > other._cname)

def RemoveWithoutException(target_list, element_value):
    try:
        target_list.remove(element_value)
    except Exception as e:
        1

def GenerateRandomSubscribeUids(start_uid, count, now_uid):
    # 输入参数时会检查是否订阅是合法的
    global g_config
    audio_list = []
    high_list = []
    low_list = []

    audio_random_index_list = random.sample(range(0, count), count)
    RemoveWithoutException(audio_random_index_list, now_uid - start_uid)
    for i in g_config.sub_audio_users_:
        RemoveWithoutException(audio_random_index_list, i)

    video_random_index_list = random.sample(range(0, count), count)
    RemoveWithoutException(video_random_index_list, now_uid - start_uid)
    for i in g_config.sub_video_high_users_:
        RemoveWithoutException(video_random_index_list, i)
    for i in g_config.sub_video_low_users_:
        RemoveWithoutException(video_random_index_list, i)

    audio_random_index_list = [x + start_uid for x in audio_random_index_list]
    video_random_index_list = [x + start_uid for x in video_random_index_list]

    for i in g_config.sub_audio_users_:
        audio_list.append(i + start_uid)
    for i in g_config.sub_video_high_users_:
        high_list.append(i + start_uid)
    for i in g_config.sub_video_low_users_:
        low_list.append(i + start_uid)

    for i in range(g_config.random_sub_audio_count):
        audio_list.append(video_random_index_list[i])
    for i in range(g_config.random_sub_video_high_count):
        high_list.append(video_random_index_list[i])
    for i in range(g_config.random_sub_video_high_count,
                   g_config.random_sub_video_high_count +
                   g_config.random_sub_video_low_count):
        low_list.append(video_random_index_list[i])
    return audio_list, high_list, low_list


class CommandBuilder:

    def __init__(self):
        self._process_number = 1
        self._count = 0
        self._command_line = None

        # 命令行Base string
        self._start_line = "export LD_LIBRARY_PATH=./ && nohup ./sample_send_h264_pcm --video-rate={video_fps} "
        self._ip_port_line = "--ip={vosip} --port={vosport} "
        self._base_line_with_vos = "--token={token} -c {cname} -R {role} --uid={uid} \
--audio-input={audio_file} --video-input={video_file} --dual-video-file={video_low_file} \
{vos_info}\
--a-sub-flag={sub_audio} --v-sub-flag={sub_video} \
{subscribe_details} \
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
            vos_line=""
            # NOTE(jinpeng)
            # local_ap 和 vos非强相关, 不因在指定vos逻辑里判断
            if user._is_local == 1 and g_config.ap != "":
                vos_line += "--ap={} ".format(g_config.ap)
            if user._is_local == 1 and g_config.ap_cert != "":
                vos_line += "--ap_cert={} ".format(g_config.ap_cert)
            if user._vosip != "" and user._vosport != 0:
                vos_line += self._ip_port_line.format(vosip=user._vosip, vosport=user._vosport)
            subscribe_defail = ""
            if GetLen(user._sub_audio) != 0:
                subscribe_defail += "--a-sub-uids={} ".format(','.join(str(x) for x in user._sub_audio))
            if GetLen(user._sub_video_high) != 0:
                subscribe_defail += "--v-high-sub-uids={} ".format(','.join(str(x) for x in user._sub_video_high))
            if GetLen(user._sub_video_low) != 0:
                subscribe_defail += "--v-low-sub-uids={} ".format(','.join(str(x) for x in user._sub_video_low))
            self._command_line += self._base_line_with_vos.format(
                token=g_config.private_static_token_ if vos_line != "" else g_config.public_token_,
                cname=user._cname,
                role=user._role,
                uid=user._uid,
                audio_file=g_config.audio_file_,
                video_file=g_config.video_file_,
                video_low_file=g_config.video_low_file_,
                vos_info=vos_line,
                sub_audio=g_config.sub_audio_,
                sub_video=g_config.sub_video_,
                subscribe_details = subscribe_defail,
                mute_audio=g_config.pub_audio_ ^ 1,
                mute_video=g_config.pub_video_ ^ 1,
                pub_low_video=g_config.pub_video_low_)
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

    def AddUser(self, cname, role, uid, vosip, vosport, use_aut, is_local, base_uid=0):
        total_broadcaster_count = g_config.broadcaster_specify_vos_ +\
                                  g_config.broadcaster_specify_vos_aut_ +\
                                  g_config.broadcaster_allocation_ +\
                                  g_config.broadcaster_allocation_aut_
        subscribe_audio_list,\
            subscribe_video_high_list,\
                subscribe_video_low_list = \
                    GenerateRandomSubscribeUids(base_uid, total_broadcaster_count, uid)

        self._user_list.append(
            UserBase(cname, role, int(uid), vosip, vosport, use_aut, is_local,
                     subscribe_audio_list,
                     subscribe_video_high_list,
                     subscribe_video_low_list))

        self._count += 1
        if self._count == g_config.user_per_proc_:
            self.BuildAndRunCommand()

    def CheckFinalCommand(self):
        # 最终可能有剩余command line没有执行 需要check
        if len(self._user_list) != 0:
            self.BuildAndRunCommand()


def BuildVoss(input, result):
    if not input or len(input) == 0:
        result = []
        return
    temp_voss = []
    for vos in input:
        try:
            vosip, stp, enp = vos.split(":")
        except Exception as e:
            raise Exception("VossFormatError")
        temp_voss.append([vosip, stp, enp])

    for ip, st_port, en_port in temp_voss:
        for p in range(int(st_port), int(en_port) + 1):
            result.append("{}:{}".format(ip, p))


def InitGlobal(total_count, usr_per_proc, uid_start, uid_delta, sub_audio,
               sub_video, pub_audio, pub_video, pub_video_low,
               broadcaster_vos_list,
               broadcaster_vosaut_list, audience_vos_list,
               audience_vosaut_list, cname_base, method, audio_file,
               video_file, video_low_file, token, broadcaster_specify_vos,
               broadcaster_specify_vos_aut, broadcaster_allocation,
               broadcaster_allocation_aut, audience_specify_vos,
               audience_specify_vos_aut, audience_allocation,
               audience_allocation_aut, video_fps, cname_suffix_start,
               sub_audio_users, sub_video_high_users,
               sub_video_low_users, case_wait,
               ap, ap_cert, public_token,
               random_sub_audio_count, random_sub_video_high_count,
               random_sub_video_low_count):
    global g_config
    if method == "stop":
        Print("Stop with command : {}".format(stop_command))
        RunShellAndGetOutput(stop_command)
        exit(0)
    elif method == "start":
        try:
            # NOTE(jinpeng)
            # 此处逻辑更改为 当人数不为空但列表为空时 表示通过local ap分配
            # if (broadcaster_specify_vos != 0 and len(broadcaster_vos_list) == 0):
            #     raise Exception("BroadcasterSpecifyVosError")
            # if (broadcaster_specify_vos_aut != 0 and len(broadcaster_vosaut_list) == 0):
            #     raise Exception("BroadcasterSpecifyVosAutError")
            # if (audience_specify_vos != 0 and len(audience_vos_list) == 0):
            #     raise Exception("AudienceSpecifyVosError")
            # if (audience_specify_vos_aut != 0 and len(audience_vosaut_list) == 0):
            #     raise Exception("AudienceSpecifyVosAutError")

            if (sub_audio != 0 and sub_audio != 1) or\
               (sub_video != 0 and sub_video != 1) or\
               (pub_audio != 0 and pub_audio != 1) or\
               (pub_video != 0 and pub_video != 1) or\
               (pub_video_low != 0 and pub_video_low != 1):
                raise Exception("PubSubFlagInvalid")
            if token == "":
                raise Exception("TokenInvalid")

            total_broadcasters_count = broadcaster_specify_vos + broadcaster_specify_vos_aut + broadcaster_allocation + broadcaster_allocation_aut
            sub_audio_users = sub_audio_users if sub_audio_users != None else []
            sub_video_high_users = sub_video_high_users if sub_video_high_users != None else []
            sub_video_low_users = sub_video_low_users if sub_video_low_users != None else []
            if GetLen(sub_audio_users
                   ) + random_sub_audio_count >= total_broadcasters_count:
                raise Exception("SubAudioUserCountInvalid")
            if GetLen(sub_video_high_users) + GetLen(sub_video_low_users) +\
                random_sub_video_high_count + random_sub_video_low_count \
                    >= total_broadcasters_count:
                raise Exception("SubVideoHighUserCountInvalid")
            # 指定uid这个其实不太好用 因为指定的是相对base uid的值 当下场景认为random uid可以满足需求 后续有需求再改
            # 相对值从0开始
            for i in sub_audio_users:
                if i >= total_broadcasters_count:
                    raise Exception("WrongSubAudioUsers")
            for i in sub_video_high_users:
                if i >= total_broadcasters_count:
                    raise Exception("WrongSubVideoHighUsers")
            for i in sub_video_low_users:
                if i >= total_broadcasters_count or i in sub_video_high_users:
                    raise Exception("WrongSubVideoLowUsers")

            g_config.broadcaster_specify_vos_ = broadcaster_specify_vos
            g_config.broadcaster_specify_vos_aut_ = broadcaster_specify_vos_aut
            g_config.broadcaster_allocation_ = broadcaster_allocation
            g_config.broadcaster_allocation_aut_ = broadcaster_allocation_aut
            g_config.audience_specify_vos_ = audience_specify_vos
            g_config.audience_specify_vos_aut_ = audience_specify_vos_aut
            g_config.audience_allocation_ = audience_allocation
            g_config.audience_allocation_aut_ = audience_allocation_aut

            g_config.total_count_ = total_count
            g_config.user_per_proc_ = usr_per_proc
            g_config.uid_start_ = uid_start
            g_config.uid_delta_ = uid_delta
            g_config.sub_audio_ = sub_audio
            g_config.sub_video_ = sub_video
            g_config.pub_audio_ = pub_audio
            g_config.pub_video_ = pub_video
            g_config.pub_video_low_ = pub_video_low
            g_config.audio_file_ = audio_file
            g_config.video_file_ = video_file
            g_config.video_low_file_ = video_low_file

            g_config.broadcaster_vos_list_ = []
            g_config.broadcaster_vosaut_list_ = []
            g_config.audience_vos_list_ = []
            g_config.audience_vosaut_list_ = []

            g_config.cname_base_ = cname_base
            g_config.private_static_token_ = token
            g_config.public_token_ = public_token
            if public_token == "":
                # 当不存在public_token时 fallback到静态token
                g_config.public_token_ = token
            g_config.video_fps_ = video_fps
            g_config.cname_suffix_start_ = cname_suffix_start
            g_config.case_wait = case_wait

            g_config.sub_audio_users_ = sub_audio_users
            g_config.sub_video_high_users_ = sub_video_high_users
            g_config.sub_video_low_users_ = sub_video_low_users
            g_config.random_sub_audio_count = random_sub_audio_count
            g_config.random_sub_video_high_count = random_sub_video_high_count
            g_config.random_sub_video_low_count = random_sub_video_low_count

            BuildVoss(broadcaster_vos_list, g_config.broadcaster_vos_list_)
            BuildVoss(broadcaster_vosaut_list, g_config.broadcaster_vosaut_list_)
            BuildVoss(audience_vos_list, g_config.audience_vos_list_)
            BuildVoss(audience_vosaut_list, g_config.audience_vosaut_list_)
            g_config.ap = ap
            g_config.ap_cert = ap_cert

            g_config.ToDebugPrint()

        except Exception as e:
            logging.exception(e)
            exit(0)
    else:
        Print("method not support")
        exit(0)


# class VosOutOfOrderGenerator:
#   def __init__(self, vos_list):
#     self._vos_list = vos_list
#     self._index = 0
#   def GetNextVos(self):
#     if len(self._vos_list) == 0:
#       sys.exit(-1)
#     vos = self._vos_list[self._index]
#     self._index = (self._index + 1) % len(self._vos_list)
#     return vos


def Run(persist):
    global g_config
    # 初始化相关参数
    command_builder = CommandBuilder()
    now_uid = g_config.uid_start_
    now_cname_suffix_id = now_uid
    if g_config.cname_suffix_start_ != 0:
        now_cname_suffix_id = g_config.cname_suffix_start_

    channel = g_config.cname_base_ + "_" + str(now_cname_suffix_id)
    broadcaster_vos_pos = 0
    broadcaster_vosaut_pos = 0
    audience_vos_pos = 0
    audience_vosaut_pos = 0

    USE_AUT = 1
    NOT_USE_AUT = 0

    ROLE_BROADCASTER = 0
    ROLE_AUDIENCE = 1

    LOCAL_USER = 1
    NOT_LOCAL_USER = 0

    for _ in range(g_config.total_count_):
        other_uid = now_uid * g_config.uid_delta_ + 1
        base_uid = other_uid

        # 主播 指定vos
        for __ in range(g_config.broadcaster_specify_vos_):
            ip, port = ["", 0]
            if len(g_config.broadcaster_vos_list_) > 0:
                stress_vos = g_config.broadcaster_vos_list_[broadcaster_vos_pos]
                broadcaster_vos_pos = (broadcaster_vos_pos + 1) % len(g_config.broadcaster_vos_list_)
                ip, port = stress_vos.split(':')
            command_builder.AddUser(channel, ROLE_BROADCASTER, other_uid, ip, port, NOT_USE_AUT, LOCAL_USER, base_uid)
            other_uid += 1
        # 主播 不指定vos
        for __ in range(g_config.broadcaster_allocation_):
            command_builder.AddUser(channel, ROLE_BROADCASTER, other_uid, "", 0, NOT_USE_AUT, NOT_LOCAL_USER, base_uid)
            other_uid += 1

        # 主播 指定vosaut
        for __ in range(g_config.broadcaster_specify_vos_aut_):
            ip, port = ["", 0]
            if len(g_config.broadcaster_vosaut_list_) > 0:
                stress_vos = g_config.broadcaster_vosaut_list_[broadcaster_vosaut_pos]
                broadcaster_vosaut_pos = (broadcaster_vosaut_pos + 1) % len(g_config.broadcaster_vosaut_list_)
                ip, port = stress_vos.split(':')
            command_builder.AddUser(channel, ROLE_BROADCASTER, other_uid, ip, port, USE_AUT, LOCAL_USER, base_uid)
            other_uid += 1
        # 主播 不指定vosaut
        for __ in range(g_config.broadcaster_allocation_aut_):
            command_builder.AddUser(channel, ROLE_BROADCASTER, other_uid, "", 0, USE_AUT, NOT_LOCAL_USER, base_uid)
            other_uid += 1

        # 观众 指定vos
        for __ in range(g_config.audience_specify_vos_):
            ip, port = ["", 0]
            if len(g_config.audience_vos_list_) > 0:
                stress_vos = g_config.audience_vos_list_[audience_vos_pos]
                audience_vos_pos = (audience_vos_pos + 1) % len(g_config.audience_vos_list_)
                ip, port = stress_vos.split(':')
            command_builder.AddUser(channel, ROLE_AUDIENCE, other_uid, ip, port, NOT_USE_AUT, LOCAL_USER)
            other_uid += 1
        # 观众 不指定vos
        for __ in range(g_config.audience_allocation_):
            command_builder.AddUser(channel, ROLE_AUDIENCE, other_uid, "", 0, NOT_USE_AUT, NOT_LOCAL_USER)
            other_uid += 1

        # 观众 指定vosaut
        for __ in range(g_config.audience_specify_vos_aut_):
            ip, port = ["", 0]
            if len(g_config.audience_vosaut_list_) > 0:
                stress_vos = g_config.audience_vosaut_list_[audience_vosaut_pos]
                audience_vosaut_pos = (audience_vosaut_pos + 1) % len(g_config.audience_vosaut_list_)
                ip, port = stress_vos.split(':')
            command_builder.AddUser(channel, ROLE_AUDIENCE, other_uid, ip, port, USE_AUT, LOCAL_USER)
            other_uid += 1
        # 观众 不指定vos
        for __ in range(g_config.audience_allocation_aut_):
            command_builder.AddUser(channel, ROLE_AUDIENCE, other_uid, "", 0, USE_AUT, NOT_LOCAL_USER)
            other_uid += 1

        now_uid += 1
        now_cname_suffix_id += 1
        channel = g_config.cname_base_ + "_" + str(now_cname_suffix_id)

    # 未运行的任务
    command_builder.CheckFinalCommand()

    if persist:
        while True:
            time.sleep(1)
    return


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Auto Run Agoplay")
    # 模型控制
    parser.add_argument('--broadcaster_specify_vos', type=int, default=0)
    parser.add_argument('--broadcaster_specify_vos_aut', type=int, default=0)
    parser.add_argument('--broadcaster_allocation', type=int, default=0)
    parser.add_argument('--broadcaster_allocation_aut', type=int, default=0)
    parser.add_argument('--audience_specify_vos', type=int, default=0)
    parser.add_argument('--audience_specify_vos_aut', type=int, default=0)
    parser.add_argument('--audience_allocation', type=int, default=0)
    parser.add_argument('--audience_allocation_aut', type=int, default=0)

    # 用户控制
    parser.add_argument('--total_count', type=int, default=10)  # 起多少个上述模型频道
    parser.add_argument('--user_per_proc', type=int, default=10)  # 单进程主播个数
    parser.add_argument('--uid_start', type=int, default=1)  # uid起始值
    parser.add_argument('--uid_delta', type=int,
                        default=10000)  # 频道间uid间隔 保证不冲突(为了调查问题方便)

    # 流控制
    parser.add_argument('--sub_audio', type=int, default=1)  # 是否订阅音频 0/1
    parser.add_argument('--sub_video', type=int, default=1)  # 是否订阅视频 0/1
    parser.add_argument('--sub_audio_users', nargs='+')  # 订阅哪些主播audio 注意因为是并发测试 这里填的是相对于主播的delta
    parser.add_argument('--sub_video_high_users', nargs='+')  # 订阅哪些主播video high
    parser.add_argument('--sub_video_low_users', nargs='+')  # 订阅哪些主播video low
    parser.add_argument('--random_sub_audio_count', type=int, default=0)  # 订阅哪些主播audio
    parser.add_argument('--random_sub_video_high_count', type=int, default=0)  # 订阅哪些主播video high
    parser.add_argument('--random_sub_video_low_count', type=int, default=0)  # 订阅哪些主播video low
    parser.add_argument('--pub_audio', type=int, default=1)  # 是否发音频 0/1
    parser.add_argument('--pub_video', type=int, default=1)  # 是否发视频 0/1
    parser.add_argument('--pub_low_video', type=int, default=1)  # 是否发视频 0/1
    parser.add_argument('--audio_file', type=str, default="zxkgf.wav")
    parser.add_argument('--video_file', type=str, default="zxkgf-360p.h264")
    parser.add_argument('--video_low_file', type=str, default="zxkgf-180p.h264")
    parser.add_argument('--video_fps', type=int, default=30)  # 视频fps

    # server控制
    parser.add_argument('--broadcaster_vos_list',
                        nargs='+')  # 主播 edge1 传入一些string "ip:stport:enport"
    parser.add_argument('--broadcaster_vosaut_list', nargs='+')  # 主播 edge2
    parser.add_argument('--audience_vos_list', nargs='+')  # 观众 edge1
    parser.add_argument('--audience_vosaut_list', nargs='+')  # 观众 edge2
    parser.add_argument('--ap', type=str, default="")  # 指定的local ap
    parser.add_argument('--ap_cert', type=str, default="")  # 指定的local ap cert

    # 频道控制
    parser.add_argument('--cname_base', type=str, default='test')  # 加入的频道名前缀
    parser.add_argument('--cname_suffix_start', type=int,
                        default=0)  # 加入的频道名后缀起始值, 不填使用uid起始值
    parser.add_argument('--token', type=str, default="")  # 直接输入Token
    parser.add_argument('--public_token', type=str, default="")  # 连接公网的token

    # 脚本功能控制
    parser.add_argument('--method', type=str,
                        default='stop')  # 脚本运行的功能 start还是stop
    parser.add_argument('--persist', type=bool, default=False)  # 自动化支持 是否持续运行
    parser.add_argument('--case_wait', type=float, default=0.5)  # case间运行等待时间

    args = parser.parse_args()
    InitGlobal(args.total_count, args.user_per_proc, args.uid_start,
               args.uid_delta, args.sub_audio, args.sub_video, args.pub_audio,
               args.pub_video, args.pub_low_video, args.broadcaster_vos_list,
               args.broadcaster_vosaut_list, args.audience_vos_list,
               args.audience_vosaut_list, args.cname_base, args.method,
               args.audio_file, args.video_file, args.video_low_file,
               args.token,
               args.broadcaster_specify_vos, args.broadcaster_specify_vos_aut,
               args.broadcaster_allocation, args.broadcaster_allocation_aut,
               args.audience_specify_vos, args.audience_specify_vos_aut,
               args.audience_allocation, args.audience_allocation_aut,
               args.video_fps, args.cname_suffix_start, args.sub_audio_users,
               args.sub_video_high_users, args.sub_video_low_users,
               args.case_wait, args.ap, args.ap_cert,
               args.public_token, args.random_sub_audio_count,
               args.random_sub_video_high_count,
               args.random_sub_video_low_count)
    Run(args.persist)
