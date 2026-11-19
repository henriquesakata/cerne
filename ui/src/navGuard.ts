// navGuard (F-0464): the one question the hash router asks before it leaves
// the current route — today, the vault editor's unsaved draft. It sits in the
// main bundle (App reads it); the editor, in the vault chunk, sets it.
let guard: (() => boolean) | null = null

export function setNavGuard(fn: (() => boolean) | null): void {
  guard = fn
}

// navAllowed is true when no guard is set or the guard lets the route go.
export function navAllowed(): boolean {
  return guard ? guard() : true
}
