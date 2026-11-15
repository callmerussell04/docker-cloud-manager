import { lazy } from 'react';

// The terminal brings xterm with it, so its code loads when a terminal is
// first opened and not with every page that lists containers.
export const LazyContainerTerminalModal = lazy(async () => ({
  default: (await import('./ContainerTerminalModal')).ContainerTerminalModal,
}));
