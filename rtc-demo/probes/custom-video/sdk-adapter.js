// Controlled RTC responses; media tracks are genuine browser MediaStreamTracks.
(() => {
  const calls = [];
  const tracks = [];
  const clients = [];
  let failNextJoin = false;
  let serial = 0;
  const record = (op, data = {}) => calls.push({ op, ...data });

  function wrapTrack(raw, source) {
    if (!(raw instanceof MediaStreamTrack)) throw new Error(`${source}: media track is not ready`);
    const id = ++serial;
    const elements = [];
    const wrapper = {
      id, raw, source,
      getMediaStreamTrack: () => raw,
      getTrackLabel: () => raw.label,
      getVolumeLevel: () => 0.5,
      play(target) {
        record('play', { id, kind: raw.kind, target: target || null });
        const element = document.createElement(raw.kind === 'audio' ? 'audio' : 'video');
        element.autoplay = true;
        element.muted = true;
        element.playsInline = true;
        element.srcObject = new MediaStream([raw]);
        if (target) document.getElementById(target)?.append(element);
        else { element.hidden = true; document.body.append(element); }
        elements.push(element);
        element.play().catch(error => record('play-error', { id, message: error.message }));
      },
      stop() {
        record('stop', { id });
        raw.stop();
        for (const element of elements) { element.pause(); element.srcObject = null; element.remove(); }
      },
      close() { record('close', { id }); raw.stop(); },
    };
    tracks.push(wrapper);
    record('track', { id, kind: raw.kind, source });
    return wrapper;
  }

  const audioStats = () => ({ sendBitrate: 32000, sendRttMs: 20, sendJitterMs: 2, currentPacketLossRate: 0, sendPackets: 100, sendPacketsLost: 0, codecType: 'opus', sendVolumeLevel: 0.5 });
  const videoStats = () => ({ sendBitrate: 480000, targetSendBitrate: 500000, sendRttMs: 20, sendJitterMs: 2, currentPacketLossRate: 0, sendPackets: 100, sendPacketsLost: 0, captureResolutionWidth: 320, captureResolutionHeight: 180, sendResolutionWidth: 320, sendResolutionHeight: 180, captureFrameRate: 12, sendFrameRate: 12, encodeDelay: 2, codecType: 'VP8', totalDuration: 2, totalFreezeTime: 0 });

  function createClient() {
    const handlers = new Map();
    const remote = new Map();
    const client = {
      connectionState: 'DISCONNECTED',
      on(event, handler) {
        if (!handlers.has(event)) handlers.set(event, []);
        const list = handlers.get(event);
        if (!list.includes(handler)) list.push(handler);
      },
      off(event, handler) { handlers.set(event, (handlers.get(event) || []).filter(item => item !== handler)); },
      async emit(event, ...args) {
        await Promise.all((handlers.get(event) || []).map(handler => handler(...args)));
      },
      setLocalAccessPointsV2(config) { record('access-points', { config }); },
      async join(appId, channel, token, uid) {
        record('join', { appId, channel, tokenPresent: !!token, uid });
        if (failNextJoin) { failNextJoin = false; throw new Error('PROBE_JOIN_REJECTED'); }
        client.connectionState = 'CONNECTED';
        return uid || 101;
      },
      async publish(published) {
        if (published.length !== 2 || published.some(track => track.raw.readyState !== 'live')) throw new Error('publish requires two live media tracks');
        record('publish', { trackIds: published.map(track => track.id), kinds: published.map(track => track.raw.kind) });
      },
      async leave() { record('leave'); client.connectionState = 'DISCONNECTED'; remote.clear(); },
      async subscribe(user, mediaType) { record('subscribe', { uid: user.uid, mediaType }); },
      getLocalAudioStats: audioStats,
      getLocalVideoStats: videoStats,
      getRemoteAudioStats: () => Object.fromEntries([...remote.keys()].map(uid => [uid, { receiveDelay: 30, receiveBitrate: 24000, packetLossRate: 0, receivePackets: 100, receivePacketsLost: 0 }])),
      getRemoteVideoStats: () => Object.fromEntries([...remote.keys()].map(uid => [uid, { receiveDelay: 30, receiveBitrate: 320000, receiveResolutionWidth: 320, receiveResolutionHeight: 180, receivePackets: 100, receivePacketsLost: 0, totalDuration: 2, totalFreezeTime: 0, freezeRate: 0 }])),
      handlers, remote,
    };
    clients.push(client);
    return client;
  }

  window.AgoraRTC = {
    VERSION: 'logic-probe-adapter',
    createClient,
    enableLogUpload() {},
    async createCustomAudioTrack({ mediaStreamTrack }) { return wrapTrack(mediaStreamTrack, 'video-element'); },
    async createCustomVideoTrack({ mediaStreamTrack }) { return wrapTrack(mediaStreamTrack, 'video-element'); },
  };

  window.__rtcProbe = {
    rejectNextJoin() { failNextJoin = true; },
    snapshot() {
      return {
        calls: JSON.parse(JSON.stringify(calls)),
        connectionState: clients[0]?.connectionState,
        listeners: Object.fromEntries([...(clients[0]?.handlers || [])].map(([name, list]) => [name, list.length])),
        tracks: tracks.map(track => ({ id: track.id, kind: track.raw.kind, source: track.source, readyState: track.raw.readyState })),
      };
    },
    async emitQuality() { await clients[0].emit('network-quality', { uplinkNetworkQuality: 1, downlinkNetworkQuality: 3 }); },
    async publishRemote(uid = 7002) {
      const stream = await RTCFileMedia.capture(document.querySelector('#remote-source'), '/media/input');
      const user = { uid, audioTrack: wrapTrack(stream.getAudioTracks()[0], 'remote-video-file'), videoTrack: wrapTrack(stream.getVideoTracks()[0], 'remote-video-file') };
      clients[0].remote.set(uid, user);
      await clients[0].emit('user-published', user, 'audio');
      await clients[0].emit('user-published', user, 'video');
      return uid;
    },
    async unpublishRemote(uid = 7002) {
      const user = clients[0].remote.get(uid);
      await clients[0].emit('user-unpublished', user, 'video');
      clients[0].remote.delete(uid);
      user.audioTrack.close(); user.videoTrack.close();
      document.querySelector('#remote-source').pause();
    },
    cleanup() { for (const track of tracks) track.raw.stop(); document.querySelector('#remote-source').pause(); },
  };
})();
