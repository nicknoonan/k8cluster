const pollIntervalMs = 10000;

const state = {
  pollTimer: null,
  busy: false,
};

async function request(path) {
  const response = await fetch(path, { method: path.includes('/status') ? 'GET' : 'POST' });
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

function setFeedback(message, isError = false) {
  const feedback = document.getElementById('feedback');
  feedback.textContent = message;
  feedback.className = isError ? 'error' : 'muted';
}

function setButtonsDisabled(disabled) {
  state.busy = disabled;
  document.getElementById('power-on').disabled = disabled;
  document.getElementById('power-off').disabled = disabled;
  document.getElementById('refresh').disabled = disabled;
}

function updateActionAvailability() {
  if (state.busy) {
    return;
  }
  document.getElementById('power-on').disabled = false;
  document.getElementById('power-off').disabled = false;
  document.getElementById('refresh').disabled = false;
}

function formatBoolean(value) {
  return value ? 'Yes' : 'No';
}

function renderBadge(element, text, variant) {
  element.textContent = text;
  element.className = `badge ${variant}`.trim();
}

function renderDeployments(deployments) {
  const container = document.getElementById('deployments');
  const safeDeployments = Array.isArray(deployments) ? deployments : [];
  if (!safeDeployments.length) {
    container.textContent = 'No managed deployments configured.';
    container.className = 'muted';
    return;
  }

  container.className = '';
  container.innerHTML = `
    <ul>
      ${safeDeployments
        .map((deployment) => `<li><strong>${deployment.namespace}/${deployment.name}</strong> - replicas: ${deployment.replicas}</li>`)
        .join('')}
    </ul>
  `;
}

function renderStatus(status) {
  const node = status?.node || {};
  const addresses = Array.isArray(node.addresses) ? node.addresses : [];
  const variant = status.phase === 'READY' ? 'ready' : status.phase === 'OFFLINE' ? 'offline' : 'running';
  renderBadge(document.getElementById('phase-badge'), status.phase, variant);
  document.getElementById('node-name').textContent = node.name || '-';
  document.getElementById('node-exists').textContent = formatBoolean(node.exists);
  document.getElementById('node-ready').textContent = formatBoolean(node.ready);
  document.getElementById('node-cordoned').textContent = formatBoolean(node.cordoned);
  document.getElementById('node-addresses').textContent = addresses.length ? addresses.join(', ') : '-';
  renderDeployments(status.deployments);
  updateActionAvailability();
}

function scheduleRefresh() {
  clearTimeout(state.pollTimer);
  state.pollTimer = setTimeout(() => {
    refresh({ quiet: true }).catch((error) => {
      setFeedback(error.message, true);
      scheduleRefresh();
    });
  }, pollIntervalMs);
}

async function refresh(options = {}) {
  const status = await request('/api/status');
  renderStatus(status);
  if (!options.quiet) {
    setFeedback('Status updated.');
  }
  scheduleRefresh();
  return status;
}

document.getElementById('power-on').addEventListener('click', async () => {
  try {
    setButtonsDisabled(true);
    setFeedback('Starting node power on...');
    await request('/api/power/on');
    await refresh();
  } catch (error) {
    setFeedback(error.message, true);
  } finally {
    setButtonsDisabled(false);
  }
});

document.getElementById('power-off').addEventListener('click', async () => {
  try {
    setButtonsDisabled(true);
    setFeedback('Starting node shutdown...');
    await request('/api/power/off');
    await refresh();
  } catch (error) {
    setFeedback(error.message, true);
  } finally {
    setButtonsDisabled(false);
  }
});

document.getElementById('refresh').addEventListener('click', async () => {
  try {
    setButtonsDisabled(true);
    await refresh();
  } catch (error) {
    setFeedback(error.message, true);
  } finally {
    setButtonsDisabled(false);
  }
});

refresh().catch((error) => {
  setFeedback(error.message, true);
});
