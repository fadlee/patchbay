// patchbay — Vanilla JS realtime dashboard & traffic visualizer

(function () {
  'use strict';

  // State
  let rules = [];
  let trafficLogs = [];
  let sseConnected = false;
  let chartPoints = [];
  let autoScroll = true;
  let selectedRuleFilter = '';
  let loggingEnabled = true;

  // DOM Elements
  const rulesListEl = document.getElementById('rules-list');
  const summaryTextEl = document.getElementById('summary-text');
  const sseStatusEl = document.getElementById('sse-status');
  const logsTableBodyEl = document.getElementById('logs-tbody');
  const ruleFilterSelect = document.getElementById('log-rule-filter');
  const autoScrollCheckbox = document.getElementById('auto-scroll-toggle');
  const clearLogsBtn = document.getElementById('clear-logs-btn');
  const toggleLoggingBtn = document.getElementById('toggle-logging-btn');
  const addRuleForm = document.getElementById('add-rule-form');
  const canvas = document.getElementById('traffic-chart');
  const ctx = canvas ? canvas.getContext('2d') : null;
  // Format helpers
  function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  function formatTime(isoString) {
    if (!isoString) return '';
    const d = new Date(isoString);
    return d.toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
  }

  function escapeHTML(str) {
    if (!str) return '';
    return String(str).replace(/[&<>'"]/g, tag => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;'
    }[tag] || tag));
  }

  // Render Rules Table
  function renderRules() {
    if (!rulesListEl) return;
    if (rules.length === 0) {
      rulesListEl.innerHTML = `
        <div class="empty-state">
          <div class="empty-icon">
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="2"></circle>
              <path d="M16.24 7.76a6 6 0 0 1 0 8.49m-8.48-.01a6 6 0 0 1 0-8.49m11.31-2.82a10 10 0 0 1 0 14.14m-14.14 0a10 10 0 0 1 0-14.14"></path>
            </svg>
          </div>
          <div class="empty-title">No jacks patched yet</div>
          <div class="empty-desc">Fill in the form below to wire your first port forwarding rule.</div>
        </div>
      `;
      if (summaryTextEl) summaryTextEl.innerHTML = '<strong>0</strong> rules &middot; <strong>0</strong> active';
      return;
    }

    let runningCount = 0;
    let totalActiveConns = 0;
    let totalBytesIn = 0;
    let totalBytesOut = 0;

    let html = `
      <div class="table-wrap">
        <table class="rules-table">
          <thead>
            <tr>
              <th style="width: 32px"></th>
              <th>NAME / PROTO</th>
              <th>LISTEN &rarr; TARGET</th>
              <th>CONNECTIONS</th>
              <th>TRAFFIC (IN / OUT)</th>
              <th style="text-align: right">ACTIONS</th>
            </tr>
          </thead>
          <tbody>
    `;

    rules.forEach(r => {
      if (r.running) runningCount++;
      const activeConns = (r.stats && r.stats.ActiveConns) || 0;
      const totalConns = (r.stats && r.stats.TotalConns) || 0;
      const bytesIn = (r.stats && r.stats.BytesIn) || 0;
      const bytesOut = (r.stats && r.stats.BytesOut) || 0;

      totalActiveConns += activeConns;
      totalBytesIn += bytesIn;
      totalBytesOut += bytesOut;

      const isRunning = r.running;
      const statusClass = isRunning ? 'running' : 'stopped';
      const statusLabel = isRunning ? 'Running' : 'Stopped';
      const toggleLabel = isRunning ? 'stop' : 'start';

      html += `
        <tr data-rule-id="${escapeHTML(r.id)}">
          <td>
            <span class="status-indicator ${statusClass}" title="${statusLabel}"></span>
          </td>
          <td>
            <div class="rule-name-col">
              <strong>${escapeHTML(r.name)}</strong>
              <span class="proto-tag">${escapeHTML(r.protocol.toUpperCase())}</span>
            </div>
            ${r.firewall_warning ? `<div class="fw-warning">&#9888; ${escapeHTML(r.firewall_warning)}</div>` : ''}
          </td>
          <td>
            <span class="mono-addr">${escapeHTML(r.listen_addr)}:${r.listen_port} &rarr; ${escapeHTML(r.target_addr)}:${r.target_port}</span>
          </td>
          <td>
            <span class="mono-val">${activeConns} active</span>
            <span class="text-muted">/ ${totalConns} total</span>
          </td>
          <td>
            <span class="mono-val">${formatBytes(bytesIn)}</span>
            <span class="text-muted">/ ${formatBytes(bytesOut)}</span>
          </td>
          <td style="text-align: right">
            <div class="action-buttons">
              <button class="action-btn toggle-btn ${isRunning ? 'btn-stop' : 'btn-start'}" data-action="toggle" data-id="${escapeHTML(r.id)}">${toggleLabel}</button>
              <button class="action-btn delete-btn" data-action="delete" data-id="${escapeHTML(r.id)}" title="Delete rule">&times;</button>
            </div>
          </td>
        </tr>
      `;
    });

    html += `
          </tbody>
        </table>
      </div>
    `;

    rulesListEl.innerHTML = html;

    if (summaryTextEl) {
      summaryTextEl.innerHTML = `<strong>${rules.length}</strong> rules &middot; <strong>${runningCount}</strong> active &middot; <strong>${totalActiveConns}</strong> conns &middot; <strong>${formatBytes(totalBytesIn + totalBytesOut)}</strong> total`;
    }

    updateFilterOptions();
  }

  function updateFilterOptions() {
    if (!ruleFilterSelect) return;
    const currentVal = ruleFilterSelect.value;
    let opts = '<option value="">All Rules</option>';
    rules.forEach(r => {
      opts += `<option value="${escapeHTML(r.id)}">${escapeHTML(r.name || r.id)}</option>`;
    });
    ruleFilterSelect.innerHTML = opts;
    ruleFilterSelect.value = currentVal;
  }

  // Render Log Rows
  function renderLogs() {
    if (!logsTableBodyEl) return;
    const filtered = selectedRuleFilter
      ? trafficLogs.filter(l => l.rule_id === selectedRuleFilter)
      : trafficLogs;

    if (filtered.length === 0) {
      logsTableBodyEl.innerHTML = `
        <tr>
          <td colspan="8" class="text-center text-muted" style="padding: 24px">No connection logs recorded yet</td>
        </tr>
      `;
      return;
    }

    let html = '';
    filtered.slice(0, 100).forEach(l => {
      const statusBadge = l.status === 'closed'
        ? '<span class="badge badge-success">closed</span>'
        : l.status === 'error'
        ? '<span class="badge badge-danger">error</span>'
        : '<span class="badge badge-active">active</span>';

      html += `
        <tr>
          <td class="mono-text">${formatTime(l.time)}</td>
          <td><strong>${escapeHTML(l.rule_name || l.rule_id)}</strong></td>
          <td><span class="proto-tag">${escapeHTML((l.protocol || 'tcp').toUpperCase())}</span></td>
          <td class="mono-text">${escapeHTML(l.client_addr)}</td>
          <td class="mono-text">${escapeHTML(l.target_addr)}</td>
          <td class="mono-text">${formatBytes(l.bytes_in)} / ${formatBytes(l.bytes_out)}</td>
          <td class="mono-text">${l.duration_ms}ms</td>
          <td>${statusBadge}</td>
        </tr>
      `;
    });
    logsTableBodyEl.innerHTML = html;
  }

  // Canvas Traffic Chart
  function updateChart(newBytes) {
    if (!ctx || !canvas) return;

    const now = new Date();
    chartPoints.push({ time: now, val: newBytes });
    if (chartPoints.length > 60) {
      chartPoints.shift();
    }
    drawChart();
  }

  function drawChart() {
    if (!ctx || !canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();

    if (canvas.width !== rect.width * dpr || canvas.height !== rect.height * dpr) {
      canvas.width = rect.width * dpr;
      canvas.height = rect.height * dpr;
    }

    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);

    if (chartPoints.length < 2) {
      ctx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--text-muted').trim() || '#888';
      ctx.font = `${12 * dpr}px sans-serif`;
      ctx.textAlign = 'center';
      ctx.fillText('Waiting for live traffic data...', w / 2, h / 2);
      return;
    }

    let maxVal = Math.max(...chartPoints.map(p => p.val), 1024);
    const paddingBottom = 24 * dpr;
    const paddingTop = 16 * dpr;
    const paddingLeft = 48 * dpr;
    const paddingRight = 16 * dpr;

    const plotW = w - paddingLeft - paddingRight;
    const plotH = h - paddingTop - paddingBottom;

    // Grid lines
    ctx.strokeStyle = getComputedStyle(document.documentElement).getPropertyValue('--border').trim() || '#ddd';
    ctx.lineWidth = 1 * dpr;
    ctx.setLineDash([4 * dpr, 4 * dpr]);

    for (let i = 0; i <= 3; i++) {
      const y = paddingTop + (plotH / 3) * i;
      ctx.beginPath();
      ctx.moveTo(paddingLeft, y);
      ctx.lineTo(w - paddingRight, y);
      ctx.stroke();

      const labelVal = maxVal * (1 - i / 3);
      ctx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--text-faint').trim() || '#aaa';
      ctx.font = `${10 * dpr}px monospace`;
      ctx.textAlign = 'right';
      ctx.fillText(formatBytes(labelVal) + '/s', paddingLeft - 6 * dpr, y + 3 * dpr);
    }
    ctx.setLineDash([]);

    // Path
    const signalColor = getComputedStyle(document.documentElement).getPropertyValue('--signal').trim() || '#168a5b';
    const gradient = ctx.createLinearGradient(0, paddingTop, 0, h - paddingBottom);
    gradient.addColorStop(0, signalColor + '44');
    gradient.addColorStop(1, signalColor + '00');

    ctx.beginPath();
    chartPoints.forEach((p, idx) => {
      const x = paddingLeft + (idx / (chartPoints.length - 1)) * plotW;
      const y = paddingTop + plotH - (p.val / maxVal) * plotH;
      if (idx === 0) {
        ctx.moveTo(x, y);
      } else {
        ctx.lineTo(x, y);
      }
    });

    // Fill area
    ctx.lineTo(paddingLeft + plotW, h - paddingBottom);
    ctx.lineTo(paddingLeft, h - paddingBottom);
    ctx.closePath();
    ctx.fillStyle = gradient;
    ctx.fill();

    // Stroke line
    ctx.beginPath();
    chartPoints.forEach((p, idx) => {
      const x = paddingLeft + (idx / (chartPoints.length - 1)) * plotW;
      const y = paddingTop + plotH - (p.val / maxVal) * plotH;
      if (idx === 0) {
        ctx.moveTo(x, y);
      } else {
        ctx.lineTo(x, y);
      }
    });
    ctx.strokeStyle = signalColor;
    ctx.lineWidth = 2 * dpr;
    ctx.stroke();
  }

  // REST API Client
  async function fetchRules() {
    try {
      const res = await fetch('/api/rules');
      if (!res.ok) return;
      const data = await res.json();
      rules = data.rules || [];
      renderRules();
    } catch (e) {
      console.error('Failed to load rules:', e);
    }
  }

  async function fetchLogs() {
    try {
      const res = await fetch('/api/logs?limit=100');
      if (!res.ok) return;
      const data = await res.json();
      trafficLogs = data.logs || [];
      renderLogs();
    } catch (e) {
      console.error('Failed to load logs:', e);
    }
  }


  function updateLoggingBtnUI() {
    if (!toggleLoggingBtn) return;
    if (loggingEnabled) {
      toggleLoggingBtn.textContent = 'Logging: ON';
      toggleLoggingBtn.className = 'action-btn toggle-log-btn btn-log-on';
      toggleLoggingBtn.title = 'Traffic logging is active. Click to pause.';
    } else {
      toggleLoggingBtn.textContent = 'Logging: OFF';
      toggleLoggingBtn.className = 'action-btn toggle-log-btn btn-log-off';
      toggleLoggingBtn.title = 'Traffic logging is paused. Click to enable.';
    }
  }

  async function fetchConfig() {
    try {
      const res = await fetch('/api/config');
      if (!res.ok) return;
      const data = await res.json();
      if (typeof data.logging_enabled === 'boolean') {
        loggingEnabled = data.logging_enabled;
        updateLoggingBtnUI();
      }
    } catch (e) {
      console.error('Failed to load config:', e);
    }
  }
  // SSE Setup
  let lastBytesTotal = 0;
  let lastStatsTime = Date.now();

  function initSSE() {
    if (!window.EventSource) {
      if (sseStatusEl) sseStatusEl.innerHTML = '<span class="status-dot dot-offline"></span> SSE Unsupported';
      return;
    }

    const es = new EventSource('/api/events');

    es.onopen = () => {
      sseConnected = true;
      if (sseStatusEl) {
        sseStatusEl.innerHTML = '<span class="status-dot dot-online"></span> Realtime';
        sseStatusEl.className = 'sse-badge badge-online';
      }
    };

    es.onerror = () => {
      sseConnected = false;
      if (sseStatusEl) {
        sseStatusEl.innerHTML = '<span class="status-dot dot-reconnecting"></span> Reconnecting...';
        sseStatusEl.className = 'sse-badge badge-reconnecting';
      }
    };

    es.addEventListener('stats', (e) => {
      try {
        const payload = JSON.parse(e.data);
        if (payload.rules) {
          rules = payload.rules;
          renderRules();

          // Calculate delta throughput for chart
          let currentBytes = 0;
          rules.forEach(r => {
            if (r.stats) currentBytes += (r.stats.BytesIn || 0) + (r.stats.BytesOut || 0);
          });
          const now = Date.now();
          const dt = (now - lastStatsTime) / 1000;
          if (dt > 0 && lastBytesTotal > 0 && currentBytes >= lastBytesTotal) {
            const bytesPerSec = (currentBytes - lastBytesTotal) / dt;
            updateChart(bytesPerSec);
          } else {
            updateChart(0);
          }
          lastBytesTotal = currentBytes;
          lastStatsTime = now;
        }
      } catch (err) {
        console.error('Error parsing stats event:', err);
      }
    });

    es.addEventListener('log', (e) => {
      try {
        const entry = JSON.parse(e.data);
        trafficLogs.unshift(entry);
        if (trafficLogs.length > 200) trafficLogs.pop();
        renderLogs();
      } catch (err) {
        console.error('Error parsing log event:', err);
      }
    });
  }

  // Event Listeners & Actions
  if (rulesListEl) {
    rulesListEl.addEventListener('click', async (e) => {
      const btn = e.target.closest('button[data-action]');
      if (!btn) return;
      const action = btn.dataset.action;
      const id = btn.dataset.id;
      if (!id) return;

      btn.disabled = true;
      if (action === 'toggle') {
        try {
          const res = await fetch(`/api/rules/${encodeURIComponent(id)}/toggle`, { method: 'POST' });
          if (res.ok) await fetchRules();
        } catch (err) {
          alert('Failed to toggle rule: ' + err.message);
        }
      } else if (action === 'delete') {
        if (!confirm('Are you sure you want to delete this forwarding rule?')) {
          btn.disabled = false;
          return;
        }
        try {
          const res = await fetch(`/api/rules/${encodeURIComponent(id)}`, { method: 'DELETE' });
          if (res.ok) await fetchRules();
        } catch (err) {
          alert('Failed to delete rule: ' + err.message);
        }
      }
      btn.disabled = false;
    });
  }

  if (addRuleForm) {
    addRuleForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const formData = new FormData(addRuleForm);
      const payload = {
        name: formData.get('name') || '',
        protocol: formData.get('protocol') || 'tcp',
        listen_addr: formData.get('listen_addr') || '0.0.0.0',
        listen_port: parseInt(formData.get('listen_port'), 10),
        target_addr: formData.get('target_addr') || '',
        target_port: parseInt(formData.get('target_port'), 10),
      };

      try {
        const res = await fetch('/api/rules', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        if (res.ok) {
          addRuleForm.reset();
          await fetchRules();
        } else {
          const errData = await res.json();
          alert('Failed to add rule: ' + (errData.error || res.statusText));
        }
      } catch (err) {
        alert('Network error: ' + err.message);
      }
    });
  }

  if (ruleFilterSelect) {
    ruleFilterSelect.addEventListener('change', (e) => {
      selectedRuleFilter = e.target.value;
      renderLogs();
    });
  }

  if (autoScrollCheckbox) {
    autoScrollCheckbox.addEventListener('change', (e) => {
      autoScroll = e.target.checked;
    });
  }

  if (clearLogsBtn) {
    clearLogsBtn.addEventListener('click', async () => {
      if (!confirm('Clear recent in-memory logs?')) return;
      try {
        const res = await fetch('/api/logs/clear', { method: 'POST' });
        if (res.ok) {
          trafficLogs = [];
          renderLogs();
        }
      } catch (err) {
        alert('Failed to clear logs: ' + err.message);
      }
    });
  }

  if (toggleLoggingBtn) {
    toggleLoggingBtn.addEventListener('click', async () => {
      toggleLoggingBtn.disabled = true;
      try {
        const res = await fetch('/api/config/logging', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled: !loggingEnabled })
        });
        if (res.ok) {
          const data = await res.json();
          loggingEnabled = data.logging_enabled;
          updateLoggingBtnUI();
        }
      } catch (err) {
        alert('Failed to toggle logging: ' + err.message);
      }
      toggleLoggingBtn.disabled = false;
    });
  }

  // Auto-Update feature
  const updateBannerEl = document.getElementById('update-banner');
  const updateTextEl = document.getElementById('update-text');
  const updateChangelogLinkEl = document.getElementById('update-changelog-link');
  const updateApplyBtnEl = document.getElementById('update-apply-btn');
  let updateAssetURL = '';

  async function checkUpdate() {
    try {
      const res = await fetch('/api/update/check');
      if (!res.ok) return;
      const info = await res.json();
      if (info && info.update_available && updateBannerEl) {
        updateAssetURL = info.asset_url || '';
        if (updateTextEl) {
          updateTextEl.textContent = `New update available: v${info.latest_version} (Current: v${info.current_version})`;
        }
        if (updateChangelogLinkEl && info.release_url) {
          updateChangelogLinkEl.href = info.release_url;
          updateChangelogLinkEl.style.display = 'inline-flex';
        } else if (updateChangelogLinkEl) {
          updateChangelogLinkEl.style.display = 'none';
        }
        if (updateApplyBtnEl) {
          if (!info.asset_url) {
            updateApplyBtnEl.style.display = 'none';
          } else {
            updateApplyBtnEl.style.display = 'inline-flex';
          }
        }
        updateBannerEl.style.display = 'flex';
      }
    } catch (e) {
      // Silently ignore update check errors
    }
  }

  if (updateApplyBtnEl) {
    updateApplyBtnEl.addEventListener('click', async () => {
      if (!confirm('Download and install update now? Application will restart.')) return;
      updateApplyBtnEl.disabled = true;
      updateApplyBtnEl.textContent = 'Updating...';
      try {
        const res = await fetch('/api/update/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ asset_url: updateAssetURL })
        });
        const data = await res.json();
        if (!res.ok) {
          throw new Error(data.error || 'Update failed');
        }
        alert(data.message || 'Installer launched. Please follow on-screen installer instructions.');
      } catch (err) {
        alert('Failed to apply update: ' + err.message);
        updateApplyBtnEl.disabled = false;
        updateApplyBtnEl.textContent = 'Update & Restart';
      }
    });
  }

  window.addEventListener('resize', drawChart);

  // Initialize
  fetchConfig();
  fetchRules();
  fetchLogs();
  initSSE();
  checkUpdate();
  // Initial empty chart draw
  drawChart();
})();
