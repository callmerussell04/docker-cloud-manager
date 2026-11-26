import { lazy } from 'react';

// The terminal brings xterm with it, so its code loads when a terminal is
// first opened and not with the container page.
export const LazyContainerTerminal = lazy(async () => ({
  default: (await import('./ContainerTerminal')).ContainerTerminal,
}));
