// The browser-ready ELK build ships no co-located types; re-export the typed
// ELK class from elkjs's API declaration so Graph.tsx stays fully typed.
declare module 'elkjs/lib/elk.bundled.js' {
  import ELK from 'elkjs/lib/elk-api'
  export default ELK
}
