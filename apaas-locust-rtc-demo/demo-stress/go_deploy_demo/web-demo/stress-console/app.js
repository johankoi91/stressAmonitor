const apiBase = '/api/stress';
let selectedTaskId = null;
let autoRefreshTimer = null;

const els = {
  apiStatus: document.querySelector('#apiStatus'),
  form: document.querySelector('#taskForm'),
  taskType: document.querySelector('#taskType'),
  taskList: document.querySelector('#taskList'),
  refreshTasks: document.querySelector('#refreshTasks'),
  refreshLog: document.querySelector('#refreshLog'),
  downloadLogs: document.querySelector('#downloadLogs'),
  cancelTask: document.querySelector('#cancelTask'),
  logOutput: document.querySelector('#logOutput'),
  selectedTaskText: document.querySelector('#selectedTaskText'),
  locustOnly: document.querySelector('.locust-only'),
  urlsOnly: document.querySelector('.urls-only'),
};

function formToJSON(form) {
  const data = new FormData(form);
  const payload = {};
  for (const [key, value] of data.entries()) {
    const text = String(value).trim();
    if (!text) continue;
    if (['users', 'spawnRate', 'count'].includes(key)) {
      payload[key] = Number(text);
    } else {
      payload[key] = text;
    }
  }
  return payload;
}

function setStatus(ok, text) {
  els.apiStatus.textContent = text;
  els.apiStatus.classList.toggle('ok', ok);
  els.apiStatus.classList.toggle('bad', !ok);
}

async function checkHealth() {
  try {
    const res = await fetch(`${apiBase}/health`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    setStatus(true, '正常');
  } catch (err) {
    setStatus(false, '异常');
  }
}

function updateTaskTypeUI() {
  const type = els.taskType.value;
  els.locustOnly.classList.toggle('hidden', type !== 'locust');
  els.urlsOnly.classList.toggle('hidden', type !== 'gen-demo-urls');
}

async function createTask(event) {
  event.preventDefault();
  const payload = formToJSON(els.form);
  payload.type = els.taskType.value;
  if (payload.type === 'locust') {
    payload.webUI = true;
    payload.autostart = true;
  }
  delete payload.locustMode;

  try {
    const res = await fetch(`${apiBase}/tasks`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    const body = await res.json();
    if (!res.ok) {
      throw new Error(body.error || `HTTP ${res.status}`);
    }
    selectedTaskId = body.id;
    await loadTasks();
    await loadLog();
  } catch (err) {
    alert(`启动失败：${err.message}`);
  }
}

function badgeClass(status) {
  if (['success', 'failed', 'running', 'cancelled'].includes(status)) return status;
  return '';
}

function formatDate(value) {
  if (!value) return '-';
  return new Date(value).toLocaleString();
}

async function loadTasks() {
  try {
    const res = await fetch(`${apiBase}/tasks`);
    const body = await res.json();
    const list = body.tasks || [];
    list.sort((a, b) => new Date(b.startedAt) - new Date(a.startedAt));

    if (list.length === 0) {
      els.taskList.className = 'task-list empty';
      els.taskList.textContent = '暂无任务';
      return;
    }

    els.taskList.className = 'task-list';
    els.taskList.innerHTML = list.map(task => `
      <div class="task-item ${task.id === selectedTaskId ? 'active' : ''}" data-id="${task.id}">
        <div class="task-head">
          <span>${task.type}</span>
          <span class="badge ${badgeClass(task.status)}">${task.status}</span>
        </div>
        <div><strong>ID:</strong> ${task.id}</div>
        <div><strong>开始:</strong> ${formatDate(task.startedAt)}</div>
        <div><strong>结果目录:</strong> ${task.resultPath || '-'}</div>
      </div>
    `).join('');

    els.taskList.querySelectorAll('.task-item').forEach(item => {
      item.addEventListener('click', async () => {
        selectedTaskId = item.dataset.id;
        await loadTasks();
        await loadLog();
      });
    });
  } catch (err) {
    els.taskList.className = 'task-list empty';
    els.taskList.textContent = `加载任务失败：${err.message}`;
  }
}

async function loadLog() {
  if (!selectedTaskId) {
    els.logOutput.textContent = '请选择一个任务查看日志。';
    els.selectedTaskText.textContent = '尚未选择任务';
    return;
  }

  els.selectedTaskText.textContent = `当前任务：${selectedTaskId}`;
  try {
    const res = await fetch(`${apiBase}/tasks/${selectedTaskId}/log`);
    const text = await res.text();
    els.logOutput.textContent = text || '暂无日志。';
    els.logOutput.scrollTop = els.logOutput.scrollHeight;
  } catch (err) {
    els.logOutput.textContent = `加载日志失败：${err.message}`;
  }
}

async function cancelTask() {
  if (!selectedTaskId) return;
  if (!confirm(`确认取消任务 ${selectedTaskId}？`)) return;
  try {
    await fetch(`${apiBase}/tasks/${selectedTaskId}/cancel`, { method: 'POST' });
    await loadTasks();
    await loadLog();
  } catch (err) {
    alert(`取消失败：${err.message}`);
  }
}

function downloadLogs() {
  if (!selectedTaskId) {
    alert('请先选择一个任务');
    return;
  }
  window.open(`${apiBase}/tasks/${selectedTaskId}/logs.zip`, '_blank');
}

function startAutoRefresh() {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer);
  autoRefreshTimer = setInterval(async () => {
    await loadTasks();
    if (selectedTaskId) await loadLog();
  }, 5000);
}

els.taskType.addEventListener('change', updateTaskTypeUI);
els.form.addEventListener('submit', createTask);
els.refreshTasks.addEventListener('click', loadTasks);
els.refreshLog.addEventListener('click', loadLog);
els.downloadLogs.addEventListener('click', downloadLogs);
els.cancelTask.addEventListener('click', cancelTask);

checkHealth();
updateTaskTypeUI();
loadTasks();
startAutoRefresh();
