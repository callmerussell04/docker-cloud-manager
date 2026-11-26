import { createContext, useContext } from 'react';

// ShellLayout is what the shell tells the page inside it about the room the
// content area has (see shared/lib/sizeClass.ts for the rules).
export interface ShellLayout {
  // toolbarStacked moves the toolbar's pills to a second row.
  toolbarStacked: boolean;
  // rowsCompact turns list rows into two lines: the name, then status and age,
  // with the column labels and the memory column hidden.
  rowsCompact: boolean;
  // tabBar says the phone tab bar floats over the bottom of the content.
  tabBar: boolean;
}

// Outside the shell a page gets the wide layout.
export const ShellLayoutContext = createContext<ShellLayout>({ toolbarStacked: false, rowsCompact: false, tabBar: false });

export function useShellLayout(): ShellLayout {
  return useContext(ShellLayoutContext);
}
