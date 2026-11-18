// Extracted from customVideoSource: createClient -> local AP -> file capture ->
// custom audio/video tracks -> join/publish -> subscribe -> stop/close/leave.
(() => {
  const errorText = error => error?.message || String(error);
  const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

  window.RTCFileMedia = {
    async capture(video, mediaUrl, timeoutMs = 12000) {
      if (!video) throw new Error('Video element is required');
      if (video.src !== new URL(mediaUrl, location.href).href) video.src = mediaUrl;
      video.loop = true;
      video.playsInline = true;
      await video.play();
      const capture = video.captureStream || video.mozCaptureStream;
      if (!capture) { video.pause(); throw new Error('HTMLVideoElement.captureStream is unavailable'); }
      const stream = capture.call(video);
      const deadline = performance.now() + timeoutMs;
      while ((!stream.getAudioTracks().length || !stream.getVideoTracks().length) && performance.now() < deadline) await sleep(30);
      if (!stream.getAudioTracks().length || !stream.getVideoTracks().length) {
        stream.getTracks().forEach(track => track.stop());
        video.pause();
        throw new Error('Media file must provide audio and video tracks');
      }
      return stream;
    },
  };

  class RTCFileSession {
    constructor({ accessPoints, mediaUrl = '/media/input', timeoutMs = 12000, onEvent } = {}) {
      this.client = AgoraRTC.createClient({ mode: 'rtc', codec: 'vp8' });
      this.state = 'idle';
      this.error = null;
      this.localTracks = [];
      this.capturedTracks = [];
      this.remote = new Map();
      this.events = [];
      this.mediaUrl = mediaUrl;
      this.timeoutMs = timeoutMs;
      this.onEvent = onEvent;
      this.video = document.querySelector('#file-source');
      this.handlers = {
        'user-published': (user, type) => { this.subscribe(user, type).catch(error => this.record('subscribe-error', { uid: user.uid, mediaType: type, message: errorText(error) })); },
        'user-unpublished': (user, type) => this.unpublished(user, type),
        'network-quality': quality => this.record('network-quality', quality),
        'connection-state-change': (current, previous, reason) => this.record('connection-state', { current, previous, reason }),
      };
      for (const [event, handler] of Object.entries(this.handlers)) this.client.on(event, handler);
      if (accessPoints?.serverList?.length) {
        this.client.setLocalAccessPointsV2({ accessPoints });
        this.record('access-points', { ...accessPoints });
      }
      this.record('initialized', { sdkVersion: AgoraRTC.VERSION });
    }

    record(type, data = {}) {
      const event = { type, ...data };
      this.events.push(event);
      this.onEvent?.(event);
      document.querySelector('#status').textContent = JSON.stringify({ state: this.state, lastEvent: event }, null, 2);
    }

    validate({ appId, channel, uid, role }) {
      if (!/^[0-9a-f]{32}$/i.test(appId || '')) throw new Error('appId must contain 32 hexadecimal characters');
      if (!channel || typeof channel !== 'string') throw new Error('channel is required');
      if (!Number.isInteger(uid) || uid < 1 || uid > 4294967295) throw new Error('uid must be an integer in 1..4294967295');
      if (!['publisher', 'subscriber'].includes(role)) throw new Error('role must be publisher or subscriber');
    }

    async prepareMedia() {
      const stream = await RTCFileMedia.capture(this.video, this.mediaUrl, this.timeoutMs);
      this.capturedTracks = stream.getTracks();
      this.localTracks.push(await AgoraRTC.createCustomAudioTrack({ mediaStreamTrack: stream.getAudioTracks()[0] }));
      this.localTracks.push(await AgoraRTC.createCustomVideoTrack({ mediaStreamTrack: stream.getVideoTracks()[0] }));
      this.record('media-ready', { kinds: this.capturedTracks.map(track => track.kind) });
    }

    async start({ appId, channel, uid, token = '', role = 'publisher' }) {
      this.validate({ appId, channel, uid, role });
      if (['joining', 'connected', 'leaving'].includes(this.state)) throw new Error('Session is already active');
      this.state = 'joining';
      this.error = null;
      this.record('joining', { appId, channel, uid, role, tokenPresent: !!token });
      try {
        if (role === 'publisher') await this.prepareMedia();
        this.uid = await this.client.join(appId, channel, token || null, uid);
        if (role === 'publisher') {
          await this.client.publish(this.localTracks);
          this.localTracks.find(track => track.getMediaStreamTrack().kind === 'video').play('local-player');
          this.record('published', { kinds: this.localTracks.map(track => track.getMediaStreamTrack().kind) });
        }
        this.state = 'connected';
        this.record('connected', { uid: this.uid, role });
        return this.snapshot();
      } catch (error) {
        this.releaseMedia();
        await this.client.leave().catch(() => {});
        this.state = 'failed';
        this.error = errorText(error).replaceAll(token || '\u0000', '[REDACTED]');
        this.record('start-failed', { message: this.error });
        throw error;
      }
    }

    async subscribe(user, mediaType) {
      await this.client.subscribe(user, mediaType);
      let entry = this.remote.get(user.uid);
      if (!entry) { entry = { user, audio: false, video: false }; this.remote.set(user.uid, entry); }
      entry[mediaType] = true;
      if (mediaType === 'video') {
        let player = document.getElementById(`remote-${user.uid}`);
        if (!player) { player = document.createElement('div'); player.id = `remote-${user.uid}`; player.style.cssText = 'width:320px;height:180px'; document.querySelector('#remote-players').append(player); }
        user.videoTrack.play(player.id);
      } else if (mediaType === 'audio') user.audioTrack.play();
      this.record('subscribed', { uid: user.uid, mediaType });
    }

    unpublished(user, mediaType) {
      const entry = this.remote.get(user.uid);
      if (!entry) return;
      entry[mediaType] = false;
      user[`${mediaType}Track`]?.stop?.();
      if (mediaType === 'video') document.getElementById(`remote-${user.uid}`)?.remove();
      if (!entry.audio && !entry.video) this.remote.delete(user.uid);
      this.record('unpublished', { uid: user.uid, mediaType });
    }

    releaseMedia() {
      for (const track of this.localTracks) { try { track.stop(); track.close(); } catch {} }
      this.localTracks = [];
      for (const track of this.capturedTracks) track.stop();
      this.video.pause();
      document.querySelector('#local-player').replaceChildren();
    }

    async stop() {
      this.state = 'leaving';
      this.releaseMedia();
      for (const entry of this.remote.values()) { entry.user.audioTrack?.stop?.(); entry.user.videoTrack?.stop?.(); }
      this.remote.clear();
      document.querySelector('#remote-players').replaceChildren();
      try {
        await this.client.leave();
        this.state = 'idle';
        this.record('left');
      } catch (error) {
        this.state = 'failed'; this.error = errorText(error); this.record('leave-failed', { message: this.error }); throw error;
      }
    }

    async destroy() {
      await this.stop().catch(() => {});
      for (const [event, handler] of Object.entries(this.handlers)) this.client.off?.(event, handler);
    }

    snapshot() {
      const describe = track => ({ kind: track.kind, readyState: track.readyState });
      return {
        state: this.state, uid: this.uid, error: this.error,
        localTracks: this.localTracks.map(track => describe(track.getMediaStreamTrack())),
        capturedTracks: this.capturedTracks.map(describe),
        remoteUsers: [...this.remote].map(([uid, entry]) => ({ uid, audio: entry.audio, video: entry.video })),
        mediaPaused: this.video.paused, mediaReadyState: this.video.readyState,
        localAudioStats: this.client.getLocalAudioStats(), localVideoStats: this.client.getLocalVideoStats(),
        remoteAudioStats: this.client.getRemoteAudioStats(), remoteVideoStats: this.client.getRemoteVideoStats(),
        events: [...this.events], deviceCaptureCalls: window.__deviceCaptureCalls || 0,
      };
    }
  }
  window.RTCFileSession = RTCFileSession;
})();
