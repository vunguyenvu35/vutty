/**
 * Commands typed in the terminals, newest first, kept for each host separately: what was typed on this
 * computer is no use at a server, and the other way round. They feed the command suggestions and the History tab
 * of the terminal sidebar. They stay in this browser profile only (localStorage). Only lines that were echoed
 * back by the terminal are kept, so what is typed at a password prompt (which the shell does not echo) never ends
 * up here.
 *
 * A host is the saved host's id, or 'local' / 'serial:…' / 'telnet:…' for the other kinds of terminal.
 */
const KEY = 'vutty.history.v2'
export const MAX_HISTORY = 300
/** hosts that keep a history: the least recently used one is dropped beyond this (hosts get deleted) */
export const MAX_HOSTS = 200
const MAX_LINE = 2000

type ByHost = Record<string, string[]>

function load(): ByHost {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '{}')
    if (!v || typeof v !== 'object' || Array.isArray(v)) return {}
    const out: ByHost = {}
    for (const [host, list] of Object.entries(v)) {
      if (Array.isArray(list)) out[host] = list.filter((x): x is string => typeof x === 'string').slice(0, MAX_HISTORY)
    }
    return out
  } catch {
    return {}
  }
}

let byHost = load()
const NONE: string[] = []
const subs = new Set<() => void>()
const save = () => {
  try { localStorage.setItem(KEY, JSON.stringify(byHost)) } catch { /* private window: keep it in memory */ }
  subs.forEach(f => f())
}

/** The commands typed at this host, newest first. The same array is returned until it changes. */
export const getHistory = (host: string): string[] => byHost[host] ?? NONE

export function subscribeHistory(f: () => void): () => void {
  subs.add(f)
  return () => { subs.delete(f) }
}

/** Adds a command at the top of a host's history; the same command typed again moves up instead of repeating. */
export function recordCommand(host: string, line: string) {
  const l = line.trim()
  if (!host || !l || l.length > MAX_LINE) return
  const list = [l, ...(byHost[host] ?? NONE).filter(x => x !== l)].slice(0, MAX_HISTORY)
  const next: ByHost = {...byHost}
  delete next[host] // re-inserted last: the keys stay in order of use
  next[host] = list
  const hosts = Object.keys(next)
  for (const old of hosts.slice(0, Math.max(0, hosts.length - MAX_HOSTS))) delete next[old]
  byHost = next
  save()
}

/** Forgets one host's history (or all of them when no host is given). */
export function clearHistory(host?: string) {
  if (host === undefined) byHost = {}
  else { byHost = {...byHost}; delete byHost[host] }
  save()
}
