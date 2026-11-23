// The grid columns shared by the containers list head and its rows:
// Container, Status, Address, Memory, then Created with the row's actions.
// Compact rows are the name and status lines, then the menu.
export const containerListLayout = {
  wide: 'minmax(0,1.5fr) minmax(0,1.2fr) minmax(0,1fr) 96px 124px',
  compact: 'minmax(0,1fr) 40px',
} as const;
