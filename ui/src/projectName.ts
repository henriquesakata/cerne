// The project-name rules (F-0331), mirroring app.RenameProject so a refusal
// reads in the UI's language; the server stays the authority (a divergence
// shows its 400). internal/app/testdata/project_names.json keeps both in step.
export type NameRule = 'empty' | 'long' | 'control'

// The trim set: JS \s plus NEL, which is Go's unicode.IsSpace plus the BOM.
const EDGES = /^[\s\u0085]+|[\s\u0085]+$/gu
// A letter, number, punctuation or symbol (emoji included): a zero-width-only
// name is empty.
const VISIBLE = /[\p{L}\p{N}\p{P}\p{S}]/u
// Control characters, line/paragraph separators and bidi controls: each forges
// structure in CONTEXT.md and prompts, or reorders what the selector shows.
const BREAKING = /[\p{Cc}\p{Zl}\p{Zp}\p{Bidi_Control}]/u

export function checkProjectName(raw: string): { name: string; rule: NameRule | null } {
  const name = raw.replace(EDGES, '')
  if (!VISIBLE.test(name)) return { name, rule: 'empty' }
  if ([...name].length > 64) return { name, rule: 'long' }
  if (BREAKING.test(name)) return { name, rule: 'control' }
  return { name, rule: null }
}
