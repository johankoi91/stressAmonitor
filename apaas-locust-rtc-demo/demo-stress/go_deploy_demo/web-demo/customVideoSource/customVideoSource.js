/*
 *  These procedures use Agora Video Call SDK for Web to enable local and remote
 *  users to join and leave a Video Call channel managed by Agora Platform.
 */

/*
 *  Create an {@link https://docs.agora.io/en/Video/API%20Reference/web_ng/interfaces/iagorartcclient.html|AgoraRTCClient} instance.
 *
 *  @param {string} mode - The {@link https://docs.agora.io/en/Voice/API%20Reference/web_ng/interfaces/clientconfig.html#mode| streaming algorithm} used by Agora SDK.
 *  @param  {string} codec - The {@link https://docs.agora.io/en/Voice/API%20Reference/web_ng/interfaces/clientconfig.html#codec| client codec} used by the browser.
 */
var client = AgoraRTC.createClient({
  mode: "rtc",
  codec: "vp8"
});
AgoraRTC.enableLogUpload();





 // 获取网络质量并更新 badge
const NQ_LABEL = ["未知", "极佳", "良好", "一般", "较差", "很差", "断开"];
const NQ_CSS   = ["nq-unknown", "nq-best", "nq-good", "nq-fair", "nq-poor", "nq-bad", "nq-down"];
let statsInterval;

client.on("network-quality", quality => {
  const up   = quality.uplinkNetworkQuality;
  const down = quality.downlinkNetworkQuality;
  console.log("uplink network quality", up, "downlink network quality", down);
  $("#nq-uplink-badge").text(NQ_LABEL[up] ?? up).attr("class", `nq-badge ${NQ_CSS[up] ?? "nq-unknown"}`);
  $("#nq-downlink-badge").text(NQ_LABEL[down] ?? down).attr("class", `nq-badge ${NQ_CSS[down] ?? "nq-unknown"}`);
});

function initStats() {
  statsInterval = setInterval(flushStats, 1000);
}

function destructStats() {
  clearInterval(statsInterval);
  $("#local-stats").html("");
  $("#remote-stats-container").html("");
  $("#remote-stats-section").hide();
}

function statsItem(label, value, unit, colorFn) {
  const display = (value != null && value !== "") ? `${value}${unit ? " " + unit : ""}` : "-";
  const cls = colorFn ? colorFn(value) : "";
  return `<div class="stats-item"><span class="stats-item-label">${label}</span><span class="stats-item-value ${cls}">${display}</span></div>`;
}

function statsGroup(title) {
  return `<div class="stats-group">${title}</div>`;
}

function lossColor(v)  { return v > 5 ? "val-bad" : v > 1 ? "val-warn" : "val-good"; }
function rttColor(v)   { return v > 200 ? "val-bad" : v > 80 ? "val-warn" : "val-good"; }
function bitrateColor(v) { return v > 0 ? "val-good" : "val-warn"; }

function buildLocalGrid(a, v) {
  return `<div class="stats-grid">
    ${statsGroup("音频")}
    ${statsItem("发送码率",    a.sendBitrate,           "bps", bitrateColor)}
    ${statsItem("RTT",         a.sendRttMs,             "ms",  rttColor)}
    ${statsItem("Jitter",      a.sendJitterMs,          "ms")}
    ${statsItem("丢包率",      a.currentPacketLossRate, "%",   lossColor)}
    ${statsItem("累计包数",    a.sendPackets,           "")}
    ${statsItem("累计丢包",    a.sendPacketsLost,       "")}
    ${statsItem("编码",        a.codecType,             "")}
    ${statsItem("音量",        a.sendVolumeLevel,       "")}
    ${statsGroup("视频")}
    ${statsItem("发送码率",    v.sendBitrate,           "bps", bitrateColor)}
    ${statsItem("目标码率",    v.targetSendBitrate,     "bps")}
    ${statsItem("RTT",         v.sendRttMs,             "ms",  rttColor)}
    ${statsItem("Jitter",      v.sendJitterMs,          "ms")}
    ${statsItem("丢包率",      v.currentPacketLossRate, "%",   lossColor)}
    ${statsItem("累计包数",    v.sendPackets,           "")}
    ${statsItem("累计丢包",    v.sendPacketsLost,       "")}
    ${statsItem("采集分辨率",  `${v.captureResolutionWidth}x${v.captureResolutionHeight}`, "")}
    ${statsItem("发送分辨率",  `${v.sendResolutionWidth}x${v.sendResolutionHeight}`,       "")}
    ${statsItem("采集帧率",    v.captureFrameRate,      "fps")}
    ${statsItem("发送帧率",    v.sendFrameRate,         "fps")}
    ${statsItem("编码延迟",    v.encodeDelay != null ? Number(v.encodeDelay).toFixed(1) : null, "ms")}
    ${statsItem("编码",        v.codecType,             "")}
    ${statsItem("累计时长",    v.totalDuration,         "s")}
    ${statsItem("卡顿时长",    v.totalFreezeTime,       "s")}
  </div>`;
}

function buildRemoteGrid(ra, rv) {
  return `<div class="stats-grid">
    ${statsGroup("音频")}
    ${statsItem("端到端延迟",  Number(ra.receiveDelay).toFixed(1),       "ms",  rttColor)}
    ${statsItem("接收码率",    ra.receiveBitrate,                        "bps", bitrateColor)}
    ${statsItem("丢包率",      Number(ra.packetLossRate).toFixed(2),     "%",   lossColor)}
    ${statsItem("累计接收包",  ra.receivePackets,                        "")}
    ${statsItem("累计丢包",    ra.receivePacketsLost,                    "")}
    ${statsGroup("视频")}
    ${statsItem("端到端延迟",  Number(rv.receiveDelay).toFixed(1),       "ms",  rttColor)}
    ${statsItem("接收码率",    rv.receiveBitrate,                        "bps", bitrateColor)}
    ${statsItem("接收分辨率",  `${rv.receiveResolutionWidth}x${rv.receiveResolutionHeight}`, "")}
    ${statsItem("丢包率",      Number(rv.receivePacketsLost).toFixed(2), "%",   lossColor)}
    ${statsItem("卡顿率",      Number(rv.freezeRate).toFixed(3),        "%",   lossColor)}
    ${statsItem("累计接收包",  rv.receivePackets,                        "")}
    ${statsItem("累计丢包",    rv.receivePacketsLost,                    "")}
    ${statsItem("累计时长",    rv.totalDuration,                         "s")}
    ${statsItem("卡顿时长",    rv.totalFreezeTime,                       "s")}
  </div>`;
}

function flushStats() {
  const a = client.getLocalAudioStats();
  const v = client.getLocalVideoStats();
  $("#local-stats").html(buildLocalGrid(a, v));

  const uids = Object.keys(remoteUsers);
  if (uids.length === 0) {
    $("#remote-stats-section").hide();
    return;
  }
  $("#remote-stats-section").show();
  const remoteAudio = client.getRemoteAudioStats();
  const remoteVideo = client.getRemoteVideoStats();
  const html = uids.map(uid => {
    const ra = remoteAudio[uid];
    const rv = remoteVideo[uid];
    if (!ra || !rv) return "";
    return `<div class="stats-remote-uid">uid: ${uid}</div>${buildRemoteGrid(ra, rv)}`;
  }).join("");
  $("#remote-stats-container").html(html);
}

/*
 *  Clear the video and audio tracks used by `client` on initiation.
 */
var localTracks = {
  videoTrack: null,
  audioTrack: null
};

/*
 *  On initiation no users are connected.
 */
var remoteUsers = {};

/*
 *  On initiation. `client` is not attached to any project or channel for any specific user.
 */
var options = {
  appid: null,
  channel: null,
  uid: null,
  role: "audience",
  token: null
};
var currentStream = null;

/*
 * When this page is called with parameters in the URL, this procedure
 * attempts to join a Video Call channel using those parameters.
 */
$(() => {
  var urlParams = new URL(location.href).searchParams;
  options.appid = urlParams.get("appid");
  options.channel = urlParams.get("channel");
  options.token = urlParams.get("token");
  options.uid = urlParams.get("uid");
  currentStream = urlParams.get("stream-source");

  if (options.appid && options.channel) {
    $("#uid").val(options.uid);
    $("#appid").val(options.appid);
    $("#token").val(options.token);
    $("#channel").val(options.channel);
    $("#join-form").submit();
  }
});

/*
 * When a user clicks Join or Leave in the HTML form, this procedure gathers the information
 * entered in the form and calls join asynchronously. The UI is updated to match the options entered
 * by the user.
 */
$("#join-form").submit(async function (e) {
  e.preventDefault();
  $("#join").attr("disabled", true);
  $("#audience-join").attr("disabled", true);

  try {
    currentStream = $("#stream-source").val();
    options.channel = $("#channel").val();
    options.uid = Number($("#uid").val());
    options.appid = $("#appid").val();
    options.token = $("#token").val();
    await join();
    showToast(`已加入频道 ${options.channel}（uid: ${options.uid}）`);
  } catch (error) {
    console.error(error);
    showToast("加入失败：" + (error.message || error));
  } finally {
    $("#leave").attr("disabled", false);
  }
});

$("#leave").click(function (e) {
  leave();
});

$("#join").click(function (e) {
  options.role = "host";
  $("#join-form").submit();
});

$("#audience-join").click(function (e) {
  options.role = "audience";
  $("#join-form").submit();
});


/*
 * Join a channel, then create local video and audio tracks and publish them to the channel.
 */
async function join() {
  // Add an event listener to play remote tracks when remote user publishes.
  client.on("user-published", handleUserPublished);
  client.on("user-unpublished", handleUserUnpublished);

  const apServer = $("#ap-server-list").val().trim();
  const apDomain = $("#ap-domain").val().trim();
  const apPort   = parseInt($("#ap-port").val(), 10);
  if (apServer && apDomain && apPort) {
    client.setLocalAccessPointsV2({
      accessPoints: {
        serverList: [apServer],
        domain: apDomain,
        port: apPort
      }
    });
  }

  if (options.role === "host") {  
    if (currentStream == "camera") {
      localTracks.audioTrack = await AgoraRTC.createMicrophoneAudioTrack({encoderConfig: "music_standard"});
      localTracks.videoTrack = await AgoraRTC.createCameraVideoTrack();
      $("#local-slot").show();
    } else {
      var videoFromDiv = document.getElementById("sample-video");
      // https://developers.google.com/web/updates/2016/10/capture-stream - captureStream() 
      // can only be called after the video element is able to play video;
      try {
        videoFromDiv.play();
      } catch (e) {
        console.log(error);
      }
      //specify mozCaptureStream for Firefox.
      var captureStream = navigator.userAgent.indexOf("Firefox") > -1 ? videoFromDiv.mozCaptureStream() : videoFromDiv.captureStream();
      localTracks.audioTrack = await AgoraRTC.createCustomAudioTrack({mediaStreamTrack: captureStream.getAudioTracks()[0]});
      localTracks.videoTrack = await AgoraRTC.createCustomVideoTrack({mediaStreamTrack: captureStream.getVideoTracks()[0]});
    }
  }

  options.uid = await client.join(options.appid, options.channel, options.token || null, options.uid || null);

  if (options.role === "host") {
    await client.publish(Object.values(localTracks));
    localTracks.videoTrack.play("local-player");
    $("#local-player-name").text(`本地 · uid ${options.uid}`);
    $("#local-empty").hide();
  }
  $("#network-quality-panel").show();
  initStats();
}

/*
 * Stop all local and remote tracks then leave the channel.
 */
async function stopCurrentChannel() {
  for (trackName in localTracks) {
    var track = localTracks[trackName];
    if (track) {
      track.stop();
      track.close();
      localTracks[trackName] = undefined;
    }
  }

  // Remove remote users and player views.
  remoteUsers = {};
  $("#remote-playerlist").html("");
  $("#local-player-name").text("");

  // leave the channel
  await client.leave();
  console.log("client leaves channel success");
}


async function leave() {
  await stopCurrentChannel();
  destroyVideoElementJQ();
  destructStats();
  $("#network-quality-panel").hide();
  $("#nq-uplink-badge, #nq-downlink-badge").text("-").attr("class", "nq-badge nq-unknown");
  $("#local-player-name").text("");
  $("#local-empty").show();
  $("#join").attr("disabled", false);
  $("#leave").attr("disabled", true);
  $("#audience-join").attr("disabled", false);
  showToast("已离开频道");
}


function destroyVideoElementJQ() {
  const $video = $('#local-video video');
  if ($video.length === 0) return;
  const video = $video[0]; // 原生 DOM 元素
  video.pause();
  // 停止 captureStream
  try {
    const stream = video.captureStream?.();
    stream?.getTracks().forEach(track => track.stop());
  } catch (e) {
    console.warn("captureStream not supported or already stopped");
  }
}


async function subscribe(user, mediaType) {
  const uid = user.uid;
  // subscribe to a remote user
  await client.subscribe(user, mediaType);
  console.log("subscribe success");
  if (mediaType === 'video') {
    const player = $(`
      <div id="player-wrapper-${uid}">
        <p class="player-name">remoteUser(${uid})</p>
        <div id="player-${uid}" class="player"></div>
      </div>
    `);
    $("#remote-playerlist").append(player);
    user.videoTrack.play(`player-${uid}`);
  }
  if (mediaType === 'audio') {
    user.audioTrack.play();
  }
}

/*
 * Add a user who has subscribed to the live channel to the local interface.
 *
 * @param  {IAgoraRTCRemoteUser} user - The {@link  https://docs.agora.io/en/Voice/API%20Reference/web_ng/interfaces/iagorartcremoteuser.html| remote user} to add.
 * @param {trackMediaType - The {@link https://docs.agora.io/en/Voice/API%20Reference/web_ng/interfaces/itrack.html#trackmediatype | media type} to add.
 */
function handleUserPublished(user, mediaType) {
  const id = user.uid;
  remoteUsers[id] = user;
  subscribe(user, mediaType);
}

/*
 * Remove the user specified from the channel in the local interface.
 *
 * @param  {string} user - The {@link  https://docs.agora.io/en/Voice/API%20Reference/web_ng/interfaces/iagorartcremoteuser.html| remote user} to remove.
 */
function handleUserUnpublished(user, mediaType) {
  if (mediaType === 'video') {
    const id = user.uid;
    delete remoteUsers[id];
    $(`#player-wrapper-${id}`).remove();
  }
}

// 折叠/展开
$("#stats-toggle").on("click", function () {
  const $body = $("#stats-body");
  const collapsed = $body.is(":hidden");
  $body.toggle();
  $(this).text(collapsed ? "－" : "＋");
});

// 拖拽
(function () {
  const panel = document.getElementById("network-quality-panel");
  const handle = document.getElementById("stats-drag-handle");
  if (!panel || !handle) return;

  let dragging = false, ox = 0, oy = 0;

  handle.addEventListener("mousedown", e => {
    dragging = true;
    ox = e.clientX - panel.getBoundingClientRect().left;
    oy = e.clientY - panel.getBoundingClientRect().top;
    panel.style.transition = "none";
    e.preventDefault();
  });

  document.addEventListener("mousemove", e => {
    if (!dragging) return;
    const x = Math.max(0, Math.min(e.clientX - ox, window.innerWidth  - panel.offsetWidth));
    const y = Math.max(0, Math.min(e.clientY - oy, window.innerHeight - panel.offsetHeight));
    panel.style.left  = x + "px";
    panel.style.top   = y + "px";
    panel.style.right = "auto";
  });

  document.addEventListener("mouseup", () => { dragging = false; });
})();

let _toastTimer;
function showToast(msg, duration = 3000) {
  const $t = $("#join-toast");
  $("#join-toast-msg").text(msg);
  $t.fadeIn(150);
  clearTimeout(_toastTimer);
  _toastTimer = setTimeout(() => $t.fadeOut(300), duration);
}