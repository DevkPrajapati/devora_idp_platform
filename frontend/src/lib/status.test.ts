import { describe, expect, it } from 'vitest';
import { chartBarClass, meterBarClass, statusTone } from './status';

describe('statusTone', () => {
  it('marks healthy work as success and failures as danger', () => {
    expect(statusTone('Running')).toBe('success');
    expect(statusTone('Ready')).toBe('success');
    expect(statusTone('success')).toBe('success');
    expect(statusTone('Failed')).toBe('danger');
    expect(statusTone('error')).toBe('danger');
    expect(statusTone('Pending')).toBe('warn');
  });
});

describe('chart / meter colours', () => {
  it('uses green bars for success and red for failure', () => {
    expect(chartBarClass('Running')).toContain('emerald');
    expect(chartBarClass('Failed')).toContain('red');
  });

  it('shifts meter colour as load rises', () => {
    expect(meterBarClass(20)).toContain('emerald');
    expect(meterBarClass(75)).toContain('amber');
    expect(meterBarClass(95)).toContain('red');
  });
});
