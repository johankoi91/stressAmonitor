// Shared catalogue and navigation on every screen size.
(() => {
  const zh = getLanguage().startsWith('zh');
  const t = (cn, en) => zh ? cn : en;
  const data = {
    videoAndVoiceCalling: ['音视频通话', 'Video & voice', '加入频道，开启第一次实时通话。', 'Start your first real-time call.', 'video'],
    basicVoiceCall: ['基础语音通话', 'Voice calling', '创建、发布和订阅音频流。', 'Create, publish and subscribe to audio.', 'mic'],
    basicVideoCall: ['基础视频通话', 'Video calling', '逐步了解视频通话的完整流程。', 'Explore the video call workflow.', 'video'],
    basicLive: ['基础视频直播', 'Live streaming', '体验主播和观众两种角色。', 'Try host and audience roles.', 'broadcast'],
    shareTheScreen: ['屏幕共享', 'Screen sharing', '将屏幕或窗口分享给频道成员。', 'Share a screen or window.', 'screen'],
    testMediaDevices: ['音视频设备检测', 'Device check', '检查摄像头、麦克风和扬声器。', 'Check your camera, mic and speakers.', 'sliders'],
    adjustVideoProfile: ['视频编码属性', 'Video profiles', '调整分辨率、帧率与码率。', 'Adjust resolution, frame rate and bitrate.', 'sliders'],
    displayCallStats: ['通话中质量检测', 'Call statistics', '观察网络状态与音视频质量。', 'Inspect network and media quality.', 'chart'],
    audioEffects: ['音频音效', 'Audio effects', '播放音效，体验混音控制。', 'Play effects and control audio mixing.', 'mic'],
    joinMultipleChannel: ['加入多频道', 'Multiple channels', '在同一页面管理多个频道。', 'Manage more than one channel.', 'grid'],
    customVideoSource: ['自定义视频源', 'Custom video', '使用自定义视频轨道进行推流。', 'Publish a custom video track.', 'video'],
    selfRendering: ['视频自渲染', 'Custom rendering', '控制视频帧的显示方式。', 'Control how frames are displayed.', 'screen'],
    selfCapturing: ['视频自采集', 'Custom capture', '将自采集视频送入 SDK。', 'Feed your own video into the SDK.', 'video'],
    screenshot: ['视频截图', 'Video snapshot', '从视频轨道获取一帧画面。', 'Capture a frame from a video track.', 'screen'],
    geoFencing: ['区域访问限制', 'Geofencing', '设置连接服务的地域范围。', 'Choose service connection regions.', 'globe'],
    virtualBackground: ['虚拟背景', 'Virtual background', '体验背景替换和背景虚化。', 'Replace or blur your background.', 'spark'],
    beauty: ['美颜', 'Beauty effects', '调整实时视频的美颜效果。', 'Adjust real-time beauty effects.', 'spark'],
    aiDenoiser: ['AI 降噪', 'AI denoising', '减少通话中的环境噪声。', 'Reduce background noise in calls.', 'mic'],
    superClarity: ['AI 画质', 'Super clarity', '体验视频画质增强。', 'Explore enhanced video clarity.', 'spark'],
    spatialAudio: ['空间音频', 'Spatial audio', '感受声音的方向与距离。', 'Experience sound direction and distance.', 'mic'],
    vad: ['语音活动检测', 'Voice activity', '检测音频中的说话状态。', 'Detect speech activity in audio.', 'chart'],
    videoCompositor: ['本地合图', 'Video compositor', '将多路画面合成为一路视频。', 'Compose multiple tracks into one.', 'grid'],
    rtmDemo: ['RTM 实时消息', 'RTM messaging', '测试实时消息、频道与在线状态。', 'Explore real-time messages, channels and presence.', 'chat'],
    dualStream: ['大小流', 'Dual stream', '在不同清晰度的视频流间切换。', 'Switch between stream qualities.', 'sliders'],
  };
  const groupNames = {
    quickMenu:t('快速开始','Quick start'), basicMenu:t('基础通话','Basics'),
    advancedMenu:t('进阶功能','Advanced'), extensionMenu:t('插件','Extensions'),
    othersMenu:t('其他','More'),
  };
  const paths = {
    chat:'<path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-3 2V11.5A8.5 8.5 0 0 1 9.5 3h3a8.5 8.5 0 0 1 8.5 8.5Z"/><path d="M7 10h8M7 14h5"/>',
    video:'<rect x="3" y="5" width="12" height="14" rx="3"/><path d="m15 10 6-4v12l-6-4"/>',
    mic:'<rect x="9" y="2" width="6" height="13" rx="3"/><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8"/>',
    screen:'<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>',
    sliders:'<path d="M6 3v18M12 3v18M18 3v18M3 8h6M9 16h6M15 7h6"/>',
    broadcast:'<circle cx="12" cy="12" r="2"/><path d="M8 7a7 7 0 0 0 0 10M16 7a7 7 0 0 1 0 10M5 3a12 12 0 0 0 0 18M19 3a12 12 0 0 1 0 18"/>',
    chart:'<path d="M3 3v18h18M7 15l4-5 4 3 5-7"/>',
    grid:'<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
    globe:'<circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18"/>',
    spark:'<path d="m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5Z"/>',
  };
  const icon = name => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] || paths.grid}</svg>`;
  const groups = DEMO_CATEGORIES.map(category => ({
    ...MENU_LIST.find(group => group.name === category.name), url: category.url,
  }));
  const items = groups.flatMap(g => g.data.map(item => ({...item, group:g.name})));
  const category = getDemoCategory() || DEMO_CATEGORIES[0];
  const currentGroup = groups.find(group => group.name === category.name);
  const isHome = isCataloguePage();
  if (isHome) document.title = `${groupNames[currentGroup.name]} · ${t('RTC 功能示例','RTC Examples')}`;
  const host = document.querySelector('.container > .left');
  if (!host) return;
  document.documentElement.lang = zh ? 'zh-CN' : 'en';
  document.body.classList.add('rtc-unified', isHome ? 'rtc-home' : 'rtc-example');
  if (!isHome) {
    host.innerHTML = `<header class="rtc-header">
    <a class="rtc-brand" href="${currentGroup.url}"><img src="${ORIGIN_URL}/assets/agora-logo-en.png" alt="Agora"><span>Web SDK <b>LAB</b></span></a>
    <nav aria-label="${t('主导航','Main navigation')}">
      <a class="rtc-home-link" href="${currentGroup.url}">← ${groupNames[currentGroup.name]}</a>
      <span id="language-switch-wrapper"></span>
      <a class="rtc-settings-button" aria-label="${t('设置','Settings')}" href="${getDemoSettingsURL()}">${icon('sliders')}<span>${t('设置','Settings')}</span></a>
    </nav></header>`;
  host.querySelector('.rtc-settings-button').addEventListener('click', () => {
    __setJumpBackUrl(location.href);
  });
    const current=items.find(i=>normalizeDemoPath(new URL(i.url).pathname)===normalizeDemoPath(location.pathname));
    const meta=current && data[current.name];
    const heading=document.createElement('div'); heading.className='rtc-example-heading';
    heading.innerHTML=`<p class="rtc-eyebrow">${current ? groupNames[current.group] : 'WEB SDK'}</p><h1>${escapeHTML(meta ? meta[zh ? 0 : 1] : document.title.split('--')[0])}</h1><p>${escapeHTML(meta ? meta[zh ? 2 : 3] : '')}</p>`;
    document.querySelector('.right').before(heading);
    const appInfo=document.getElementById('app-info');
    if(appInfo && appInfo.childNodes.length) {
      const details=document.createElement('details'); details.className='rtc-reference';
      const summary=document.createElement('summary');summary.textContent=t('项目与参考资料','Project & references');details.append(summary);
      while(appInfo.firstChild) details.append(appInfo.firstChild);
      appInfo.append(details);
    }
    return;
  }
  host.hidden = true;
  const content=document.querySelector('.container > .right');
  const dialog=document.createElement('dialog'); dialog.id='rtc-settings';dialog.className='rtc-dialog';dialog.setAttribute('aria-labelledby','rtc-settings-title');
  dialog.innerHTML=`<div class="rtc-dialog-heading"><div><p class="rtc-eyebrow">PROJECT SETTINGS</p><h2 id="rtc-settings-title">${t('项目设置','Project settings')}</h2></div><button type="button" class="rtc-close" aria-label="${t('关闭设置','Close settings')}">×</button></div><p class="rtc-dialog-intro">${t('配置一次，在所有示例中使用。','Configure once for all examples.')}</p><div class="rtc-settings-form"></div>`;
  const formHost=dialog.querySelector('.rtc-settings-form');
  while(content.firstChild) formHost.append(content.firstChild);
  document.body.append(dialog);
  dialog.querySelector('.rtc-close').addEventListener('click',()=>dialog.close());
  dialog.addEventListener('close',()=>sessionStorage.removeItem(JUMP_BACK_URL_KEY));
  window.openDemoSettings=()=>{if(!dialog.open) dialog.showModal();};
  window.closeDemoSettings=()=>dialog.close();
  content.innerHTML=`<div class="rtc-catalogue-toolbar"><h1>${t('功能示例','Explore examples')} <span>${currentGroup.data.length}</span></h1><div class="rtc-catalogue-actions"><span id="language-switch-wrapper"></span><button type="button" class="rtc-settings-button" aria-label="${t('设置','Settings')}">${icon('sliders')}<span>${t('设置','Settings')}</span></button></div></div>
    <nav class="rtc-filters" aria-label="${t('功能分类','Categories')}">${groups.map(group=>`<a href="${group.url}" data-group="${group.name}" class="${group.name===currentGroup.name?'active':''}" ${group.name===currentGroup.name?'aria-current="page"':''}>${groupNames[group.name]}</a>`).join('')}</nav>
    <p class="rtc-result-count" role="status" aria-live="polite"></p><div class="rtc-grid"></div>
    <footer class="rtc-footer"><span>Agora Web SDK · ${t('官方功能示例','Official examples')}</span><a href="https://github.com/AgoraIO/API-Examples-Web" target="_blank" rel="noreferrer">GitHub ↗</a></footer>`;

  content.querySelector('.rtc-settings-button').addEventListener('click',()=>window.openDemoSettings());
  const grid=content.querySelector('.rtc-grid');
  currentGroup.data.forEach(item=>{
    const meta=data[item.name] || [item.name,item.name,'','', 'grid'];
    const card=document.createElement('a'); card.className='rtc-demo-card';card.href=item.url;
    card.dataset.group=currentGroup.name;
    card.innerHTML=`<div class="rtc-card-top"><span class="rtc-card-icon">${icon(meta[4])}</span><span class="rtc-card-category">${groupNames[currentGroup.name]}</span></div><h3>${escapeHTML(meta[zh?0:1])}</h3><p>${escapeHTML(meta[zh?2:3])}</p><div class="rtc-card-bottom"><span>${item.name==='shareTheScreen'?t('需浏览器支持屏幕采集','Requires screen capture support'):t('打开示例','Open example')}</span><span aria-hidden="true">↗</span></div>`;
    card.addEventListener('click',e=>{if(item.requiresProjectSettings!==false&&!getOptionsFromLocal().appid){e.preventDefault();__setJumpBackUrl(item.url);window.openDemoSettings();}});
    grid.append(card);
  });
  content.querySelector('.rtc-result-count').textContent=t(`显示 ${currentGroup.data.length} 个示例`,`Showing ${currentGroup.data.length} examples`);
  if(location.hash==='#settings') window.openDemoSettings();
})();
