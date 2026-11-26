// Scope selects whose resources a request reads or changes: the signed-in
// user's own resources, or every user's through the admin endpoints.
export type Scope = 'own' | 'all';

// scopedPath returns the endpoint for a resource path in a scope. The admin
// endpoints serve the same paths under the /admin prefix.
export function scopedPath(scope: Scope, path: string) {
  return scope === 'all' ? `/admin${path}` : path;
}
