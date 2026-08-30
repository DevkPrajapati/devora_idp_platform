#!/usr/bin/env node
/**
 * Drives the real UI in Chrome and reports console errors, failed requests and
 * screenshots per route.
 *
 * Uses the DevTools protocol directly rather than a browser-automation
 * dependency: Node ships a WebSocket client, and the only capabilities needed
 * are "seed a token", "navigate", "read the console" and "screenshot".
 *
 * Usage: node scripts/ui-smoke.mjs [frontend-url]
 */
import { spawn } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { setTimeout as sleep } from 'node:timers/promises';

const FRONTEND = process.argv[2] || 'http://localhost:5173';
const KEYCLOAK = process.env.KEYCLOAK_URL || 'http://localhost:8080';
const OUT_DIR = new URL('../artifacts/ui-smoke/', import.meta.url).pathname;
const CHROME =
  process.env.CHROME_PATH ||
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const PORT = 9333;
// How long a route gets to settle. A page still showing skeletons after this is
// slower than a user will wait.
const SETTLE_MS = Number(process.env.SETTLE_MS || 6000);

// Every route the app router accepts. The router reads window.location.pathname,
// so these are real paths served by the dev server's SPA fallback.
const ROUTES = [
  '/',
  '/clusters',
  '/workloads',
  '/namespaces',
  '/deployments',
  '/services',
  '/storage',
  '/monitoring',
  '/projects',
  '/databases',
  '/builds',
  '/registry',
  '/rbac',
  '/audit',
  '/settings',
];

async function getToken() {
  const res = await fetch(`${KEYCLOAK}/realms/idp/protocol/openid-connect/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'password',
      client_id: 'idp-frontend',
      username: process.env.IDP_USER || 'admin',
      password: process.env.IDP_PASSWORD || 'admin',
    }),
  });
  if (!res.ok) throw new Error(`token request failed: ${res.status}`);
  return res.json();
}

/** Minimal CDP client over a single page target. */
class Cdp {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    this.listeners = [];
    ws.addEventListener('message', (ev) => {
      const msg = JSON.parse(ev.data);
      if (msg.id !== undefined) {
        const p = this.pending.get(msg.id);
        if (p) {
          this.pending.delete(msg.id);
          msg.error ? p.reject(new Error(msg.error.message)) : p.resolve(msg.result);
        }
        return;
      }
      for (const fn of this.listeners) fn(msg);
    });
  }

  static async connect(url) {
    const ws = new WebSocket(url);
    await new Promise((resolve, reject) => {
      ws.addEventListener('open', resolve, { once: true });
      ws.addEventListener('error', reject, { once: true });
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

  on(fn) {
    this.listeners.push(fn);
  }
}

async function main() {
  await mkdir(OUT_DIR, { recursive: true });
  const tokens = await getToken();

  const chrome = spawn(
    CHROME,
    [
      '--headless=new',
      `--remote-debugging-port=${PORT}`,
      '--no-first-run',
      '--disable-gpu',
      '--hide-scrollbars',
      '--window-size=1440,1000',
      '--user-data-dir=/tmp/idp-ui-smoke-profile',
      'about:blank',
    ],
    { stdio: 'ignore' },
  );

  let wsUrl = '';
  for (let i = 0; i < 40 && !wsUrl; i++) {
    await sleep(300);
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/json/version`);
      wsUrl = (await res.json()).webSocketDebuggerUrl;
    } catch {
      /* chrome not listening yet */
    }
  }
  if (!wsUrl) {
    chrome.kill();
    throw new Error('Chrome did not expose a debugging endpoint');
  }

  const browser = await Cdp.connect(wsUrl);
  const { targetId } = await browser.send('Target.createTarget', { url: 'about:blank' });
  const targets = await fetch(`http://127.0.0.1:${PORT}/json/list`).then((r) => r.json());
  const page = await Cdp.connect(
    targets.find((t) => t.id === targetId).webSocketDebuggerUrl,
  );

  const consoleErrors = [];
  const failedRequests = [];
  // Tracks RPCs from send to response so a request that never comes back can be
  // told apart from one that returned an error. A page stuck on a skeleton is
  // usually the former, and the two need different fixes.
  const inflight = new Map();
  const slow = [];

  page.on((msg) => {
    if (msg.method === 'Runtime.exceptionThrown') {
      const d = msg.params.exceptionDetails;
      consoleErrors.push(d.exception?.description || d.text);
    }
    if (msg.method === 'Runtime.consoleAPICalled' && msg.params.type === 'error') {
      consoleErrors.push(msg.params.args.map((a) => a.value ?? a.description ?? '').join(' '));
    }
    if (msg.method === 'Network.requestWillBeSent') {
      const url = msg.params.request.url;
      if (url.includes('/rpc/') || url.includes('/idp.v1.')) {
        inflight.set(msg.params.requestId, { url, at: Date.now() });
      }
    }
    if (msg.method === 'Network.responseReceived') {
      const { status, url } = msg.params.response;
      const started = inflight.get(msg.params.requestId);
      inflight.delete(msg.params.requestId);
      const ms = started ? Date.now() - started.at : 0;
      const name = url.split('/').pop();
      if (status >= 400) failedRequests.push(`${status} ${name}`);
      else if (ms > 3000) slow.push(`${name} ${(ms / 1000).toFixed(1)}s`);
    }
    if (msg.method === 'Network.loadingFailed') {
      const started = inflight.get(msg.params.requestId);
      inflight.delete(msg.params.requestId);
      if (!msg.params.canceled && started) {
        failedRequests.push(`FAILED ${started.url.split('/').pop()} ${msg.params.errorText}`);
      }
    }
  });

  await page.send('Runtime.enable');
  await page.send('Network.enable');
  await page.send('Page.enable');

  // Seed the session the way a successful login would, so the audit exercises
  // the authenticated routes rather than the login screen.
  await page.send('Page.navigate', { url: FRONTEND });
  await sleep(1500);
  await page.send('Runtime.evaluate', {
    expression: `
      localStorage.setItem('idp_access_token', ${JSON.stringify(tokens.access_token)});
      localStorage.setItem('idp_refresh_token', ${JSON.stringify(tokens.refresh_token || '')});
      localStorage.setItem('idp_user', JSON.stringify({
        id: 'admin', username: 'admin', email: 'admin@idp.local',
        roles: ['platform-admin'], firstName: 'Admin', lastName: 'User'
      }));
      sessionStorage.removeItem('idp_logged_out');
    `,
  });

  const results = [];
  for (const route of ROUTES) {
    consoleErrors.length = 0;
    failedRequests.length = 0;
    slow.length = 0;
    inflight.clear();

    // A full navigation, not a pushState, so each route boots the app from
    // scratch the way a page refresh or a deep link would.
    await page.send('Page.navigate', { url: `${FRONTEND}${route}` });
    await sleep(SETTLE_MS);

    const { result } = await page.send('Runtime.evaluate', {
      expression: `(() => {
        const body = document.body.innerText || '';
        return JSON.stringify({
          chars: body.length,
          onLogin: /Sign in|Login to/i.test(body) && body.length < 2000,
          errorText: (body.match(/Error [^\\n]{0,120}/g) || []).slice(0, 5),
          skeletons: document.querySelectorAll('.animate-pulse').length,
        });
      })()`,
      returnByValue: true,
    });
    const info = JSON.parse(result.value);

    const shot = await page.send('Page.captureScreenshot', {
      format: 'png',
      captureBeyondViewport: true,
    });
    const file = route === '/' ? 'dashboard' : route.slice(1);
    await writeFile(`${OUT_DIR}${file}.png`, Buffer.from(shot.data, 'base64'));

    const now = Date.now();
    results.push({
      route,
      ...info,
      consoleErrors: [...new Set(consoleErrors)],
      failedRequests: [...new Set(failedRequests)],
      slow: [...new Set(slow)],
      pending: [...inflight.values()].map(
        (r) => `${r.url.split('/').pop()} open ${((now - r.at) / 1000).toFixed(1)}s`,
      ),
    });
  }

  chrome.kill();

  let bad = 0;
  console.log(`${'ROUTE'.padEnd(14)} ${'SKEL'.padEnd(5)} NOTES`);
  console.log('-'.repeat(100));
  for (const r of results) {
    const notes = [];
    if (r.onLogin) notes.push('STUCK ON LOGIN');
    if (r.skeletons > 0) notes.push(`${r.skeletons} skeletons after ${SETTLE_MS / 1000}s`);
    for (const p of r.pending) notes.push(`PENDING ${p}`);
    for (const s of r.slow) notes.push(`SLOW ${s}`);
    for (const e of r.errorText) notes.push(e.trim());
    for (const e of r.consoleErrors.slice(0, 2)) notes.push(`console: ${e.slice(0, 80)}`);
    for (const f of r.failedRequests.slice(0, 3)) notes.push(f.slice(0, 80));
    if (notes.length) bad++;
    console.log(
      `${r.route.padEnd(14)} ${String(r.skeletons).padEnd(5)} ${notes.join(' | ') || 'clean'}`,
    );
  }
  console.log('-'.repeat(100));
  console.log(`${results.length} routes, ${bad} with findings. Screenshots: ${OUT_DIR}`);
  if (bad > 0) process.exitCode = 1;
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
