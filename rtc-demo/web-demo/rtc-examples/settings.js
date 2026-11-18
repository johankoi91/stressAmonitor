// Settings stay in the existing local options object used by all SDK examples.
$(() => {
  const zh=getLanguage().startsWith('zh');
  const t=(a,b)=>zh?a:b;
  const options=getOptionsFromLocal();
  $('#project-id-select').append('<option value="custom_settings">Custom</option>');
  $('#appid').val(options.appid || '88dc432b1fe248039879fb84844c497b');
  $('#certificate').val(options.certificate || '');
  $('#certificate').attr('type','password');
  $('.proxy-list').append('<option value="0">Off</option><option value="3">UDP</option><option value="5">TCP / TLS</option>').val(options.proxyMode || '0');
  const privateOptions=options.privateConnection || {};
  const privateSection=document.createElement('section');
  privateSection.className='mt-4';
  privateSection.innerHTML=`
    <div class="form-check form-switch">
      <input class="form-check-input" type="checkbox" id="private-enabled">
      <label class="form-check-label" for="private-enabled">${t('启用私有化连接','Enable private connection')}</label>
    </div>
    <div id="private-fields" class="mt-2">
      <label class="form-label" for="private-servers">${t('接入服务器（多个用逗号分隔）','Access servers (comma-separated)')}</label>
      <input class="form-control" id="private-servers" type="text">
      <label class="form-label mt-2" for="private-domain">${t('接入域名','Access domain')}</label>
      <input class="form-control" id="private-domain" type="text">
      <label class="form-label mt-2" for="private-access-port">${t('接入端口','Access port')}</label>
      <input class="form-control" id="private-access-port" type="number" min="1" max="65535" step="1">
      <label class="form-label mt-2" for="private-report">${t('上报服务器（多个用逗号分隔）','Report servers (comma-separated)')}</label>
      <input class="form-control" id="private-report" type="text">
      <label class="form-label mt-2" for="private-port">${t('上报端口','Report port')}</label>
      <input class="form-control" id="private-port" type="number" min="1" max="65535" step="1">
    </div>
    <p class="tips">${t('保存后，对新打开或重新加载的示例生效。','After saving, reopen or reload examples to apply.')}</p>`;
  document.getElementById('setup-btn').parentElement.before(privateSection);
  $('#private-enabled').prop('checked',!!privateOptions.enabled);
  $('#private-servers').val((privateOptions.serverList || ['ap.edge.rtcdevelopers.com']).join(', '));
  $('#private-domain').val(privateOptions.domain || 'edge.rtcdevelopers.com');
  $('#private-report').val((privateOptions.hostname || ['ap.edge.rtnsvc.com']).join(', '));
  $('#private-access-port').val(privateOptions.accessPort ?? 443);
  $('#private-port').val(privateOptions.port || 6443);
  function togglePrivateFields(){
    const enabled=$('#private-enabled').prop('checked');
    $('#private-fields').prop('hidden',!enabled).find('input').prop('disabled',!enabled);
  }
  $('#private-enabled').on('change',togglePrivateFields);
  togglePrivateFields();
  $('#setup-btn').text(t('保存设置','Save settings')).removeClass('i18n');
  const error=document.createElement('p');error.className='rtc-settings-error';error.setAttribute('role','alert');
  document.getElementById('setup-btn').before(error);
  function save(){
    const appid=$('#appid').val().trim();
    if(!/^[a-fA-F0-9]{32}$/.test(appid)) {
      error.textContent=t('请填写 32 位十六进制 App ID。','Enter a 32-character hexadecimal App ID.');
      document.getElementById('appid').focus();return;
    }
    const splitHosts=value=>value.trim().split(/[,，\s]+/).filter(Boolean);
    const privateConnection={
      enabled:$('#private-enabled').prop('checked'),
      serverList:splitHosts($('#private-servers').val()),
      domain:$('#private-domain').val().trim(),
      accessPort:Number($('#private-access-port').val()),
      hostname:splitHosts($('#private-report').val()),
      port:Number($('#private-port').val()),
    };
    const validHost=value=>value.length<=253 && value.split('.').every(label=>/^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(label));
    if(privateConnection.enabled && (!privateConnection.serverList.length || !privateConnection.hostname.length ||
      ![...privateConnection.serverList,...privateConnection.hostname,privateConnection.domain].every(validHost) ||
      ![privateConnection.accessPort,privateConnection.port].every(port=>Number.isInteger(port) && port>=1 && port<=65535))) {
      error.textContent=t('请填写有效的服务器和域名（不含协议、路径或端口），接入和上报端口须为 1–65535 的整数。','Enter valid hosts and domain without scheme, path or port; access and report ports must be an integer from 1 to 65535.');
      return;
    }
    error.textContent='';
    setOptionsToLocal({appid,certificate:$('#certificate').val().trim(),proxyMode:$('.proxy-list').val(),privateConnection});
    const target=sessionStorage.getItem(JUMP_BACK_URL_KEY);
    window.closeDemoSettings();
    sessionStorage.removeItem(JUMP_BACK_URL_KEY);
    if(target){
      const url=new URL(target,location.href);
      const basePath=new URL(ORIGIN_URL).pathname.replace(/\/$/,'')+'/';
      if(url.origin===location.origin && url.pathname.startsWith(basePath) && url.pathname!==new URL(SETUP_PAGE_URL).pathname) location.href=url.href;
    }
  }
  $('#setup-btn').on('click',save);
  $('form[name=form]').on('submit',e=>{e.preventDefault();save();});
  window.addEventListener('hashchange',()=>{if(location.hash==='#settings') window.openDemoSettings();});
  window.addEventListener('load',()=>{
    if(typeof AgoraRTC!=='undefined') $('#version-text').text('Web SDK '+AgoraRTC.VERSION);
  });
  // Translated tips are inserted asynchronously by the upstream i18n module.
  const observer=new MutationObserver(()=>{
    $('#appid-link').attr('href',appIdLink);
    $('#certificate-link').attr('href',appCertificateLink);
    $('#proxy-link').attr('href',proxyLink);
  });
  observer.observe(document.querySelector('.rtc-settings-form'),{childList:true,subtree:true});
});
