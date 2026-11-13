const API = '/api/v1';
const RECENT_SCAN_PREVIEW_COUNT = 2;
let currentFindings = [];
let scanInProgress = false;
let activeScanWorkspaceId = '';
let scanSuccessWorkspaceId = '';
let workspaceNames = new Map();
let showAllScans = false;
let workspacePendingDelete = null;

function getHideCommonPreference() {
  try { return localStorage.getItem('driftctl-hide-common') !== 'false'; }
  catch (_) { return true; }
}

let hideCommonResources = getHideCommonPreference();

async function fetchJSON(url, opts) {
  const res = await fetch(url, opts);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || res.statusText);
  }
  if (res.status === 204) return null;
  return res.json();
}

function el(tag, attrs = {}, children = []) {
  const node = document.createElement(tag);
  Object.entries(attrs).forEach(([key, value]) => {
    if (key === 'className') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2).toLowerCase(), value);
    else node.setAttribute(key, value);
  });
  children.forEach(child => node.append(child));
  return node;
}

function loadingState(message) {
  return el('div', { className: 'loading-state' }, [
    el('span', { className: 'spinner', 'aria-hidden': 'true' }),
    el('span', { text: message }),
  ]);
}

function setContainerMessage(container, message, type = 'empty') {
  container.innerHTML = '';
  container.append(el('div', { className: `empty-state ${type}` }, [
    el('span', { className: 'empty-state-mark', text: type === 'error' ? '!' : '✓', 'aria-hidden': 'true' }),
    el('span', { text: message }),
  ]));
}

async function loadWorkspaces() {
  const container = document.getElementById('workspaces');
  try {
    const workspaces = await fetchJSON(`${API}/workspaces`);
    workspaceNames = new Map(workspaces.map(workspace => [workspace.id, workspace.name]));
    container.innerHTML = '';
    if (!workspaces.length) {
      setContainerMessage(container, 'No workspaces configured yet. Add a workspace to start scanning.');
      return;
    }
    workspaces.forEach(workspace => {
      const button = el('button', {
        className: 'run-scan-button',
        text: 'Run Scan',
        onClick: event => triggerScan(workspace.id, event.currentTarget),
      });
      button.type = 'button';
      button.dataset.workspaceId = workspace.id;
      const deleteButton = el('button', {
        className: 'workspace-delete-button',
        type: 'button',
        text: '×',
        'aria-label': `Delete workspace ${workspace.name}`,
        title: `Delete ${workspace.name}`,
        onClick: () => openWorkspaceDeleteModal(workspace),
      });
      const card = el('article', { className: 'workspace-card' }, [
        deleteButton,
        el('div', { className: 'workspace-card-heading' }, [
          el('div', {}, [
            el('p', { className: 'eyebrow', text: (workspace.provider || 'cloud').toUpperCase() }),
            el('h3', { text: workspace.name }),
          ]),
          el('span', { className: 'workspace-indicator', 'aria-hidden': 'true' }),
        ]),
        el('p', { className: 'meta', text: `Regions: ${(workspace.regions || []).join(', ') || 'default'}` }),
        button,
      ]);
      card.dataset.workspaceId = workspace.id;
      container.append(card);
    });
    updateScanButtons();
  } catch (error) {
    setContainerMessage(container, `Could not load workspaces: ${error.message}`, 'error');
  }
}

function openWorkspaceDeleteModal(workspace) {
  workspacePendingDelete = workspace;
  document.getElementById('workspace-delete-name').textContent = workspace.name;
  const modal = document.getElementById('workspace-delete-modal');
  document.getElementById('workspace-delete-confirm').disabled = false;
  modal.showModal();
}

async function deleteWorkspace() {
  if (!workspacePendingDelete) return;
  const workspace = workspacePendingDelete;
  const confirmButton = document.getElementById('workspace-delete-confirm');
  confirmButton.disabled = true;
  confirmButton.textContent = 'Deleting…';
  try {
    await fetchJSON(`${API}/workspaces/${encodeURIComponent(workspace.id)}`, { method: 'DELETE' });
    document.getElementById('workspace-delete-modal').close();
    workspacePendingDelete = null;
    document.querySelector(`.workspace-card[data-workspace-id="${CSS.escape(workspace.id)}"]`)?.remove();
    await loadWorkspaces();
    showToast('Workspace Deleted', `Workspace '${workspace.name}' deleted successfully.`, 'success');
  } catch (error) {
    showToast('Could Not Delete Workspace', error.message || 'The workspace could not be deleted.', 'error');
  } finally {
    confirmButton.disabled = false;
    confirmButton.textContent = 'Delete Workspace';
  }
}

document.getElementById('workspace-delete-cancel').addEventListener('click', () => {
  document.getElementById('workspace-delete-modal').close();
  workspacePendingDelete = null;
});
document.getElementById('workspace-delete-form').addEventListener('submit', event => {
  event.preventDefault();
  deleteWorkspace();
});
document.getElementById('workspace-delete-modal').addEventListener('close', () => {
  workspacePendingDelete = null;
});

function updateScanButtons() {
  document.querySelectorAll('.run-scan-button').forEach(button => {
    const isBusy = scanInProgress && button.dataset.workspaceId === activeScanWorkspaceId &&
      button.dataset.workspaceId !== scanSuccessWorkspaceId;
    const isComplete = button.dataset.workspaceId === scanSuccessWorkspaceId;
    button.disabled = scanInProgress || isComplete;
    button.classList.toggle('is-busy', isBusy);
    button.classList.toggle('is-complete', isComplete);
    button.innerHTML = '';
    if (isBusy) {
      button.append(el('span', { className: 'spinner spinner-small', 'aria-hidden': 'true' }));
      button.append(el('span', { text: 'Scanning AWS Resources...' }));
    } else if (isComplete) {
      button.append(el('span', { className: 'button-check', text: '✓', 'aria-hidden': 'true' }));
      button.append(el('span', { text: 'Scan Completed' }));
    } else {
      button.textContent = 'Run Scan';
    }
  });
}

async function triggerScan(workspaceId) {
  if (scanInProgress) return;
  scanInProgress = true;
  activeScanWorkspaceId = workspaceId;
  scanSuccessWorkspaceId = '';
  updateScanButtons();
  const progress = document.getElementById('scan-progress');
  document.getElementById('scan-progress-message').textContent =
    `Scanning AWS resources for ${workspaceNames.get(workspaceId) || 'workspace'}…`;
  progress.hidden = false;

  try {
    const report = await fetchJSON(`${API}/workspaces/${workspaceId}/scans`, { method: 'POST' });
    if (report.status !== 'completed') {
      throw new Error('The scan did not complete. Check the workspace state and server logs.');
    }

    scanSuccessWorkspaceId = workspaceId;
    updateScanButtons();
    showReport(report);
    await Promise.all([loadScans(), loadWorkspaces()]);
    showToast('Scan Completed Successfully', `The scan completed with ${report.summary?.total_findings ?? 0} findings.`, 'success');
    if (report.email_notification_status === 'sent') {
      showToast('Email Notification Sent', 'The drift report has been sent successfully.', 'success');
    } else if (report.email_notification_status === 'failed') {
      showToast('Email Notification Failed', 'The scan completed successfully, but the email notification could not be sent.', 'warning');
    }
  } catch (error) {
    scanSuccessWorkspaceId = '';
    showToast('Scan Failed', error.message || 'The scan could not be completed.', 'error');
  } finally {
    scanInProgress = false;
    activeScanWorkspaceId = '';
    progress.hidden = true;
    updateScanButtons();
    if (scanSuccessWorkspaceId === workspaceId) {
      window.setTimeout(() => {
        if (scanSuccessWorkspaceId === workspaceId) {
          scanSuccessWorkspaceId = '';
          updateScanButtons();
        }
      }, 1600);
    }
  }
}

function showToast(title, message, type = 'success') {
  const region = document.getElementById('toast-region');
  const toast = el('div', { className: `toast toast-${type}`, role: type === 'error' || type === 'warning' ? 'alert' : 'status' }, [
    el('span', { className: 'toast-icon', text: type === 'success' ? '✓' : type === 'warning' ? '!' : '×', 'aria-hidden': 'true' }),
    el('div', { className: 'toast-copy' }, [
      el('strong', { text: title }),
      el('span', { text: message }),
    ]),
    el('button', { className: 'toast-close', text: '×', 'aria-label': 'Dismiss notification', onClick: event => event.currentTarget.closest('.toast').remove() }),
  ]);
  region.append(toast);
  window.setTimeout(() => {
    toast.classList.add('toast-leaving');
    window.setTimeout(() => toast.remove(), 220);
  }, 6500);
}

async function loadScans() {
  const container = document.getElementById('scans');
  const controls = document.getElementById('scan-history-controls');
  try {
    const scans = await fetchJSON(`${API}/scans?limit=20`);
    renderDashboardSummary(scans[0]);
    container.innerHTML = '';
    controls.innerHTML = '';
    if (!scans.length) {
      setContainerMessage(container, 'No scans yet. Run a workspace scan to see results here.');
      return;
    }

    const table = el('table', { className: 'history-table' });
    table.append(el('thead', {}, [el('tr', {}, [
      el('th', { text: 'Scan' }), el('th', { text: 'Workspace' }),
      el('th', { text: 'Status' }), el('th', { text: 'Findings' }),
      el('th', { text: 'Scan time' }),
    ])]));
    const tbody = el('tbody');
    const visibleScans = showAllScans ? scans : scans.slice(0, RECENT_SCAN_PREVIEW_COUNT);
    visibleScans.forEach(scan => {
      const link = el('button', { className: 'history-link', text: `${(scan.scan_id || '').slice(0, 8)}…` });
      link.type = 'button';
      link.addEventListener('click', () => showReportById(scan.scan_id));
      tbody.append(el('tr', {}, [
        el('td', {}, [link]),
        el('td', { text: scan.workspace || '—' }),
        el('td', {}, [el('span', { className: `badge badge-${scan.status}`, text: scan.status })]),
        el('td', { className: 'number-cell', text: String(scan.summary?.total_findings ?? 0) }),
        el('td', { text: formatTime(scan.completed_at || scan.started_at) }),
      ]));
    });
    table.append(tbody);
    container.append(table);
    if (scans.length > RECENT_SCAN_PREVIEW_COUNT) {
      const toggle = el('button', {
        className: 'history-toggle',
        type: 'button',
        text: showAllScans ? 'Show Less' : 'View All Scans',
        'aria-expanded': String(showAllScans),
        onClick: () => {
          showAllScans = !showAllScans;
          loadScans();
        },
      });
      controls.append(toggle);
    }
  } catch (error) {
    renderDashboardSummary(null);
    setContainerMessage(container, `Could not load scan history: ${error.message}`, 'error');
  }
}

function formatTime(value) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}

function renderDashboardSummary(scan) {
  const container = document.getElementById('dashboard-summary');
  container.innerHTML = '';
  const findings = scan?.findings || [];
  const count = severity => findings.filter(finding => finding.severity === severity).length;
  const stats = [
    ['Resources scanned', scan?.summary?.total_resources ?? 0, 'stat-blue', '▦'],
    ['Drift findings', scan?.summary?.total_findings ?? 0, 'stat-purple', '✦'],
    ['Critical', count('critical'), 'stat-red', '!'],
    ['Warnings', count('warning'), 'stat-amber', '△'],
    ['Info', count('info'), 'stat-slate', 'i'],
    ['Last scan', formatTime(scan?.completed_at || scan?.started_at), 'stat-slate', '◷'],
  ];
  stats.forEach(([label, value, tone, icon], index) => {
    container.append(el('div', { className: `stat-card ${tone}`, style: `--card-order:${index}` }, [
      el('div', { className: 'stat-card-top' }, [
        el('span', { className: 'stat-label', text: label }),
        el('span', { className: 'stat-icon', text: icon, 'aria-hidden': 'true' }),
      ]),
      el('div', { className: label === 'Last scan' ? 'stat-value stat-time' : 'stat-value', text: String(value) }),
    ]));
  });
}

async function showReportById(scanId) {
  try {
    const report = await fetchJSON(`${API}/scans/${scanId}`);
    showReport(report);
    document.getElementById('report-panel').scrollIntoView({ behavior: 'smooth', block: 'start' });
  } catch (error) {
    showToast('Could Not Load Report', error.message, 'error');
  }
}

function showReport(report) {
  const panel = document.getElementById('report-panel');
  panel.hidden = false;
  document.getElementById('report-scan-id').textContent = report.scan_id || '';

  const summary = document.getElementById('summary');
  summary.innerHTML = '';
  const severities = { critical: 0, warning: 0, info: 0 };
  (report.findings || []).forEach(finding => {
    if (severities[finding.severity] !== undefined) severities[finding.severity]++;
  });
  [
    ['Resources', report.summary?.total_resources, 'stat-blue'],
    ['Critical', severities.critical, 'stat-red'],
    ['Warnings', severities.warning, 'stat-amber'],
    ['Info', severities.info, 'stat-slate'],
    ['Total findings', report.summary?.total_findings, 'stat-purple'],
  ].forEach(([label, value, tone]) => {
    summary.append(el('div', { className: `stat-card compact ${tone}` }, [
      el('div', { className: 'stat-card-top' }, [
        el('span', { className: 'stat-label', text: label }),
        el('span', { className: 'stat-icon', text: label === 'Resources' ? '▦' : label === 'Critical' ? '!' : label === 'Warnings' ? '△' : label === 'Info' ? 'i' : '✦', 'aria-hidden': 'true' }),
      ]),
      el('div', { className: 'stat-value', text: String(value ?? 0) }),
    ]));
  });

  currentFindings = report.findings || [];
  populateFilter('filter-severity', [...new Set(currentFindings.map(finding => finding.severity).filter(Boolean))], 'All severities');
  populateFilter('filter-resource-type', [...new Set(currentFindings.map(finding => finding.resource_type).filter(Boolean))], 'All resource types');
  document.getElementById('filter-kind').value = '';
  const commonToggle = document.getElementById('hide-common-resources');
  commonToggle.checked = hideCommonResources;
  renderFindings();
}

function populateFilter(id, values, allLabel) {
  const select = document.getElementById(id);
  select.innerHTML = '';
  select.append(el('option', { value: '', text: allLabel }));
  values.sort().forEach(value => select.append(el('option', { value, text: value })));
  select.value = '';
}

function isCommonInfrastructure(finding) {
  const type = (finding.resource_type || '').toLowerCase();
  if (!['aws_vpc', 'aws_subnet', 'aws_security_group'].includes(type)) return false;

  const name = (finding.resource_name || '').trim().toLowerCase();
  const resourceID = (finding.resource_id || '').toLowerCase();
  const identity = `${name} ${resourceID}`;
  if (type === 'aws_vpc' && (name === 'default' || name.includes('default vpc') || name.includes('default-vpc'))) return true;
  if (type === 'aws_security_group' && (name === 'default' || name.includes('default security group'))) return true;

  // The AWS inventory names untagged VPCs and subnets by their generated IDs.
  // These are commonly default networking resources; the toggle lets users
  // bring them back whenever they want the complete report.
  const idTail = resourceID.split('/').pop();
  if (name === idTail && ((type === 'aws_vpc' && /^vpc-[0-9a-f]+$/.test(name)) ||
    (type === 'aws_subnet' && /^subnet-[0-9a-f]+$/.test(name)))) return true;

  return name.startsWith('eksctl-') || name.startsWith('eks-') ||
    /(^|[^a-z0-9])(eksctl|eks|kubernetes|k8s)([^a-z0-9]|$)/.test(identity);
}

function renderFindings() {
  const severity = document.getElementById('filter-severity').value;
  const resourceType = document.getElementById('filter-resource-type').value;
  const kind = document.getElementById('filter-kind').value;
  const filtered = currentFindings.filter(finding =>
    (!severity || finding.severity === severity) &&
    (!resourceType || finding.resource_type === resourceType) &&
    (!kind || finding.kind === kind) &&
    (!hideCommonResources || !isCommonInfrastructure(finding)));

  document.getElementById('findings-count').textContent = `Showing ${filtered.length} of ${currentFindings.length} findings`;
  const findingsEl = document.getElementById('findings');
  findingsEl.innerHTML = '';
  if (!filtered.length) {
    const message = currentFindings.length ? 'No findings match these filters.' : 'No drift detected. Your infrastructure matches the recorded state.';
    setContainerMessage(findingsEl, message);
    return;
  }

  const table = el('table', { className: 'findings-table' });
  table.append(el('thead', {}, [el('tr', {}, [
    el('th', { text: 'Drift type' }), el('th', { text: 'Severity' }),
    el('th', { text: 'Resource' }), el('th', { text: 'Field' }),
    el('th', { text: 'Expected' }), el('th', { text: 'Actual' }),
  ])]));
  const tbody = el('tbody');
  filtered.forEach(finding => tbody.append(el('tr', {}, [
    el('td', {}, [el('span', { className: 'kind-chip', text: finding.kind })]),
    el('td', {}, [el('span', { className: `severity-pill severity-${finding.severity}`, text: finding.severity })]),
    el('td', { className: 'resource-cell', text: `${finding.resource_name || finding.resource_id} (${finding.resource_type})` }),
    el('td', { text: finding.field || '—' }),
    el('td', { className: 'value-cell', text: finding.expected != null ? JSON.stringify(finding.expected) : '—' }),
    el('td', { className: 'value-cell', text: finding.actual != null ? JSON.stringify(finding.actual) : '—' }),
  ])));
  table.append(tbody);
  findingsEl.append(table);
}

['filter-severity', 'filter-resource-type', 'filter-kind'].forEach(id => {
  document.getElementById(id).addEventListener('change', renderFindings);
});

const commonToggle = document.getElementById('hide-common-resources');
commonToggle.checked = hideCommonResources;
commonToggle.addEventListener('change', () => {
  hideCommonResources = commonToggle.checked;
  try { localStorage.setItem('driftctl-hide-common', String(hideCommonResources)); }
  catch (_) { /* The toggle remains usable when browser storage is unavailable. */ }
  renderFindings();
});

loadWorkspaces();
loadScans();
