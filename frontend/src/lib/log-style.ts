export type LogLevel =
  | 'fatal'
  | 'error'
  | 'warn'
  | 'info'
  | 'debug'
  | 'trace'
  | 'success'
  | 'plain';

export interface LogSegment {
  text: string;
  className: string;
}

const LEVEL_RE =
  /\b(FATAL|PANIC|CRITICAL|ERROR|ERR|FAILED|FAILURE|EXCEPTION|WARN(?:ING)?|INFO|INF|DEBU(?:G)?|DBG|TRACE|SUCCESS|SUCCEEDED|READY)\b/i;

const HTTP_RE = /\b([1-5]\d{2})\b/;
const QUOTED_RE = /("[^"\\]*(?:\\.[^"\\]*)*"|'[^'\\]*(?:\\.[^'\\]*)*')/;
const KEY_RE = /\b([A-Za-z_][\w.-]*)=/;
const NUMBER_RE = /\b(\d+(?:\.\d+)?(?:ms|s|m|h|Ki|Mi|Gi|Ti)?)\b/;

/** Drops CSI/OSC sequences and leftover `[37m` / `[0m` after ESC was eaten. */
export function stripAnsi(text: string): string {
  if (!text) return '';
  return text
    .replace(/\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)/g, '')
    .replace(/\u001b\[[0-9;?]*[ -/]*[@-~]/g, '')
    .replace(/\u001b./g, '')
    .replace(/\[[0-9;]{1,4}m/g, '');
}

export function classifyLogLine(message: string): LogLevel {
  return classifyStripped(stripAnsi(message));
}

function classifyStripped(message: string): LogLevel {
  if (!message) return 'plain';
  if (/\b(FATAL|PANIC|CRITICAL)\b/i.test(message)) return 'fatal';
  if (/\b(ERROR|ERR|FAILED|FAILURE|EXCEPTION)\b/i.test(message)) return 'error';
  if (/\b5\d{2}\b/.test(message) && /\b(GET|POST|PUT|PATCH|DELETE|HTTP|status)\b/i.test(message)) {
    return 'error';
  }
  if (/\b(WARN(?:ING)?)\b/i.test(message)) return 'warn';
  if (/\b(SUCCESS|SUCCEEDED|READY)\b/i.test(message)) return 'success';
  if (/\b(DEBUG|DEBU|DBG)\b/i.test(message)) return 'debug';
  if (/\bTRACE\b/i.test(message)) return 'trace';
  if (/\b(INFO|INF)\b/i.test(message)) return 'info';
  return 'plain';
}

export function logLevelClass(level: LogLevel): string {
  switch (level) {
    case 'fatal':
      return 'log-fatal';
    case 'error':
      return 'log-error';
    case 'warn':
      return 'log-warn';
    case 'info':
      return 'log-info';
    case 'debug':
      return 'log-debug';
    case 'trace':
      return 'log-trace';
    case 'success':
      return 'log-success';
    default:
      return 'log-plain';
  }
}

export function logLevelBarClass(level: LogLevel): string {
  switch (level) {
    case 'fatal':
    case 'error':
      return 'bg-red-500';
    case 'warn':
      return 'bg-amber-400';
    case 'success':
      return 'bg-emerald-400';
    case 'info':
      return 'bg-sky-400';
    default:
      return 'bg-zinc-600';
  }
}

/**
 * Tokenizes a log line so keywords, HTTP codes, quoted strings, and key=value
 * pairs can be coloured independently of the rest of the message.
 */
export function colorizeLogMessage(message: string): LogSegment[] {
  message = stripAnsi(message);
  if (!message) return [];

  const tokens: { start: number; end: number; className: string }[] = [];
  const push = (re: RegExp, className: string, map?: (m: RegExpExecArray) => string) => {
    const global = new RegExp(re.source, re.flags.includes('g') ? re.flags : `${re.flags}g`);
    let m: RegExpExecArray | null;
    while ((m = global.exec(message)) !== null) {
      const text = map ? map(m) : m[0];
      const start = m.index + m[0].indexOf(text);
      tokens.push({ start, end: start + text.length, className });
      if (m[0].length === 0) global.lastIndex += 1;
    }
  };

  push(LEVEL_RE, 'log-token-level', (m) => m[1]);
  push(QUOTED_RE, 'log-token-string');
  push(KEY_RE, 'log-token-key', (m) => m[1]);
  push(HTTP_RE, 'log-token-http');
  push(NUMBER_RE, 'log-token-number');

  tokens.sort((a, b) => a.start - b.start || b.end - a.end);

  const merged: typeof tokens = [];
  for (const t of tokens) {
    const last = merged[merged.length - 1];
    if (last && t.start < last.end) continue;
    merged.push(t);
  }

  const segments: LogSegment[] = [];
  let cursor = 0;
  for (const t of merged) {
    if (t.start > cursor) {
      segments.push({ text: message.slice(cursor, t.start), className: '' });
    }
    let className = t.className;
    if (className === 'log-token-level') {
      className = `log-token-level ${logLevelClass(classifyLogLine(message.slice(t.start, t.end)))}`;
    } else if (className === 'log-token-http') {
      const code = Number(message.slice(t.start, t.end));
      className = code >= 500 ? 'log-error' : code >= 400 ? 'log-warn' : 'log-success';
    }
    segments.push({ text: message.slice(t.start, t.end), className });
    cursor = t.end;
  }
  if (cursor < message.length) {
    segments.push({ text: message.slice(cursor), className: '' });
  }
  return segments;
}

export function formatClock(ms: number): string {
  const d = new Date(ms);
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  const ss = String(d.getSeconds()).padStart(2, '0');
  return `${hh}:${mm}:${ss}`;
}

export function formatAgo(fromMs: number, nowMs: number): string {
  const delta = Math.max(0, Math.floor((nowMs - fromMs) / 1000));
  if (delta === 0) return 'just now';
  if (delta === 1) return '1s ago';
  if (delta < 60) return `${delta}s ago`;
  const mins = Math.floor(delta / 60);
  if (mins < 60) return `${mins}m ago`;
  return `${Math.floor(mins / 60)}h ago`;
}
