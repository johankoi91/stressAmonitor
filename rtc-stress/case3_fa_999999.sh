export LD_LIBRARY_PATH=./ && nohup ./sample_send_h264_pcm --video-rate=15 \
--token=f29d4dc623c741a3ad38ccaf27a8d2e2 -c agora0001 -R 0 --uid=999999 --audio-input=zxkgf.wav --video-input=ceshi_1080p.h264 --ap=172.31.11.174 --ap_cert=ap.1452738.agora.local --a-sub-flag=0 --v-sub-flag=0 --mute-audio=0 --mute-video=0 --use-aut=0 \
--mode=0 --hold=999999999 &
