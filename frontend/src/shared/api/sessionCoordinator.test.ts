import { expect, test } from 'vitest';
import { createSessionCoordinator, SessionUnavailableError } from './sessionCoordinator';

type Dependencies = Parameters<typeof createSessionCoordinator>[0];
type Event = [string] | [string, string];

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function fixture(overrides: Partial<Dependencies> = {}) {
  const events: Event[] = [];
  const api = createSessionCoordinator({
    refresh: async () => 'access',
    logout: async () => {},
    withLock: async (fn) => fn(),
    onToken: (token) => events.push(['token', token]),
    onLogout: () => events.push(['logout']),
    onSuspend: (action) => events.push(['suspend', action]),
    isUnauthorized: (error) => (error as { status?: number } | undefined)?.status === 401,
    ...overrides,
  });
  return { api, events };
}

test('startup, callback and interceptor share one refresh', async () => {
  const response = deferred<string>();
  let calls = 0;
  const { api, events } = fixture({ refresh: () => { calls++; return response.promise; } });
  const startup = api.refresh();
  const callback = api.refresh();
  const interceptor = api.refresh();
  response.resolve('new-access');
  expect(await Promise.all([startup, callback, interceptor])).toEqual(['new-access', 'new-access', 'new-access']);
  expect(calls).toBe(1);
  expect(events).toEqual([['token', 'new-access']]);
});

test('logout waits for refresh and suppresses its late token', async () => {
  const response = deferred<string>();
  const { api, events } = fixture({ refresh: () => response.promise });
  const refresh = api.refresh();
  const logout = api.logout();
  response.resolve('late-token');
  await expect(refresh).rejects.toThrow();
  await logout;
  expect(events).toEqual([['logout']]);
});

test('uncertain refresh suspends instead of revoking; explicit retry can recover', async () => {
  let calls = 0;
  const { api, events } = fixture({ refresh: async () => { if (++calls === 1) throw new Error('network'); return 'recovered'; } });
  await expect(api.refresh()).rejects.toThrow();
  expect(events).toEqual([['suspend', 'refresh']]);
  // A delayed interceptor 401 is not an explicit retry; it gets the translatable suspension error.
  const suspended = await api.refresh().catch((error: unknown) => error);
  expect(suspended).toBeInstanceOf(SessionUnavailableError);
  expect((suspended as SessionUnavailableError).messageKey).toBe('auth.session.unavailable');
  expect(calls).toBe(1);
  await api.refresh(true);
  expect(events[1]).toEqual(['token', 'recovered']);
});

test('unauthorized refresh clears auth, and failed logout stays retryable', async () => {
  const unauthorized = fixture({ refresh: async () => { throw { status: 401 }; } });
  await expect(unauthorized.api.refresh()).rejects.toEqual({ status: 401 });
  expect(unauthorized.events).toEqual([['logout']]);
  const failure = fixture({ logout: async () => { throw new Error('unavailable'); } });
  await expect(failure.api.logout()).rejects.toThrow();
  expect(failure.events).toEqual([['suspend', 'logout']]);
});

test('same-origin tabs read the latest cookie after acquiring the shared lock', async () => {
  let queue: Promise<unknown> = Promise.resolve();
  let cookie = 'first';
  const seen: string[] = [];
  const withLock = <T,>(fn: () => Promise<T>) => {
    const result = queue.then(fn);
    queue = result.catch(() => {});
    return result;
  };
  const refresh = async () => { seen.push(cookie); cookie += '-next'; return cookie; };
  const first = fixture({ withLock, refresh });
  const second = fixture({ withLock, refresh });
  await Promise.all([first.api.refresh(), second.api.refresh()]);
  expect(seen).toEqual(['first', 'first-next']);
});

test('another tab logout suppresses an in-flight refresh', async () => {
  const response = deferred<string>();
  const { api, events } = fixture({ refresh: () => response.promise });
  const refresh = api.refresh();
  api.invalidate();
  response.resolve('late');
  await expect(refresh).rejects.toThrow();
  expect(events).toEqual([['logout']]);
});

test('without origin locking independent tabs can submit the same cookie', async () => {
  const release = deferred<void>();
  const seen: string[] = [];
  const refresh = async () => { seen.push('same-cookie'); await release.promise; return 'access'; };
  const first = fixture({ refresh });
  const second = fixture({ refresh });
  const pending = Promise.all([first.api.refresh(), second.api.refresh()]);
  release.resolve();
  await pending;
  expect(seen).toEqual(['same-cookie', 'same-cookie']);
});
