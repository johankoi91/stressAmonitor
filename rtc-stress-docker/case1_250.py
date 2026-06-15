for i in range(0, 200):
	c = 1000 + i*2 + 1
	p = 4001
	l = 1000 + i + 1
	print("export LD_LIBRARY_PATH=./ && nohup ./sample_send_h264_pcm --video-rate=15 \
	--token=aab8b8f5a8cd4469a63042fcfafe7063 -c agora%d -R 0 --uid=10001 --audio-input=zxkgf.wav --video-input=high_2260.h264 --ip=175.178.45.250 --port=%d --ap=175.178.45.250 --ap_cert=ap.1145326.agora.local --a-sub-flag=1 --v-sub-flag=1 --mute-audio=0 --mute-video=0 --use-aut=0 -O \
	--token=aab8b8f5a8cd4469a63042fcfafe7063 -c agora%d -R 0 --uid=10002 --video-input=high_2260.h264 --ip=175.178.45.250 --port=%d --ap=175.178.45.250 --ap_cert=ap.1145326.agora.local --a-sub-flag=1 --v-sub-flag=1 --mute-audio=1 --mute-video=0 --use-aut=0 -O \
	--token=aab8b8f5a8cd4469a63042fcfafe7063 -c agora%d -R 0 --uid=10001 --audio-input=zxkgf.wav --video-input=high_2260.h264 --ip=175.178.45.250 --port=%d --ap=55.145.1.200 --ap_cert=ap.1145326.agora.local --a-sub-flag=1 --v-sub-flag=1 --mute-audio=0 --mute-video=0 --use-aut=0 -O \
	--token=aab8b8f5a8cd4469a63042fcfafe7063 -c agora%d -R 0 --uid=10002 --video-input=high_2260.h264 --ip=175.178.45.250 --port=%d --ap=175.178.45.250 --ap_cert=ap.1145326.agora.local --a-sub-flag=1 --v-sub-flag=1 --mute-audio=1 --mute-video=0 --use-aut=0 \
	--mode=0 --hold=999999999 1>%d.log 2>&1 &"%(c,p, c,p, c+1, p, c+1, p, l))
