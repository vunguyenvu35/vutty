import {useCallback, useEffect, useMemo, useState} from 'react'
import {useBackend} from '../backend'
import {useToast} from '../ui/Toast'
import {useConfirm} from '../ui/Confirm'
import {errMsg} from '../util'
import {useMinBusy} from '../ui/useMinBusy'
import {IconCopy, IconFingerprint, IconRefresh, IconTrash} from '../ui/Icons'
import {byName, FilterChip, ListToolbar, usePref, VIEWS, type SortOption} from '../ui/ListTools'
import type {KnownHost} from '../types'
import {useT, type TFn} from '../i18n'
import {rich} from './rich'

type Sort = 'host' | 'host-desc' | 'type'
const SORT_VALUES: Sort[] = ['host', 'host-desc', 'type']
const sortOptions = (t: TFn): SortOption<Sort>[] => [{value: 'host', label: t('Host A–Z')}, {value: 'host-desc', label: t('Host Z–A')}, {value: 'type', label: t('Key type')}]

/**
 * Known Hosts: the machines whose key you trusted (first connection, or the trust prompt). Forgetting one
 * does not delete the host from the app: the next connection simply asks to trust its key again, which is
 * what to do after a server was reinstalled and legitimately has a new key.
 */
export function KnownHostsPage() {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [items, setItems] = useState<KnownHost[] | null>(null)
  const [q, setQ] = useState('')
  const [error, setError] = useState('')
  const [types, setTypes] = useState<string[]>([])
  const [sort, setSort] = usePref<Sort>('vutty.knownhosts.sort', SORT_VALUES, 'host')
  const [view, setView] = usePref('vutty.knownhosts.view', VIEWS, 'grid')

  const [loading, setLoading] = useState(false)
  const spinning = useMinBusy(loading)
  const reload = useCallback(async () => {
    setLoading(true)
    try {
      setItems((await be.knownHosts.list()) ?? [])
      setError('')
    } catch (e) {
      setItems([])
      setError(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [be])
  useEffect(() => {
    void reload()
    return be.on('log:added', () => void reload()) // a new key was trusted, or one was forgotten
  }, [be, reload])

  const presentTypes = useMemo(() => Array.from(new Set((items ?? []).map(k => k.key_type))).sort(byName), [items])
  const activeTypes = types.filter(ty => presentTypes.includes(ty))
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const list = (items ?? []).filter(k => (activeTypes.length === 0 || activeTypes.includes(k.key_type)) && (!needle || (k.host + ' ' + k.key_type + ' ' + k.fingerprint).toLowerCase().includes(needle)))
    return [...list].sort((a, b) => (sort === 'host-desc' ? byName(b.host, a.host) : sort === 'type' ? byName(a.key_type, b.key_type) || byName(a.host, b.host) : byName(a.host, b.host)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, q, sort, activeTypes.join(',')])

  const forget = async (k: KnownHost) => {
    if (!(await confirm(t('Forget {host}? Vutty will ask whether to trust its key again the next time you connect.', {host: k.host}), {confirmLabel: t('Forget'), danger: true}))) return
    try {
      await be.knownHosts.forget(k.host, k.fingerprint)
      toast.info(t('Forgot {host}', {host: k.host}))
      await reload()
    } catch (e) {
      toast.error(errMsg(e))
    }
  }
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      toast.info(t('Fingerprint copied'))
    } catch {
      toast.error(t('Clipboard not available'))
    }
  }

  return (
    <div className="hosts-page" aria-label={t('Known hosts')}>
      <ListToolbar what={t('known hosts')} query={q} onQuery={setQ} sort={sort} sortOptions={sortOptions(t)} onSort={setSort} view={view} onView={setView}>
        {presentTypes.length > 1 && <div className="chips">{presentTypes.map(ty => <FilterChip key={ty} label={ty} on={activeTypes.includes(ty)} onToggle={() => setTypes(activeTypes.includes(ty) ? activeTypes.filter(x => x !== ty) : [...activeTypes, ty])} />)}</div>}
        <button className="ghost-btn" aria-label={t('Refresh known hosts')} aria-busy={spinning} onClick={reload}><IconRefresh size={14} /> {t('Refresh')}</button>
      </ListToolbar>

      <h3 className="sec-title">{t('Known Hosts')}</h3>
      <div className={'cards' + (view === 'list' ? ' as-list' : '')} role="list" aria-label={t('Known hosts')} data-view={view}>
        {shown.map(k => (
          <div key={k.host + k.fingerprint} className="card" role="listitem">
            <span className="tile key-tile"><IconFingerprint size={20} /></span>
            <div className="meta">
              <div className="label">{k.host}</div>
              <div className="addr" title={k.fingerprint}>{k.key_type} · {k.fingerprint}</div>
            </div>
            <div className="actions">
              <button className="icon ghost" aria-label={t('Copy fingerprint of {host}', {host: k.host})} title={t('Copy fingerprint')} onClick={() => copy(k.fingerprint)}><IconCopy size={14} /></button>
              <button className="icon ghost danger" aria-label={t('Forget {host}', {host: k.host})} title={t('Forget this host key')} disabled={k.host === '(hashed)'} onClick={() => forget(k)}><IconTrash size={14} /></button>
            </div>
          </div>
        ))}
      </div>
      {error && <div className="error" role="alert">{error}</div>}
      {items && items.length === 0 && !error && <div className="empty-hint">{t('No trusted hosts yet. A host appears here the first time you trust its key.')}</div>}
      {items && items.length > 0 && shown.length === 0 && <div className="empty-hint">{t('No host matches.')}</div>}
      {items?.some(k => k.host === '(hashed)') && <div className="muted fw-hint">{rich(t('Hashed entries (from {option}) cannot be named, so they cannot be forgotten here.'), {option: <code>HashKnownHosts</code>})}</div>}
    </div>
  )
}
