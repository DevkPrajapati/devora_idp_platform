import { describe, expect, it } from 'vitest';
import { buildJobName, isBuildActive, matchBuildPodName } from './builds';

describe('matchBuildPodName', () => {
  it('matches the Job controller pod prefix', () => {
    expect(
      matchBuildPodName(
        ['build-user-api-1-7xk2f', 'build-user-api-2-aaaaa'],
        'build-user-api-1',
      ),
    ).toBe('build-user-api-1-7xk2f');
  });

  it('accepts a pod named exactly after the Job', () => {
    expect(matchBuildPodName(['build-api-3'], 'build-api-3')).toBe('build-api-3');
  });

  it('returns empty when no pod belongs to this Job', () => {
    expect(matchBuildPodName(['build-api-2-xyz'], 'build-api-1')).toBe('');
  });
});

describe('buildJobName', () => {
  it('stays a DNS label', () => {
    expect(buildJobName('user-api', 1)).toBe('build-user-api-1');
    expect(buildJobName('user-api', 1).length).toBeLessThanOrEqual(63);
  });
});

describe('isBuildActive', () => {
  it('treats pending and running as live', () => {
    expect(isBuildActive('pending')).toBe(true);
    expect(isBuildActive('running')).toBe(true);
    expect(isBuildActive('failed')).toBe(false);
  });
});
