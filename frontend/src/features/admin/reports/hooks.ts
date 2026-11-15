import { useEffect, useState } from 'react';

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
