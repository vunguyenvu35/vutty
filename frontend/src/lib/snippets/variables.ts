/** A snippet may have blanks to fill in when it runs: {{name}}, or {{name|default}} with a value that is used when the box is left empty. */
export interface Variable {
  name: string
  /** what an empty answer becomes ('' when the snippet gives none) */
  def: string
}

/** The names a run on several hosts fills in by itself, one value per host. */
export const HOST_VARIABLES = ['host', 'hostname', 'user']

const BLANK = /\{\{\s*([A-Za-z_][A-Za-z0-9_-]*)\s*(?:\|([^}]*))?\}\}/g

/**
 * The blanks of a script, once each and in the order they first appear. {{.Names}} and {{json .}} (Go templates) are not
 * blanks. `skipHost` leaves out the per-host names, for a run on several hosts that fills them in.
 */
export function parseVariables(script: string, skipHost = false): Variable[] {
  const out: Variable[] = []
  for (const m of script.matchAll(BLANK)) {
    const name = m[1]
    if (skipHost && HOST_VARIABLES.includes(name)) continue
    if (!out.some(v => v.name === name)) out.push({name, def: (m[2] ?? '').trim()})
  }
  return out
}

/** The script with every blank replaced by its answer (or its default when the answer is empty). Names in `keep` stay as they are. */
export function fillVariables(script: string, values: Record<string, string>, keep: string[] = []): string {
  return script.replace(BLANK, (whole, name: string, def?: string) => {
    if (keep.includes(name)) return whole
    const v = values[name]
    return v !== undefined && v !== '' ? v : (def ?? '').trim()
  })
}
