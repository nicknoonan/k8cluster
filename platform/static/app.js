async function request(path) {
  const response = await fetch(path, { method: path.includes('/status') ? 'GET' : 'POST' });
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.error || response.statusText);
  }
  return body;
}

async function refresh() {
  const status = await request('/api/status');
  document.getElementById('status').textContent = JSON.stringify(status, null, 2);
}

document.getElementById('power-on').addEventListener('click', async () => {
  await request('/api/power/on');
  await refresh();
});

document.getElementById('power-off').addEventListener('click', async () => {
  await request('/api/power/off');
  await refresh();
});

document.getElementById('refresh').addEventListener('click', refresh);
refresh().catch((error) => {
  document.getElementById('status').textContent = error.message;
});
