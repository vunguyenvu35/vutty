import {t} from '../i18n'

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`
}

export function formatDate(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, {year: 'numeric', month: 'short', day: 'numeric'}) + ' ' + d.toLocaleTimeString(undefined, {hour: '2-digit', minute: '2-digit'})
}

const WINDOWS_PATH = /^([A-Za-z]:[\\/]|[A-Za-z]:$|\\\\)/

/** True for a Windows path (drive letter or UNC): the local pane on Windows. Remote paths are always POSIX. */
export const isWindowsPath = (p: string) => WINDOWS_PATH.test(p)

/** True for the top of a filesystem: "/", "C:\\", or a UNC share root. Nothing is above it. */
export function isRoot(path: string): boolean {
  if (!isWindowsPath(path)) return path === '/' || path === ''
  return /^[A-Za-z]:[\\/]*$/.test(path) || /^\\\\[^\\/]+[\\/]+[^\\/]+[\\/]*$/.test(path)
}

/** Whether a path typed in a "go to" box is complete rather than relative to the current folder. */
export const isAbsolutePath = (p: string) => p.startsWith('/') || isWindowsPath(p)

/** "/a/b/c" -> [{name:'/', path:'/'}, {name:'a', path:'/a'}, ...]; "C:\\a\\b" -> [C:\\, a, b]. */
export function breadcrumbs(path: string): {name: string; path: string}[] {
  if (isWindowsPath(path)) {
    const m = /^([A-Za-z]:|\\\\[^\\/]+[\\/]+[^\\/]+)(.*)$/.exec(path)
    if (m) {
      const root = m[1].startsWith('\\\\') ? m[1] : m[1] + '\\'
      const out = [{name: root, path: root}]
      let acc = m[1]
      for (const part of m[2].split(/[\\/]+/).filter(Boolean)) {
        acc += '\\' + part
        out.push({name: part, path: acc})
      }
      return out
    }
  }
  const out = [{name: '/', path: '/'}]
  let acc = ''
  for (const part of path.split('/').filter(Boolean)) {
    acc += '/' + part
    out.push({name: part, path: acc})
  }
  return out
}

export function parentOf(path: string): string {
  if (isWindowsPath(path)) {
    if (isRoot(path)) return path
    const trimmed = path.replace(/[\\/]+$/, '')
    const i = Math.max(trimmed.lastIndexOf('\\'), trimmed.lastIndexOf('/'))
    const dir = trimmed.slice(0, i)
    return /^[A-Za-z]:$/.test(dir) ? dir + '\\' : dir // C:\a goes to C:\, not C:
  }
  const i = path.replace(/\/+$/, '').lastIndexOf('/')
  return i <= 0 ? '/' : path.slice(0, i)
}

export function joinPath(dir: string, name: string): string {
  if (isWindowsPath(dir)) return dir.replace(/[\\/]+$/, '') + '\\' + name
  return (dir === '/' ? '' : dir.replace(/\/+$/, '')) + '/' + name
}

/** A file or folder name must be one path segment. */
export function validName(name: string, dir = ''): string | null {
  const n = name.trim()
  if (!n) return t('Enter a name')
  if (n === '.' || n === '..') return t('Choose a different name')
  if (/[/\0]/.test(n)) return t('A name cannot contain "/"')
  if (isWindowsPath(dir) && /[\\<>:"|?*]/.test(n)) return t('A name cannot contain any of \\ < > : " | ? *')
  return null
}

/** The "Kind" column: folder, or the upper-cased extension, or plain "file". */
export function kindOf(e: {name: string; is_dir: boolean}): string {
  if (e.is_dir) return t('folder')
  const i = e.name.lastIndexOf('.')
  return i > 0 && i < e.name.length - 1 ? e.name.slice(i + 1).toUpperCase() : t('file')
}

/**
 * The line that makes a terminal change into `dir`. A Windows path (drive letter or UNC) gets PowerShell
 * syntax, with -LiteralPath so [brackets] are not read as wildcards; everything else is a POSIX shell.
 */
export function cdCommand(dir: string): string {
  if (WINDOWS_PATH.test(dir)) return `cd -LiteralPath '${dir.replace(/'/g, "''")}'`
  return `cd '${dir.replace(/'/g, "'\\''")}'`
}

/** The folder a "go to" on this entry means: a folder itself, a file's parent. Understands both kinds of path. */
export function folderOf(e: {path: string; is_dir: boolean}): string {
  if (e.is_dir) return e.path
  if (!WINDOWS_PATH.test(e.path)) return parentOf(e.path)
  const i = Math.max(e.path.lastIndexOf('\\'), e.path.lastIndexOf('/'))
  const dir = e.path.slice(0, i)
  return /^[A-Za-z]:$/.test(dir) ? dir + e.path[i] : dir // C:\file.txt goes to C:\, not C:
}
