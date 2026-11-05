import {createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode, useLayoutEffect} from 'react'
import {useT, type TFn} from '../i18n'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {useBackend, type Backend} from '../backend'
import {useLoad} from './shared'
import type {Host, K8sResRef} from '../types'

export type K8sView = 'overview' | 'pods' | 'workloads' | 'network' | 'config' | 'apps' | 'resources' | 'security' | 'nodes' | 'events'
/** a page that takes the place of a view (with a way back): a pod, a deployment, a YAML, a drain */
export type K8sPage =
  | {k: 'pod'; ns: string; name: string}
  | {k: 'deploy'; ns: string; name: string}
  /** ref: any kind of the cluster (the Resources view); without it kind is one of the fixed kinds */
  | {k: 'yaml'; kind: string; ns: string; name: string; label: string; back: K8sPage | null; ref?: K8sResRef}
  | {k: 'helm'; ns: string; name: string; rev: number}
  | {k: 'drain'; node: string}

/** what the Logs tab is asked to open: one pod (and its container), or every pod of a workload or Service (kind) */
export interface K8sLogTarget {ns: string; name: string; container?: string; previous?: boolean; kind?: 'deployment' | 'statefulset' | 'daemonset' | 'job' | 'service'}

export interface K8sApi {
  be: Backend
  sid: string
  host: Host
  /** a cluster opened with a kubeconfig: no host to run kubectl on: the shell and the port-forward go through the API server */
  direct: boolean
  ctx: string
  /** '' = all namespaces */
  ns: string
  /** rights of this login in the namespace; null until known (then nothing is locked: the cluster still decides) */
  rights: Record<string, boolean> | null
  visible: boolean
  /** a number that grows after every change: a view that shows the cluster asks again */
  rev: number
  can(right: string): boolean
  busy(id: string): boolean
  /** runs a change: its button is busy meanwhile, the error goes to a toast, the views ask again at the end */
  run(id: string, fn: () => Promise<unknown>, done?: string): Promise<boolean>
  go(view: K8sView, filter?: string): void
  /** what a screen keeps while it is not in sight or is built again (a filter, a closed group): changing the namespace builds the screens again */
  kept: Record<string, unknown>
  keep(key: string, value: unknown): void
  setNs(ns: string): void
  open(page: K8sPage | null): void
  onShell?(host: Host, command: string, title: string): void
  /** a cluster opened with a kubeconfig opens a shell as a terminal tab of its own: the id of that terminal and a name for the tab */
  onPodShell?(hostId: string, title: string, prod: boolean): void
  onAccess?(): void
  onLogs(pod: K8sLogTarget): void
}

const Ctx = createContext<K8sApi | null>(null)
export const K8sProvider = Ctx.Provider
export function useK8s(): K8sApi {
  const v = useContext(Ctx)
  if (!v) throw new Error('K8sProvider is missing')
  return v
}

/** State that outlives the screen: a search typed in, a switch, a closed group stay when the namespace is changed or another view is looked at. */
export function useKept<T>(key: string, initial: T): [T, (v: T) => void] {
  const k = useK8s()
  const {keep} = k
  const value = (key in k.kept ? k.kept[key] : initial) as T
  return [value, useCallback((v: T) => keep(key, v), [keep, key])]
}

/** the busy buttons of the tab and the runner of a change: they live above the views, so a change goes on (and its button stays locked) while another view is open */
export function useRunner(onDone: () => void) {
  const toast = useToast()
  const [busy, setBusy] = useState<string[]>([])
  const alive = useRef(true)
  useEffect(() => { alive.current = true; return () => { alive.current = false } }, [])
  const run = useCallback(async (id: string, fn: () => Promise<unknown>, done?: string) => {
    setBusy(b => [...b, id])
    let ok = true
    try { await fn(); if (done) toast.info(done) } catch (e) { ok = false; toast.error(errMsg(e)) }
    if (alive.current) { setBusy(b => { const i = b.indexOf(id); return i < 0 ? b : [...b.slice(0, i), ...b.slice(i + 1)] }); onDone() }
    return ok
  }, [toast, onDone])
  return {busy: useCallback((id: string) => busy.includes(id), [busy]), run}
}

/** why a button is locked: the right this login lacks */
export function lockText(t: TFn, right: string, direct = false): string {
  switch (right) {
    case 'logs': return t('This login may not read logs')
    case 'exec': return t('This login may not open a shell in pods')
    case 'portforward': return t('This login may not forward ports')
    case 'debug': return t('This login may not add a debug container to pods')
    case 'scale': return t('This login may not scale Deployments')
    case 'edit': return t('This login may not edit this workload')
    case 'restart': return t('This login may not restart or roll back Deployments')
    case 'delete_pod': return t('This login may not delete pods')
    case 'cronjob_run': return t('This login may not run CronJobs')
    case 'cronjob_patch': return t('This login may not suspend CronJobs')
    case 'cordon': return t('This login may not cordon nodes')
    case 'drain': return t('This login may not drain nodes')
    case 'secrets': return t('This login may not get secrets')
    default: return t('This login may not list these objects')
  }
}

/** A button of a view: busy and locked for its own id while its command runs; locked, with the missing right as tooltip, when the login may not */
export function ActBtn({id, right, className = '', title, disabled, onClick, children, label}: {id?: string; right?: string; className?: string; title?: string; disabled?: boolean; onClick(): void; children: ReactNode; label?: string}) {
  const k = useK8s()
  const t = useT()
  const busy = !!id && k.busy(id)
  const locked = !!right && !k.can(right)
  return (
    <button className={className} aria-busy={busy} aria-label={label} disabled={busy || locked || disabled} title={locked ? lockText(t, right!, k.direct) : title} onClick={onClick}>{children}</button>
  )
}

/** where a button's popup menu (position: fixed) goes: below the button, else above it (the last row of a table sits at the bottom of the window), never out of the window */
export function useMenuAnchor() {
  const btn = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const [at, setAt] = useState<{left: number; top: number; above: number} | null>(null)
  const [fit, setFit] = useState<{top: number; maxHeight: number} | null>(null)
  useLayoutEffect(() => {
    if (!at || !menu.current) { setFit(null); return }
    const h = menu.current.offsetHeight
    const below = window.innerHeight - 8 - at.top, up = at.above - 4 - 8
    if (h <= below || up <= below) setFit({top: at.top, maxHeight: below})
    else setFit({top: Math.max(8, at.above - 4 - Math.min(h, up)), maxHeight: up})
  }, [at])
  const toggle = () => { const r = btn.current?.getBoundingClientRect(); setAt(a => (a ? null : {left: Math.max(8, (r?.right ?? 300) - 300), top: (r?.bottom ?? 0) + 4, above: r?.top ?? 0})) }
  const style = at ? {left: at.left, top: fit?.top ?? at.top, maxHeight: fit?.maxHeight, visibility: fit ? 'visible' as const : 'hidden' as const} : undefined
  return {btn, menu, at, setAt, toggle, style}
}

/** useLoad that also asks again after a change (rev) */
export function useKLoad<T>(load: () => Promise<T>, poll: boolean) {
  const k = useK8s()
  const r = useLoad<T>(load, poll && k.visible ? 5000 : null)
  const first = useRef(k.rev)
  useEffect(() => { if (first.current !== k.rev) { first.current = k.rev; r.reload() } }, [k.rev]) // eslint-disable-line react-hooks/exhaustive-deps
  return r
}

/** "40s", "11m", "9h", "17d": the biggest unit of a time span, as the boards write it */
export function age(sec: number): string {
  if (sec < 60) return Math.max(0, Math.round(sec)) + 's'
  if (sec < 3600) return Math.floor(sec / 60) + 'm'
  if (sec < 86400) return Math.floor(sec / 3600) + 'h'
  return Math.floor(sec / 86400) + 'd'
}

export const Pill = ({tone = '', children, title}: {tone?: string; children: ReactNode; title?: string}) => <span className={'sp-pill ' + tone} title={title}>{children}</span>

/** a sortable table: numbers start with the biggest, text with A to Z; a second click turns it round (like the Processes tab) */
export function useSort<K extends string>(text: K[]) {
  const t = useT()
  const [sort, setSort] = useState<{key: K; dir: 1 | -1} | null>(null)
  const sortBy = (key: K) => setSort(s => (s?.key === key ? {key, dir: s.dir === 1 ? -1 : 1} : {key, dir: text.includes(key) ? 1 : -1}))
  const th = (key: K, label: string, cls?: string) => (
    <th className={cls} aria-sort={sort?.key === key ? (sort.dir === 1 ? 'ascending' : 'descending') : 'none'}>
      <button className="sp-sort" title={t('Sort by {column}', {column: label})} onClick={() => sortBy(key)}>{label}{sort?.key === key && <span aria-hidden="true">{sort.dir === 1 ? ' ▲' : ' ▼'}</span>}</button>
    </th>
  )
  const apply = <T,>(list: T[], value: (x: T, key: K) => string | number): T[] => {
    if (!sort) return list
    const {key, dir} = sort
    return [...list].sort((a, b) => { const x = value(a, key), y = value(b, key); return dir * (typeof x === 'string' && typeof y === 'string' ? x.localeCompare(y) : Number(x) - Number(y)) })
  }
  return {sort, th, apply}
}

/** a percent as a small bar with its number; null is "no data" */
export function Meter({value, label}: {value: number | null; label: string}) {
  const t = useT()
  if (value === null) return <span className="sp-note">{t('no data')}</span>
  return (
    <div className="k8s-meter">
      <div className={'sp-track ' + (value >= 90 ? 'bad' : value >= 75 ? 'warn' : 'ok')} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value}><i style={{width: value + '%'}} /></div>
      <span>{value}%</span>
    </div>
  )
}

/** the way back and where this page is: "Pods › shop › api-7d9f6c" */
export function Crumb({path, onBack}: {path: string[]; onBack(): void}) {
  const t = useT()
  return (
    <nav className="k8s-crumb" aria-label={t('Where you are')}>
      <button className="sp-link" onClick={onBack}>{path[0]}</button>
      {path.slice(1).map((p, i) => <span key={i}><span aria-hidden="true"> › </span>{p}</span>)}
    </nav>
  )
}

/** a filter box that matches the words typed against the text of each row */
export const matches = (q: string, ...fields: string[]) => {
  const s = q.trim().toLowerCase()
  return !s || fields.some(f => f.toLowerCase().includes(s))
}
