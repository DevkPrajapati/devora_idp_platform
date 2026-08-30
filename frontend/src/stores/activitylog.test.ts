import { describe, expect, it } from 'vitest';
import { matchesActivityScope } from './activitylog';

describe('matchesActivityScope', () => {
  it('matches a build repo and numbered build', () => {
    expect(matchesActivityScope('build:user-web', ['build:user-web'])).toBe(true);
    expect(matchesActivityScope('build:user-web#3', ['build:user-web'])).toBe(true);
    expect(matchesActivityScope('build:user-api#1', ['build:user-web'])).toBe(false);
  });

  it('matches a deploy namespace/name', () => {
    expect(matchesActivityScope('deploy:user-auth1/user-web', ['deploy:user-auth1/user-web'])).toBe(true);
  });
});
