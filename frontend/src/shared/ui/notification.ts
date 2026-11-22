export type NotificationKind = 'success' | 'error' | 'info';

const leavesAfterMs = 5000;

// notificationLifetimeMs is how long a notification stays before it leaves on
// its own, or null when it stays until dismissed. Success and info leave; an
// error stays, so its reason is never gone before it is read.
export function notificationLifetimeMs(kind: NotificationKind): number | null {
  return kind === 'error' ? null : leavesAfterMs;
}
