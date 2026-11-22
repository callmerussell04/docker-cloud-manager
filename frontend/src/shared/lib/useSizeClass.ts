import { useSyncExternalStore } from 'react';
import { sizeClassFor, type SizeClass } from './sizeClass';

function subscribe(onChange: () => void) {
  window.addEventListener('resize', onChange);
  return () => window.removeEventListener('resize', onChange);
}

function snapshot(): SizeClass {
  return sizeClassFor(window.innerWidth);
}

// useSizeClass is the window's current size class; it changes only when the
// window crosses a class boundary.
export function useSizeClass() {
  return useSyncExternalStore(subscribe, snapshot);
}
