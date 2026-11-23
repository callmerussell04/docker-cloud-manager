// The grid columns shared by the volumes list head and its rows: Volume,
// Status, Size, Used by, then Created with the row's actions. Compact rows
// are the name and status lines, then the menu.
export const volumeListLayout = {
  wide: 'minmax(0,1.3fr) minmax(0,0.9fr) minmax(118px,0.9fr) minmax(0,1fr) 124px',
  compact: 'minmax(0,1fr) 40px',
} as const;
