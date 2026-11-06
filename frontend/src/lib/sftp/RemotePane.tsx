import {ReactNode, useEffect, useMemo, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {errMsg} from '../util'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {IconFolder} from '../ui/Icons'
import {OsIcon} from '../ui/OsIcon'
import {joinPath} from './format'
import {hostLabel, LOCAL_HOST, LOCAL_HOST_ID, localPaneFs} from './localFs'
import {FilePane, PaneFs, PaneState, DragPayload, Item} from './FilePane'
import {remoteFs, watchEdits} from './remoteFs'
import {AuthPrompt} from '../ui/AuthPrompt'
import type {AuthChallenge, EditEvent, Host, HostKeyPrompt} from '../types'

export interface RemoteHost {id: string; label: string}
let seq = 0
const HOST_MARK = '\u0001'
/** Puts the host name, in bold, where the translated sentence has its mark. */
export function authenticity(text: string, host: string): ReactNode {
  const [a, b = ''] = text.split(HOST_MARK)
  return <>{a}<b>{host}</b>{b}</>
}

interface Props {
  host: RemoteHost | null
  /** the folder to open first (the home folder when absent) */
  startPath?: string
  hosts: Host[]
  onHost(h: RemoteHost | null): void
  /** a host was picked while one is open: it becomes one more tab of this pane */
  onNewTab?(h: RemoteHost): void
  /** the host tabs shown in the header; `add` opens the host list */
  tabsLead?(add: () => void): ReactNode
  /** another host's tab is showing: keep this connection alive but out of sight */
  hidden?: boolean
  reloadKey: number
  onState(s: PaneState): void
  onDropEntry(p: DragPayload): void
  /** the SFTP session id once connected ('' when not) */
  onSession(sid: string): void
  /** right-click: copy what is chosen to the folder open on this computer */
  onCopyOut?(items: Item[]): void
}

/**
 * The Remote side: shows "Connect to host" until a saved host is chosen, then owns that host's SFTP
 * connection (through its jump chain, with the host key prompt) and browses it in a FilePane.
 */
export function RemotePane({host, startPath, hosts, onHost, onNewTab, tabsLead, hidden, reloadKey, onState, onDropEntry, onSession, onCopyOut}: Props) {
  const be = useBackend()
  const t = useT()
  const confirm = useConfirm()
  const toast = useToast()
  const sid = useRef('')
  const edited = useRef(new Set<string>()) // remote files being edited here: each is released when the connection ends
  const alive = useRef(true)
  const [state, setState] = useState<'connecting' | 'ready' | 'closed'>('connecting')
  const [home, setHome] = useState('')
  const [closedReason, setClosedReason] = useState('')
  const [prompt, setPrompt] = useState<{key: string; p: HostKeyPrompt} | null>(null)
  const [auth, setAuth] = useState<{key: string; c: AuthChallenge} | null>(null)
  const [attempt, setAttempt] = useState(0)
  const [picking, setPicking] = useState(false)
  const [filter, setFilter] = useState('')
  const [fs, setFs] = useState<PaneFs | null>(null)
  // the folder to open first: the one asked for when it can be listed, else the home folder
  const [start, setStart] = useState('')
  useEffect(() => {
    if (!fs || !home) { setStart(''); return }
    if (!startPath) { setStart(home); return }
    let on = true
    fs.list(startPath).then(() => { if (on) setStart(startPath) }, () => { if (on) { toast.error(t('{path} cannot be opened here: showing the home folder.', {path: startPath})); setStart(home) } })
    return () => { on = false }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fs, home, startPath])

  const hostId = host?.id ?? ''
  const isLocal = hostId === LOCAL_HOST_ID
  useEffect(() => {
    setFs(null)
    setHome('')
    onSession('')
    if (!hostId) return
    alive.current = true
    if (hostId === LOCAL_HOST_ID) {
      // this computer is no connection: the pane just browses local files, and a transfer into or out of it is a copy
      setState('connecting')
      setClosedReason('')
      void be.local.home().then(h => {
        if (!alive.current) return
        setFs(localPaneFs(be))
        setHome(h)
        setState('ready')
      }).catch(e => { if (alive.current) { setState('closed'); setClosedReason(errMsg(e)) } })
      return () => { alive.current = false }
    }
    const key = `sftp-${Date.now().toString(36)}-${++seq}`
    const offs: Array<() => void> = [be.on(`term:hostkey:${key}`, (p: HostKeyPrompt) => setPrompt({key, p})), be.on(`term:auth:${key}`, (c: AuthChallenge) => setAuth({key, c}))]
    setState('connecting')
    setClosedReason('')
    ;(async () => {
      try {
        const info = await be.sftp.open(hostId, key)
        if (!alive.current) { void be.sftp.close(info.sid).catch(() => {}); return }
        sid.current = info.sid
        offs.push(watchEdits(be, t, info.sid, toast, confirm))
        offs.push(be.on(`sftp:closed:${info.sid}`, () => { setState('closed'); setClosedReason(t('The connection was lost.')); sid.current = ''; setFs(null); onSession('') }))
        setPrompt(null)
        setAuth(null)
        const s = info.sid
        setFs(remoteFs(be, t, s, edited.current))
        setHome(info.home || '/')
        setState('ready')
        onSession(s)
      } catch (e) {
        if (alive.current) { setPrompt(null); setAuth(null); setState('closed'); setClosedReason(errMsg(e)) }
      }
    })()
    return () => {
      alive.current = false
      offs.forEach(o => o())
      if (sid.current) {
        for (const p of edited.current) void be.sftp.editStop(sid.current, p).catch(() => {})
        void be.sftp.close(sid.current).catch(() => {})
      }
      edited.current.clear()
      sid.current = ''
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [be, hostId, attempt])

  const answer = (trust: boolean) => {
    if (!prompt) return
    void be.session.trustHostKey(prompt.key, trust ? prompt.p.fingerprint : '').catch(e => setClosedReason(errMsg(e)))
    setPrompt(null)
  }

  const matching = useMemo(() => hosts.filter(h => (h.label + ' ' + h.hostname).toLowerCase().includes(filter.trim().toLowerCase())), [hosts, filter])
  const showLocal = ('this computer local ' + hostLabel(LOCAL_HOST)).includes(filter.trim().toLowerCase())
  const choose = (r: RemoteHost) => { setPicking(false); setFilter(''); if (host && onNewTab) onNewTab(r); else onHost(r) }

  const picker = (
    <div className="host-picker">
      <h3>{t('Select host')}</h3>
      <input aria-label={t('Filter hosts')} autoFocus placeholder={t('Search hosts')} value={filter} onChange={e => setFilter(e.target.value)} />
      <div className="host-picker-list">
        {showLocal && (
          <button aria-label={t('Open This computer')} onClick={() => choose(LOCAL_HOST)}>
            <IconFolder size={18} /><span>{hostLabel(LOCAL_HOST)}</span><small className="muted">{t('Browse and copy files on this computer')}</small>
          </button>
        )}
        {matching.map(h => (
          <button key={h.id} aria-label={t('Connect to {name}', {name: h.label})} onClick={() => choose({id: h.id, label: h.label})}>
            <OsIcon os={h.os} size={18} /><span>{h.label}</span><small className="muted">{h.username ? h.username + '@' : ''}{h.hostname}</small>
          </button>
        ))}
        {matching.length === 0 && (hosts.length ? !showLocal && <div className="muted">{t('No host matches.')}</div> : <div className="muted">{t('No saved hosts yet. Add one in Hosts first.')}</div>)}
      </div>
      <button onClick={() => { setPicking(false); setFilter('') }}>{t('Cancel')}</button>
    </div>
  )

  if (!host) {
    return (
      <section className="fpane" aria-label={t('Remote')}>
        <div className="fpane-empty">
          {!picking ? (
            <>
              <span className="fpane-empty-icon"><IconFolder size={28} /></span>
              <h3>{t('Connect to host')}</h3>
              <p className="muted">{t('Start by connecting to a saved host to manage your files with SFTP.')}</p>
              <button className="primary" onClick={() => setPicking(true)}>{t('Select host')}</button>
            </>
          ) : picker}
        </div>
      </section>
    )
  }

  return (
    <div className="rpane" style={hidden ? {display: 'none'} : undefined}>
      <FilePane side="remote" title={hostLabel(host)} icon={isLocal ? <IconFolder size={16} /> : <OsIcon os={hosts.find(x => x.id === host.id)?.os} size={16} />} fs={fs} startDir={start} reloadKey={reloadKey} showHiddenDefault={!isLocal}
        onState={onState} onDropEntry={onDropEntry} onCopyOut={onCopyOut} copyLabel={isLocal ? t('Copy to Local') : t('Download')} onClose={() => onHost(null)} onAdd={() => setPicking(true)} lead={tabsLead?.(() => setPicking(true))} />
      {picking && <div className="overlay" role="dialog" aria-label={t('Select host')}><div className="card">{picker}</div></div>}
      {state === 'connecting' && !prompt && !auth && <div className="banner info">{t('Connecting to {name}…', {name: hostLabel(host)})}</div>}
      <AuthPrompt challenge={auth?.c ?? null} promptKey={auth?.key ?? ''} onDone={() => setAuth(null)} onError={setClosedReason} />
      {state === 'closed' && (
        <div className="banner" role="alert">
          <span>{closedReason || t('Disconnected.')}</span>
          <button className="on-banner" onClick={() => setAttempt(a => a + 1)}>{t('Reconnect')}</button>
        </div>
      )}
      {prompt && (
        <div className="overlay" role="alertdialog" aria-label={t('Host key confirmation')}>
          <div className="card">
            <h3 style={{marginTop: 0}}>{t('Unknown host key')}</h3>
            <div>{authenticity(t("The authenticity of {host} can't be established.", {host: HOST_MARK}), prompt.p.host)}</div>
            <div className="muted">{t('{type} fingerprint', {type: prompt.p.key_type})}</div>
            <div className="fp" data-testid="fingerprint">{prompt.p.fingerprint}</div>
            <div className="row">
              <button className="primary" onClick={() => answer(true)}>{t('Trust and connect')}</button>
              <button onClick={() => answer(false)}>{t('Cancel')}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
