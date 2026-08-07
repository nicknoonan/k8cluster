const pollIntervals = {
  idle: 10000,
  active: 1500,
};

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

function updateActionAvailability(operationInProgress) {
  if (state.busy) {
    return;
  }
  document.getElementById('power-on').disabled = operationInProgress;
  document.getElementById('power-off').disabled = operationInProgress;
  document.getElementById('refresh').disabled = false;
}

function formatBoolean(value) {
  return value ? 'Yes' : 'No';
}

function formatTimestamp(value) {
  if (!value) {
    return '';
  }
  return new Date(value).toLocaleTimeString();
}

function renderBadge(element, text, variant) {
  element.textContent = text;
  element.className = `badge ${variant}`.trim();
}

function renderDeployments(deployments) {
  const container = document.getElementById('deployments');
  if (!deployments.length) {
    container.textContent = 'No managed deployments configured.';
    container.className = 'muted';
    return;
  }

  container.className = '';
  container.innerHTML = `
    <ul>
      ${deployments
        .map((deployment) => `<li><strong>${deployment.namespace}/${deployment.name}</strong> - replicas: ${deployment.replicas}</li>`)
        .join('')}
    </ul>
  `;
}

function renderOperation(operation) {
  const badge = document.getElementById('operation-badge');
  const message = document.getElementById('operation-message');
  const error = document.getElementById('operation-error');
  const events = document.getElementById('operation-events');

  if (!operation) {
    renderBadge(badge, 'Idle', '');
    message.textContent = 'No operation is running.';
    error.textContent = '';
    events.innerHTML = '';
    return;
  }

  renderBadge(
    badge,
    `${operation.kind} - ${operation.phase}`,
    operation.inProgress ? 'running' : operation.error ? 'offline' : 'ready',
  );
  message.textContent = operation.message;
  error.textContent = operation.error || '';
  events.innerHTML = operation.events
    .slice()
    .reverse()
    .map(
      (event) => `
        <li>
          <span class="timestamp">${formatTimestamp(event.at)}</span>
          <span>${event.message}</span>
        </li>`,
    )
    .join('');
}

function renderStatus(status) {
  const variant = status.phase === 'READY' ? 'ready' : status.phase === 'OFFLINE' ? 'offline' : 'running';
  renderBadge(document.getElementById('phase-badge'), status.phase, variant);
  document.getElementById('node-name').textContent = status.node.name || '-';
  document.getElementById('node-exists').textContent = formatBoolean(status.node.exists);
  document.getElementById('node-ready').textContent = formatBoolean(status.node.ready);
  document.getElementById('node-cordoned').textContent = formatBoolean(status.node.cordoned);
  document.getElementById('node-addresses').textContent = status.node.addresses.length ? status.node.addresses.join(', ') : '-';
  renderDeployments(status.deployments);
  renderOperation(status.operation);
  updateActionAvailability(Boolean(status.operation?.inProgress));
}

function scheduleRefresh(operationInProgress) {
  clearTimeout(state.pollTimer);
  state.pollTimer = setTimeout(() => {
    refresh({ quiet: true }).catch((error) => {
      setFeedback(error.message, true);
      scheduleRefresh(false);
    });
  }, operationInProgress ? pollIntervals.active : pollIntervals.idle);
}

async function refresh(options = {}) {
  const status = await request('/api/status');
  renderStatus(status);
  if (!options.quiet) {
    setFeedback(status.operation?.inProgress ? 'Shutdown in progress...' : 'Status updated.');
  }
  scheduleRefresh(Boolean(status.operation?.inProgress));
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
    const operation = await request('/api/power/off');
    renderOperation(operation);
    setFeedback(operation.message || 'Shutdown requested.');
    await refresh({ quiet: true });
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
