# 如需直接从 Docker Hub 拉取：FROM ubuntu:18.04
FROM docker.m.daocloud.io/library/ubuntu:18.04

ENV DEBIAN_FRONTEND noninteractive

RUN dpkg --add-architecture amd64
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    apt-utils \
    git \
    curl \
    gdb \
    gcc \
    g++ \
    libc6 \
    sudo \
    unzip \
    vim \
    wget \
    python \
    python3 \
    python3-pip \
    python3-setuptools \
    python3-wheel \
    python3-dev

# install agoplay lib and fix dependencies
RUN apt-get install -y libx11-6 libgl1-mesa-glx libc++1 rsync libxcomposite-dev

# special deps
RUN pip3 install -i https://pypi.tuna.tsinghua.edu.cn/simple pyfarmhash
RUN apt update && apt install software-properties-common -y && \
    add-apt-repository ppa:ubuntu-toolchain-r/test -y && \
    apt update && \
    apt install -y gcc-7 libstdc++6

# init dirs
RUN mkdir -p /home/bbt && \
    mkdir -p /var/log/agora && \
    mkdir -p /data/tmp

# 媒体文件
COPY *.h264 *.wav /home/bbt/

# SDK 动态库 + 可执行文件
COPY *.so sample_send_h264_pcm /home/bbt/

# Python 压测脚本
COPY *.py /home/bbt/

# Shell 脚本
COPY cpu.sh stopAll.sh meeting-entrypoint.sh \
     start-meeting-container-server.sh stop-meeting-container-server.sh \
     case3_fa_999999.sh case5.sh check_run.sh case1_1v1_70c.sh \
     /home/bbt/

# save dockerfile for reference
COPY Dockerfile /tmp

RUN chmod +x \
    /home/bbt/sample_send_h264_pcm \
    /home/bbt/meeting-entrypoint.sh \
    /home/bbt/start-meeting-container-server.sh \
    /home/bbt/stop-meeting-container-server.sh \
    /home/bbt/stopAll.sh \
    /home/bbt/cpu.sh \
    /home/bbt/case3_fa_999999.sh \
    /home/bbt/case5.sh \
    /home/bbt/check_run.sh

WORKDIR /home/bbt

EXPOSE 8088

CMD ["python3", "/home/bbt/meeting_container_server.py"]
