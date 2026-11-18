import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { chromium } from 'playwright';
import { startRuntimeServer } from './server.mjs';

async function main() {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const args = process.argv.slice(2), overrides = {};
  let configPath;
  for (let i = 0; i < args.length; i++) {
    const flag = args[i];
    if (flag === '--help') { console.log('node run.mjs [--config file] [--mode logic|live] [--media file] [--output dir] [--executable browser] [--cases name,name]'); return; }
    const value = args[++i];
    if (!value) throw new Error(`Missing value for ${flag}`);
    if (flag === '--config') configPath = value;
    else if (flag === '--mode') overrides.mode = value;
    else if (flag === '--media') overrides.mediaPath = value;
    else if (flag === '--output') overrides.outputDir = value;
    else if (flag === '--executable') overrides.browserExecutable = value;
    else if (flag === '--cases') overrides.cases = value.split(',').filter(Boolean);
    else throw new Error(`Unknown flag: ${flag}`);
  }
  const input = configPath ? JSON.parse(await fs.readFile(configPath, 'utf8')) : {};
  const config = {
    mode: 'logic', timeoutMs: 12000, holdMs: 2000, outputDir: './results', mediaPath: '', cases: [],
    ...input,
    ...(process.env.PROBE_MODE ? { mode: process.env.PROBE_MODE } : {}),
    ...(process.env.PROBE_OUTPUT ? { outputDir: process.env.PROBE_OUTPUT } : {}),
    ...(process.env.PROBE_MEDIA ? { mediaPath: process.env.PROBE_MEDIA } : {}),
    ...overrides,
  };
  const runId = new Date().toISOString().replaceAll(':', '-').replaceAll('.', '-') + '-' + process.pid;
  const output = path.resolve(config.outputDir, runId);
  await fs.mkdir(output, { recursive: true });
  const mediaPath = config.mediaPath ? path.resolve(config.mediaPath) : path.join(here, 'fixtures/probe.webm');
  const secretValues = [config.publisher?.token, config.subscriber?.token].filter(value => typeof value === 'string' && value.length > 0);
  const redact = text => secretValues.reduce((result, secret) => result.replaceAll(secret, '[REDACTED]'), String(text));
  const check = (value, message) => { if (!value) throw new Error(message); };
  const logs = [], cases = [];
  const report = {
    schemaVersion: 2, runId, startedAt: new Date().toISOString(), timezone: 'Asia/Shanghai',
    integrationSource: 'RTC customVideoSource SDK/file-stream flow, extracted into browser/integration.js',
    mode: config.mode, mediaSource: 'local-file', mediaFile: path.basename(mediaPath),
    deviceCaptureEnabled: false, remotePageRequired: false, realRTCConnection: false,
    executionCompleted: false, cases,
  };
  let browser, server, currentCase = 'setup';
  const channel = config.channel || `file_probe_${Date.now().toString(36)}`;
  const appId = config.mode === 'logic' ? '11111111111111111111111111111111' : config.appId;
  const accessPoints = config.mode === 'logic' ? { serverList: ['probe.invalid'], domain: 'probe.invalid', port: 443 } : config.accessPoints;
  const common = { appId, channel, uid: 101, token: 'logic-token', role: 'publisher' };

  async function openRuntime(actualSdk = false) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, timezoneId: 'Asia/Shanghai' });
    try {
      await context.addInitScript(() => {
        window.__deviceCaptureCalls = 0;
        if (navigator.mediaDevices) {
          navigator.mediaDevices.getUserMedia = async () => {
            window.__deviceCaptureCalls++;
            throw new Error('Camera and microphone device capture is disabled');
          };
        }
      });
      if (config.mode !== 'live') {
        await context.route('**/*', route => {
          const url = new URL(route.request().url());
          if (url.origin === new URL(server.url).origin) return route.continue();
          logs.push({ case: currentCase, type: 'blocked-external-request', url: url.origin + url.pathname });
          return route.abort();
        });
        await context.routeWebSocket('**/*', socket => socket.close());
      }
      const page = await context.newPage();
      page.setDefaultTimeout(config.timeoutMs);
      page.on('console', message => logs.push({ case: currentCase, type: message.type(), text: redact(message.text()) }));
      page.on('pageerror', error => logs.push({ case: currentCase, type: 'pageerror', text: redact(error.stack || error.message) }));
      await page.goto(server.url + (actualSdk ? '?sdk=actual' : ''), { waitUntil: 'networkidle' });
      await page.waitForFunction(() => !!window.RTCFileSession && !!window.AgoraRTC);
      await page.evaluate(options => { window.session = new RTCFileSession(options); }, { accessPoints, timeoutMs: config.timeoutMs });
      return { context, page };
    } catch (error) { await context.close(); throw error; }
  }

  async function state(page) { return page.evaluate(() => ({ ...session.snapshot(), adapter: window.__rtcProbe?.snapshot(), sdkVersion: AgoraRTC.VERSION })); }
  async function start(page, extra = {}) { return page.evaluate(options => session.start(options), { ...common, ...extra }); }
  async function stop(page) { await page.evaluate(() => session.stop()); }
  async function oneCase(definition) {
    currentCase = definition.name;
    const entry = { name: definition.name, description: definition.description, status: 'running' };
    cases.push(entry);
    const begin = Date.now();
    let context, page;
    try {
      ({ context, page } = await openRuntime(definition.actualSdk));
      await definition.run(page);
      const value = await state(page);
      check(value.deviceCaptureCalls === 0, 'A device-capture API was called');
      check(!logs.some(line => line.case === currentCase && line.type === 'pageerror'), 'Uncaught browser exception');
      entry.status = 'passed';
    } catch (error) {
      entry.status = 'failed'; entry.error = redact(error.message);
      if (page) {
        const filename = `${definition.name}.png`;
        try { await page.screenshot({ path: path.join(output, filename), fullPage: true }); entry.screenshot = filename; } catch {}
      }
    } finally {
      if (page) {
        entry.state = await state(page).catch(error => ({ snapshotError: redact(error.message) }));
        await page.evaluate(async () => { await window.session?.destroy(); window.__rtcProbe?.cleanup(); }).catch(() => {});
      }
      await context?.close();
      entry.durationMs = Date.now() - begin;
      console.log(`${entry.status === 'passed' ? 'PASS' : 'FAIL'} ${entry.name}${entry.error ? ': ' + entry.error : ''}`);
    }
  }

  const definitions = [
    { name: 'parameter-validation', description: '独立参数校验，不依赖网页设置或浏览器本地保存值', async run(page) {
      const error = await page.evaluate(async options => { try { await session.start(options); return ''; } catch (error) { return error.message; } }, { ...common, appId: '' });
      check(error.includes('appId'), 'Missing App ID was not rejected');
      check((await state(page)).adapter.calls.every(call => call.op !== 'join'), 'Invalid parameters reached join');
    } },
    { name: 'file-publish', description: '视频文件播放、captureStream、创建自定义双轨、加入及发布', async run(page) {
      const value = await start(page);
      check(value.state === 'connected' && value.localTracks.length === 2 && value.localTracks.every(track => track.readyState === 'live'), 'File tracks are not live');
      const adapter = (await state(page)).adapter;
      check(adapter.calls.some(call => call.op === 'publish' && call.kinds.includes('audio') && call.kinds.includes('video')), 'Audio/video not both published');
      check(await page.locator('#local-player video').count() === 1, 'Local preview missing');
    } },
    { name: 'subscriber-flow', description: '订阅方加入时不创建本地采集轨道', async run(page) {
      await start(page, { role: 'subscriber' });
      const value = await state(page);
      check(value.state === 'connected' && value.localTracks.length === 0 && value.capturedTracks.length === 0, 'Subscriber created local tracks');
    } },
    { name: 'remote-subscription', description: '通过文件流生成远端事件并订阅音视频、读取统计', async run(page) {
      await start(page, { role: 'subscriber' });
      await page.evaluate(() => __rtcProbe.publishRemote());
      await page.waitForFunction(() => session.snapshot().remoteUsers.some(user => user.audio && user.video));
      const value = await state(page);
      check(await page.locator('#remote-7002 video').count() === 1, 'Remote player missing');
      check(value.remoteVideoStats['7002'].receiveBitrate > 0, 'Remote statistics missing');
      await page.evaluate(() => __rtcProbe.unpublishRemote());
      check(await page.locator('#remote-7002').count() === 0, 'Unpublished video remained visible');
    } },
    { name: 'media-cleanup', description: '离开后 SDK 轨道关闭、原生轨道结束、文件暂停', async run(page) {
      await start(page); await stop(page);
      const value = await state(page);
      check(value.state === 'idle' && value.localTracks.length === 0 && value.capturedTracks.every(track => track.readyState === 'ended') && value.mediaPaused, 'Media resources leaked');
    } },
    { name: 'join-failure-cleanup', description: '加入失败时清理预先创建的文件轨道，并可再次加入', async run(page) {
      await page.evaluate(() => __rtcProbe.rejectNextJoin());
      let rejected = false; try { await start(page); } catch { rejected = true; }
      check(rejected, 'Controlled rejection did not propagate');
      let value = await state(page);
      check(value.state === 'failed' && value.localTracks.length === 0 && value.capturedTracks.every(track => track.readyState === 'ended') && value.mediaPaused, 'Failed join leaked media');
      await start(page); value = await state(page); check(value.state === 'connected', 'Session could not retry');
    } },
    { name: 'repeat-session', description: '同一集成实例重复加入/离开，事件和播放器不会累积', async run(page) {
      await start(page); await stop(page); await start(page, { role: 'subscriber' });
      await page.evaluate(() => __rtcProbe.publishRemote());
      await page.waitForFunction(() => session.snapshot().remoteUsers.some(user => user.video));
      const value = await state(page);
      check(value.adapter.listeners['user-published'] === 1 && await page.locator('#remote-7002 video').count() === 1, 'Subscription listeners or players accumulated');
    } },
    { name: 'bundled-sdk-check', description: '加载包内原版 Agora SDK，使用文件创建真实自定义音视频轨道（不入会）', actualSdk: true, async run(page) {
      await page.evaluate(() => session.prepareMedia());
      const value = await state(page);
      check(value.sdkVersion === '4.24.9' && value.localTracks.length === 2 && value.localTracks.every(track => track.readyState === 'live'), 'Original SDK failed to wrap file tracks');
      await stop(page); check((await state(page)).capturedTracks.every(track => track.readyState === 'ended'), 'Original SDK media cleanup failed');
    } },
  ];

  async function runLive() {
    currentCase = 'live-file-stream';
    const entry = { name: currentCase, description: '真实 SDK 双端入会、文件发布与远端订阅', status: 'running' };
    cases.push(entry);
    const begin = Date.now();
    let publisher, subscriber;
    try {
      subscriber = await openRuntime(true); publisher = await openRuntime(true);
      await start(subscriber.page, { appId: config.appId, role: 'subscriber', uid: config.subscriber.uid, token: config.subscriber.token || '' });
      await start(publisher.page, { appId: config.appId, role: 'publisher', uid: config.publisher.uid, token: config.publisher.token || '' });
      report.realRTCConnection = true;
      await subscriber.page.waitForFunction(uid => session.snapshot().remoteUsers.some(user => user.uid === uid && user.audio && user.video), config.publisher.uid, { timeout: config.timeoutMs });
      await subscriber.page.waitForTimeout(config.holdMs);
      await subscriber.page.waitForFunction(uid => {
        const stats = session.snapshot().remoteVideoStats[uid];
        return stats && (stats.receiveBitrate > 0 || stats.receivePackets > 0);
      }, config.publisher.uid, { timeout: config.timeoutMs });
      entry.publisher = await state(publisher.page); entry.subscriber = await state(subscriber.page);
      check(entry.publisher.deviceCaptureCalls === 0 && entry.subscriber.deviceCaptureCalls === 0, 'Device capture was attempted');
      entry.status = 'passed';
    } catch (error) {
      entry.status = 'failed'; entry.error = redact(error.message);
      for (const [name, endpoint] of [['publisher', publisher], ['subscriber', subscriber]]) {
        if (endpoint) {
          entry[name] = await state(endpoint.page).catch(() => null);
          await endpoint.page.screenshot({ path: path.join(output, `live-${name}.png`) }).catch(() => {});
        }
      }
    } finally {
      for (const endpoint of [publisher, subscriber]) if (endpoint) { await endpoint.page.evaluate(() => session.destroy()).catch(() => {}); await endpoint.context.close(); }
      entry.durationMs = Date.now() - begin;
      console.log(`${entry.status === 'passed' ? 'PASS' : 'FAIL'} ${entry.name}${entry.error ? ': ' + entry.error : ''}`);
    }
  }

  try {
    check(['logic', 'live'].includes(config.mode), 'mode must be logic or live');
    check(Number.isFinite(config.timeoutMs) && config.timeoutMs > 0, 'timeoutMs must be positive');
    if (config.mode === 'live') {
      check(/^[0-9a-f]{32}$/i.test(config.appId || ''), 'live mode requires an explicit App ID');
      check(Array.isArray(config.accessPoints?.serverList) && config.accessPoints.serverList.length && config.accessPoints.serverList.every(host => typeof host === 'string' && host.trim()) && typeof config.accessPoints.domain === 'string' && config.accessPoints.domain.trim() && Number.isInteger(config.accessPoints.port) && config.accessPoints.port >= 1 && config.accessPoints.port <= 65535, 'live mode requires explicit private access points');
      const validUid = value => Number.isInteger(value) && value >= 1 && value <= 4294967295;
      check(config.publisher && config.subscriber && validUid(config.publisher.uid) && validUid(config.subscriber.uid) && config.publisher.uid !== config.subscriber.uid, 'live publisher/subscriber require valid, distinct numeric UIDs');
      check([config.publisher.token, config.subscriber.token].every(token => token == null || typeof token === 'string'), 'live tokens must be strings');
    } else check(Array.isArray(config.cases) && config.cases.every(name => definitions.some(item => item.name === name)), 'Unknown case selection');
    report.sdkSha256 = createHash('sha256').update(await fs.readFile(path.join(here, 'sdk/AgoraRTC_N.js'))).digest('hex');
    report.mediaSha256 = createHash('sha256').update(await fs.readFile(mediaPath)).digest('hex');
    server = await startRuntimeServer({ mediaPath, mode: config.mode });
    report.runtimeURL = server.url;
    browser = await chromium.launch({
      headless: true,
      executablePath: config.browserExecutable || process.env.CHROME_PATH || undefined,
      args: ['--no-sandbox', '--autoplay-policy=no-user-gesture-required', '--disable-background-networking'],
    });
    report.browserVersion = browser.version();
    if (config.mode === 'live') await runLive();
    else for (const definition of definitions) if (!config.cases.length || config.cases.includes(definition.name)) await oneCase(definition);
    report.executionCompleted = true;
  } catch (error) { report.harnessError = redact(error.stack || error.message); console.error(report.harnessError); }
  finally {
    await browser?.close(); await server?.close();
    report.finishedAt = new Date().toISOString();
    report.summary = { total: cases.length, passed: cases.filter(item => item.status === 'passed').length, failed: cases.filter(item => item.status === 'failed').length };
    const encode = value => redact(JSON.stringify(value, null, 2));
    await fs.writeFile(path.join(output, 'report.json'), encode(report));
    await fs.writeFile(path.join(output, 'browser-log.jsonl'), logs.map(line => redact(JSON.stringify(line))).join('\n') + '\n');
    const escape = value => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;');
    const rows = cases.map(item => `<tr><td>${escape(item.description)}</td><td>${item.status}</td><td>${escape(item.error || '通过')}${item.screenshot ? `<br><a href="${item.screenshot}">截图</a>` : ''}</td></tr>`).join('');
    const html = `<!doctype html><meta charset="utf-8"><title>RTC 文件流探测</title><style>body{font:15px/1.6 system-ui;margin:32px}table{border-collapse:collapse;width:100%}td,th{border:1px solid #ccd4dd;padding:10px;text-align:left}pre{white-space:pre-wrap}</style><h1>RTC 文件流探测</h1><p>模式：${escape(config.mode)}；仅文件流，禁用摄像头/麦克风采集；无需线上网页。</p><p>执行完成：${report.executionCompleted}；通过 ${report.summary.passed}/${report.summary.total}</p><table><tr><th>用例</th><th>结果</th><th>详情</th></tr>${rows}</table><p><a href="report.json">JSON</a> · <a href="browser-log.jsonl">浏览器日志</a></p>${report.harnessError ? `<pre>${escape(report.harnessError)}</pre>` : ''}`;
    await fs.writeFile(path.join(output, 'report.html'), redact(html));
    console.log(`Report: ${output}/report.html`); console.log(JSON.stringify({ executionCompleted: report.executionCompleted, ...report.summary }));
    process.exitCode = !report.executionCompleted ? 2 : report.summary.failed ? 1 : 0;
  }
}
main().catch(error => { console.error(error.stack || error.message); process.exitCode = 2; });
