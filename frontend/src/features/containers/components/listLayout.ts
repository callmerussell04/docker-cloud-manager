// The grid columns shared by the containers list head and its rows:
// Container, Status, Address, Memory, then Created with the row's actions.
// Compact rows are the name and status lines, then the menu. The admin view
// adds Owner after the name (owned).
export const containerListLayout = {
  wide: 'minmax(0,1.5fr) minmax(0,1.2fr) minmax(0,1fr) 96px 124px',
  owned: 'minmax(0,1.9fr) minmax(0,0.7fr) minmax(0,1.1fr) minmax(0,0.9fr) 84px 124px',
  compact: 'minmax(0,1fr) 40px',
} as const;
