import { describe, expect, it } from 'vitest';
import { classifyLogLine, colorizeLogMessage, formatAgo, formatClock, stripAnsi } from './log-style';

describe('classifyLogLine', () => {
  it('detects error, warn, info, and success keywords', () => {
    expect(classifyLogLine('ERROR connection refused')).toBe('error');
    expect(classifyLogLine('WARN disk almost full')).toBe('warn');
    expect(classifyLogLine('INFO listening on :8080')).toBe('info');
    expect(classifyLogLine('Build SUCCESS')).toBe('success');
    expect(classifyLogLine('DEBU Running: npm install')).toBe('debug');
    expect(classifyLogLine('\u001b[37mDEBU\u001b[0m Running')).toBe('debug');
    expect(classifyLogLine('[37mDEBU [0m Running')).toBe('debug');
  });
});

describe('stripAnsi', () => {
  it('removes CSI codes and leftover SGR brackets', () => {
    expect(stripAnsi('\u001b[37mDEBU\u001b[0m Running')).toBe('DEBU Running');
    expect(stripAnsi('[37mDEBU [0m Running')).toBe('DEBU  Running');
  });
});

describe('colorizeLogMessage', () => {
  it('splits keywords and quoted strings into coloured segments', () => {
    const segs = colorizeLogMessage('ERROR failed to open "config.yaml"');
    const joined = segs.map((s) => s.text).join('');
    expect(joined).toBe('ERROR failed to open "config.yaml"');
    expect(segs.some((s) => s.className.includes('log-error') || s.className.includes('log-token-level'))).toBe(true);
    expect(segs.some((s) => s.className === 'log-token-string')).toBe(true);
  });
});

describe('formatClock / formatAgo', () => {
  it('renders HH:MM:SS and relative seconds', () => {
    const ms = new Date('2026-08-28T12:34:56').getTime();
    expect(formatClock(ms)).toMatch(/\d{2}:\d{2}:\d{2}/);
    expect(formatAgo(ms, ms)).toBe('just now');
    expect(formatAgo(ms, ms + 3500)).toBe('3s ago');
  });
});
