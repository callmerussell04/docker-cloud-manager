// The grid columns shared by each list head and its rows. Images: Image,
// Status, Used by, Size, then Created with the row's actions. Builds: Build,
// For, Status, Took, then Queued with the row's actions. Compact rows are
// the name and state lines, then the menu. The admin view adds Owner after
// the name (owned).
export const imageListLayout = {
  wide: 'minmax(0,1.4fr) minmax(0,0.9fr) minmax(0,1fr) minmax(84px,0.6fr) 124px',
  owned: 'minmax(0,1.4fr) minmax(0,0.8fr) minmax(0,0.9fr) minmax(0,1fr) minmax(84px,0.6fr) 124px',
  compact: 'minmax(0,1fr) 40px',
} as const;

export const buildListLayout = {
  wide: 'minmax(0,1.2fr) minmax(0,1.2fr) minmax(0,1fr) minmax(84px,0.6fr) 124px',
  owned: 'minmax(0,1.2fr) minmax(0,0.8fr) minmax(0,1.2fr) minmax(0,1fr) minmax(84px,0.6fr) 124px',
  compact: 'minmax(0,1fr) 40px',
} as const;
