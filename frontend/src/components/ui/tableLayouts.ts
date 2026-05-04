export const tableLayouts = {
  containers: {
    minWidth: 'min-w-[980px]',
    grid: 'grid-cols-[minmax(220px,2fr)_minmax(120px,0.8fr)_minmax(220px,1.4fr)_minmax(18rem,max-content)]',
  },
  volumes: {
    minWidth: 'min-w-[760px]',
    grid: 'grid-cols-[minmax(260px,2fr)_minmax(140px,1fr)_minmax(200px,1.5fr)_4rem]',
  },
  projects: {
    minWidth: 'min-w-[1080px]',
    grid: 'grid-cols-[minmax(220px,1.5fr)_minmax(140px,1fr)_minmax(260px,2fr)_minmax(180px,1.5fr)_11rem]',
  },
  images: {
    minWidth: 'min-w-[920px]',
    grid: 'grid-cols-[minmax(260px,3fr)_7rem_8rem_minmax(180px,1.5fr)_8rem]',
  },
  builds: {
    minWidth: 'min-w-[940px]',
    grid: 'grid-cols-[minmax(220px,1.5fr)_minmax(150px,1fr)_7rem_minmax(180px,1.5fr)_10rem]',
  },
  adminUsers: {
    minWidth: 'min-w-[1040px]',
    grid: 'grid-cols-[minmax(180px,1.5fr)_minmax(260px,2fr)_7rem_7rem_minmax(180px,1.5fr)_7rem]',
  },
  adminContainers: {
    minWidth: 'min-w-[1120px]',
    grid: 'grid-cols-[minmax(260px,2fr)_minmax(160px,1fr)_minmax(260px,2fr)_19rem]',
  },
  adminVolumes: {
    minWidth: 'min-w-[760px]',
    grid: 'grid-cols-[minmax(360px,3fr)_minmax(220px,1.5fr)_4rem]',
  },
  adminImages: {
    minWidth: 'min-w-[860px]',
    grid: 'grid-cols-[minmax(360px,3fr)_9rem_minmax(220px,1.5fr)_4rem]',
  },
  adminBuilds: {
    minWidth: 'min-w-[1040px]',
    grid: 'grid-cols-[minmax(180px,1.5fr)_minmax(130px,1fr)_minmax(130px,1fr)_minmax(180px,1.5fr)_minmax(170px,1.5fr)_10rem]',
  },
  adminProjects: {
    minWidth: 'min-w-[900px]',
    grid: 'grid-cols-[minmax(260px,2fr)_minmax(160px,1fr)_minmax(260px,2fr)_11rem]',
  },
} as const;
