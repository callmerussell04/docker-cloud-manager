// Grid classes shared by each table header and the row component that renders it.

export const imageTableLayout = {
  minWidth: 'min-w-[920px]',
  grid: 'grid-cols-[minmax(260px,3fr)_7rem_8rem_minmax(180px,1.5fr)_8rem]',
} as const;

export const buildTableLayout = {
  minWidth: 'min-w-[940px]',
  grid: 'grid-cols-[minmax(220px,1.5fr)_minmax(150px,1fr)_7rem_minmax(180px,1.5fr)_10rem]',
} as const;
