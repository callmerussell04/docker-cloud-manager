// The grid columns shared by the users list head and its rows: User, Role,
// Status, Quota, then the row's actions (users have no age). Compact rows are
// the name and its line, then the menu.
export const userListLayout = {
  wide: 'minmax(0,1.6fr) minmax(0,0.6fr) minmax(0,0.7fr) minmax(0,1.5fr) 96px',
  compact: 'minmax(0,1fr) 40px',
} as const;
