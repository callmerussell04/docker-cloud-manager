// Grid classes shared by each all-users table header and its rows.

export const adminContainerTableLayout = {
  minWidth: 'min-w-[1340px]',
  grid: 'grid-cols-[minmax(260px,1.7fr)_minmax(150px,0.8fr)_minmax(240px,1.3fr)_minmax(260px,1.5fr)_19rem]',
} as const;

export const adminVolumeTableLayout = {
  minWidth: 'min-w-[980px]',
  grid: 'grid-cols-[minmax(280px,2fr)_minmax(120px,0.8fr)_minmax(190px,1.2fr)_minmax(220px,1.4fr)_4rem]',
} as const;

export const adminImageTableLayout = {
  minWidth: 'min-w-[1100px]',
  grid: 'grid-cols-[minmax(280px,2fr)_7rem_minmax(130px,0.9fr)_minmax(190px,1.2fr)_minmax(220px,1.4fr)_4rem]',
} as const;

export const adminBuildTableLayout = {
  minWidth: 'min-w-[1180px]',
  grid: 'grid-cols-[minmax(180px,1.4fr)_minmax(130px,0.9fr)_minmax(130px,1fr)_minmax(180px,1.3fr)_minmax(170px,1.2fr)_minmax(100px,0.8fr)_10rem]',
} as const;

export const adminProjectTableLayout = {
  minWidth: 'min-w-[1180px]',
  grid: 'grid-cols-[minmax(240px,1.6fr)_minmax(150px,0.9fr)_minmax(240px,1.5fr)_minmax(190px,1.2fr)_minmax(220px,1.3fr)_11rem]',
} as const;
