#!/usr/bin/env node
/**
 * Reproduces the browser-only 400 responses and prints the server's reason.
 *
 * The same RPCs succeed under curl, so the request is issued from inside the
 * page — same origin, same proxy, same headers the app sets — to capture what
 * actually differs.
 */
import { spawn } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';

const FRONTEND = process.argv[2] || 'http://localhost:5173';
const KEYCLOAK = 'http://localhost:8080';
const CHROME =
  process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const PORT = 9334;

class Cdp {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    ws.addEventListener('message', (ev) => {
      const msg = JSON.parse(ev.data);
      const p = this.pending.get(msg.id);
      if (p) {
        this.pending.delete(msg.id);
        msg.error ? p.reject(new Error(msg.error.message)) : p.resolve(msg.result);
      }
    });
  }
  static async connect(url) {
    const ws = new WebSocket(url);
    await new Promise((res, rej) => {
      ws.addEventListener('open', res, { once: true });
      ws.addEventListener('error', rej, { once: true });
    });
    return new Cdp(ws);
  }
  send(method, params = {}) {
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params }));
    });
  }
}

const tokenRes = await fetch(`${KEYCLOAK}/realms/idp/protocol/openid-connect/token`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
  body: new URLSearchParams({
    grant_type: 'password',
    client_id: 'idp-frontend',
    username: 'admin',
    password: 'admin',
  }),
});
const tokens = await tokenRes.json();

const chrome = spawn(
  CHROME,
  [
    '--headless=new',
    `--remote-debugging-port=${PORT}`,
    '--no-first-run',
    '--disable-gpu',
    '--user-data-dir=/tmp/idp-probe-profile',
    'about:blank',
  ],
  { stdio: 'ignore' },
);

let wsUrl = '';
for (let i = 0; i < 40 && !wsUrl; i++) {
  await sleep(300);
  try {
    wsUrl = (await fetch(`http://127.0.0.1:${PORT}/json/version`).then((r) => r.json()))
      .webSocketDebuggerUrl;
  } catch {}
}

const browser = await Cdp.connect(wsUrl);
const { targetId } = await browser.send('Target.createTarget', { url: FRONTEND });
const list = await fetch(`http://127.0.0.1:${PORT}/json/list`).then((r) => r.json());
const page = await Cdp.connect(list.find((t) => t.id === targetId).webSocketDebuggerUrl);
await page.send('Runtime.enable');
await sleep(2000);

const expr = `(async () => {
  const token = ${JSON.stringify(tokens.access_token)};
  const out = [];
  const call = async (proc, body, label) => {
    const res = await fetch('/rpc/idp.v1.' + proc, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Connect-Protocol-Version': '1',
        'Authorization': 'Bearer ' + token,
      },
      body: JSON.stringify(body),
    });
    const text = await res.text();
    out.push(label + ' -> ' + res.status + ' ' + text.slice(0, 200));
  };

  // Exactly what the app sends.
  await call('ClusterService/ListServices', { namespace: '' }, 'ListServices {namespace:""}');
  await call('ClusterService/ListPods', { namespace: '', app: '' }, 'ListPods {namespace:"",app:""}');
  await call('ClusterService/ListNodes', {}, 'ListNodes {}');
  await call('ClusterService/GetResourceMetrics', {}, 'GetResourceMetrics {}');
  await call('ClusterService/ListClusterNamespaces', {}, 'ListClusterNamespaces {}');
  await call('ClusterService/GetOverview', {}, 'GetOverview {} (control)');

  // Concurrently, the way a page load actually issues them.
  const before = out.length;
  await Promise.all([
    call('ClusterService/ListServices', { namespace: '' }, 'CONCURRENT ListServices'),
    call('ClusterService/ListNodes', {}, 'CONCURRENT ListNodes'),
    call('ClusterService/GetResourceMetrics', {}, 'CONCURRENT GetResourceMetrics'),
    call('ClusterService/ListPods', { namespace: '', app: '' }, 'CONCURRENT ListPods'),
    call('ClusterService/ListEvents', { namespace: '', limit: 50 }, 'CONCURRENT ListEvents'),
  ]);
  void before;
  return out.join('\\n');
})()`;

const { result, exceptionDetails } = await page.send('Runtime.evaluate', {
  expression: expr,
  awaitPromise: true,
  returnByValue: true,
});
console.log(exceptionDetails ? JSON.stringify(exceptionDetails, null, 2) : result.value);

chrome.kill();
