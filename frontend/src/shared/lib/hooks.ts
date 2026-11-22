import { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

export function useDebouncedValue<T>(value: T, delayMs = 350) {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delayMs);
    return () => window.clearTimeout(timer);
  }, [delayMs, value]);

  return debounced;
}

// usePageOf is a page number that returns to 1 whenever any filter changes,
// in the same render, including a change back to earlier filters.
export function usePageOf(filters: readonly unknown[]) {
  const key = JSON.stringify(filters);
  const [state, setState] = useState({ key, page: 1 });
  if (state.key !== key) {
    // Adjusting state while rendering, as React documents for state that
    // follows a changed input; React re-renders before committing.
    setState({ key, page: 1 });
  }
  const page = state.key === key ? state.page : 1;
  const setPage = (next: number) => setState({ key, page: next });
  return [page, setPage] as const;
}

// createDialogFlag is the search parameter that opens a list page's create
// dialog, as /containers?new does.
export const createDialogFlag = 'new';

// useSearchFlag is a yes-or-no state kept in the URL as a search parameter
// with no value, such as ?new for a page's create dialog, so a link (the
// search palette's New container, for one) can open it. Setting it replaces
// the history entry rather than adding one.
export function useSearchFlag(name: string) {
  const [params, setParams] = useSearchParams();
  const on = params.has(name);
  const set = useCallback(
    (next: boolean) =>
      setParams(
        (current) => {
          const updated = new URLSearchParams(current);
          if (next) updated.set(name, '');
          else updated.delete(name);
          return updated;
        },
        { replace: true },
      ),
    [name, setParams],
  );
  return [on, set] as const;
}

function nowSeconds() {
  return Math.floor(Date.now() / 1000);
}

// useNowSeconds is the current time in Unix seconds, renewed every intervalMs,
// so a value derived from it stays the same within a render.
export function useNowSeconds(intervalMs = 30_000) {
  const [now, setNow] = useState(nowSeconds);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(nowSeconds()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);

  return now;
}
