// queryRoots are the root query keys of resources whose data another feature
// shows. A feature invalidates a root it does not own through this map, so it
// does not have to import that feature: container changes refresh projects,
// which contain containers, even though containers may not import projects;
// a created container refreshes the dashboard's figures, which reserve memory.
export const queryRoots = {
  containers: ['containers'],
  projects: ['projects'],
  dashboard: ['dashboard'],
} as const;
