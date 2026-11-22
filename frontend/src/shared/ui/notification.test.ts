import { describe, expect, it } from 'vitest';
import { notificationLifetimeMs } from './notification';

describe('notificationLifetimeMs', () => {
  it('lets success and info leave on their own after five seconds', () => {
    expect(notificationLifetimeMs('success')).toBe(5000);
    expect(notificationLifetimeMs('info')).toBe(5000);
  });

  it('keeps an error until it is dismissed', () => {
    expect(notificationLifetimeMs('error')).toBeNull();
  });
});
