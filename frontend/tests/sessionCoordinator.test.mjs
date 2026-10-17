import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createSessionCoordinator } from '../src/shared/api/sessionCoordinator.ts';

function deferred() {
 let resolve, reject;
 const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
 return { promise, resolve, reject };
}

function fixture(overrides = {}) {
 const events = [];
 const api = createSessionCoordinator({
  refresh: async () => 'access', logout: async () => {},
  withLock: async (fn) => fn(),
  onToken: (token) => events.push(['token', token]),
  onLogout: () => events.push(['logout']),
  onSuspend: (action) => events.push(['suspend', action]),
  isUnauthorized: (error) => error?.status === 401,
  ...overrides,
 });
 return { api, events };
}

test('startup, callback and interceptor share one refresh', async () => {
 const response = deferred(); let calls = 0;
 const { api, events } = fixture({ refresh: () => { calls++; return response.promise; } });
 const startup = api.refresh(); const callback = api.refresh(); const interceptor = api.refresh();
 response.resolve('new-access');
 assert.deepEqual(await Promise.all([startup, callback, interceptor]), ['new-access', 'new-access', 'new-access']);
 assert.equal(calls, 1);
 assert.deepEqual(events, [['token', 'new-access']]);
});

test('logout waits for refresh and suppresses its late token', async () => {
 const response = deferred();
 const { api, events } = fixture({ refresh: () => response.promise });
 const refresh = api.refresh();
 const logout = api.logout();
 response.resolve('late-token');
 await assert.rejects(refresh);
 await logout;
 assert.deepEqual(events, [['logout']]);
});

test('uncertain refresh suspends instead of revoking; explicit retry can recover', async () => {
 let calls = 0;
 const { api, events } = fixture({ refresh: async () => { if (++calls === 1) throw new Error('network'); return 'recovered'; } });
 await assert.rejects(api.refresh());
 assert.deepEqual(events, [['suspend', 'refresh']]);
 await assert.rejects(api.refresh()); // A delayed interceptor 401 is not an explicit retry.
 assert.equal(calls, 1);
 await api.refresh(true);
 assert.deepEqual(events[1], ['token', 'recovered']);
});

test('unauthorized refresh clears auth, and failed logout stays retryable', async () => {
 const unauthorized = fixture({ refresh: async () => { throw { status: 401 }; } });
 await assert.rejects(unauthorized.api.refresh());
 assert.deepEqual(unauthorized.events, [['logout']]);
 const failure = fixture({ logout: async () => { throw new Error('unavailable'); } });
 await assert.rejects(failure.api.logout());
 assert.deepEqual(failure.events, [['suspend', 'logout']]);
});

test('same-origin tabs read the latest cookie after acquiring the shared lock', async () => {
 let queue = Promise.resolve(); let cookie = 'first';
 const seen = [];
 const withLock = (fn) => {
  const result = queue.then(fn);
  queue = result.catch(() => {});
  return result;
 };
 const refresh = async () => { seen.push(cookie); cookie += '-next'; return cookie; };
 const first = fixture({ withLock, refresh }); const second = fixture({ withLock, refresh });
 await Promise.all([first.api.refresh(), second.api.refresh()]);
 assert.deepEqual(seen, ['first', 'first-next']);
});

test('another tab logout suppresses an in-flight refresh', async () => {
 const response = deferred(); const { api, events } = fixture({ refresh: () => response.promise });
 const refresh = api.refresh();
 api.invalidate();
 response.resolve('late');
 await assert.rejects(refresh);
 assert.deepEqual(events, [['logout']]);
});

test('without origin locking independent tabs can submit the same cookie', async () => {
 const release = deferred(); const seen = [];
 const refresh = async () => { seen.push('same-cookie'); await release.promise; return 'access'; };
 const first = fixture({ refresh }); const second = fixture({ refresh });
 const pending = Promise.all([first.api.refresh(), second.api.refresh()]);
 release.resolve(); await pending;
 assert.deepEqual(seen, ['same-cookie', 'same-cookie']);
});
