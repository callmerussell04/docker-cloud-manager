interface SessionDependencies {
  refresh: () => Promise<string>;
  logout: () => Promise<void>;
  withLock: <T>(operation: () => Promise<T>) => Promise<T>;
  onToken: (token: string) => void;
  onLogout: () => void;
  onSuspend: (action: 'refresh' | 'logout') => void;
  isUnauthorized: (error: unknown) => boolean;
}

// All callers share refresh completion, including React effect re-entry.
// The browser adapter supplies an origin-wide lock and reads cookies inside it.
export function createSessionCoordinator(deps: SessionDependencies) {
  let refreshPromise: Promise<string> | null = null;
  let logoutPromise: Promise<void> | null = null;
  let epoch = 0;
  let logoutRequested = false;
  let suspended = false;

  return {
    refresh(explicitRetry = false): Promise<string> {
      if (logoutRequested) return Promise.reject(new Error('Session ended'));
      if (suspended && !explicitRetry) return Promise.reject(new Error('Session temporarily unavailable'));
      suspended = false;
      if (refreshPromise) return refreshPromise;
      const started = epoch;
      refreshPromise = deps.withLock(async () => {
        if (started !== epoch) throw new Error('Session changed');
        return deps.refresh();
      }).then((token) => {
        if (started !== epoch) throw new Error('Session changed');
        deps.onToken(token);
        return token;
      }).catch((error: unknown) => {
        if (started === epoch) {
          if (deps.isUnauthorized(error)) {
            logoutRequested = true;
            deps.onLogout();
          } else {
            suspended = true;
            deps.onSuspend('refresh');
          }
        }
        throw error;
      }).finally(() => { refreshPromise = null; });
      return refreshPromise;
    },
    logout(): Promise<void> {
      if (logoutPromise) return logoutPromise;
      logoutRequested = true;
      epoch++;
      logoutPromise = (refreshPromise?.catch(() => {}) ?? Promise.resolve())
        .then(() => deps.withLock(deps.logout))
        .then(() => deps.onLogout())
        .catch((error: unknown) => {
          deps.onSuspend('logout');
          throw error;
        }).finally(() => { logoutPromise = null; });
      return logoutPromise;
    },
    invalidate() {
      epoch++;
      logoutRequested = true;
      deps.onLogout();
    },
    acceptLogin(token: string) {
      epoch++;
      logoutRequested = false;
      suspended = false;
      deps.onToken(token);
    },
  };
}
