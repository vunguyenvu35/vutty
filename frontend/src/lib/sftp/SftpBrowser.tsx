import {useCallback, useEffect, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useToast} from '../ui/Toast'
import {useConfirm} from '../ui/Confirm'
import {errMsg} from '../util'
import {IconClose, IconDownload, IconFolder, IconPlus} from '../ui/Icons'
import {OsIcon} from '../ui/OsIcon'
import {DragPayload, FilePane, Item, PaneFs, PaneState} from './FilePane'
import {RemoteHost, RemotePane} from './RemotePane'
import {OPEN_IN_SFTP, type OpenInSftp} from './openRequest'
import {hostLabel as displayHost, LOCAL_HOST_ID, localPaneFs} from './localFs'
import {QueuePanel} from './QueuePanel'
import {SyncDialog} from './SyncDialog'
import {queueTransfer, replaceQuestion, summarize, useQueues, verbOf} from './transfer'
import type {Host, QueueJob} from '../types'

export {replaceQuestion, summarize}

const NONE: PaneState = {cwd: '', selected: []}
interface RemoteTab {key: string; host: RemoteHost; /** open at this folder instead of the home folder */ path?: string}
let tabSeq = 0
const nextTabKey = () => 'rt' + tabSeq++
/**
 * The SFTP screen: this computer on the left, a saved host on the right. Select one or more
 * files and folders (Cmd/Ctrl-click, Shift-click), then drag them across or use Upload (in the Actions menu) / Download
 * buttons. Folders go with everything inside. Nothing is replaced without asking, and the question comes
 * once for the whole batch. Transfers run in a queue in the background: they can be paused, resumed, cancelled and
 * retried, and a broken one continues where it stopped.
 */
/** sent by the app when the Close tab shortcut is used on an SFTP screen: close the host tab that is showing */
export const CLOSE_HOST_TAB = 'vutty:sftp-close-host'

export function SftpBrowser({hostId = '', hostLabel = '', nonce = 0, hosts: given, visible = true, main = false}: {main?: boolean; hostId?: string; hostLabel?: string; nonce?: number; /** this screen is the one showing (only that one answers the Close tab shortcut) */ visible?: boolean; /** the app's current hosts (kept up to date by the app); without it they are loaded once */ hosts?: Host[]}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  // the hosts open on the right, as tabs in the pane header; every one keeps its own connection, folder and selection
  const [tabs, setTabs] = useState<RemoteTab[]>(() => (hostId ? [{key: nextTabKey(), host: {id: hostId, label: hostLabel || hostId}}] : []))
  const [activeKey, setActiveKey] = useState(() => tabs[0]?.key ?? '')
  const active = tabs.find(tab => tab.key === activeKey) ?? null
  const host = active?.host ?? null
  const [loaded, setLoaded] = useState<Host[]>([])
  const hosts = given ?? loaded
  const [localHome, setLocalHome] = useState('')
  const [local, setLocal] = useState<PaneState>(NONE)
  const [remotes, setRemotes] = useState<Record<string, PaneState>>({})
  const [sids, setSids] = useState<Record<string, string>>({})
  const remote = remotes[activeKey] ?? NONE
  const sid = sids[activeKey] ?? ''
  const [localKey, setLocalKey] = useState(0)
  const [remoteKey, setRemoteKey] = useState(0)
  const [error, setError] = useState('')
  const [queueOpen, setQueueOpen] = useState(false)
  const [syncing, setSyncing] = useState(false)

  // the callbacks of a pane must keep one identity (the pane reports into them from effects), so one pair per tab
  const callbacks = useRef(new Map<string, {onState(s: PaneState): void; onSession(id: string): void}>())
  const cbFor = (key: string) => {
    let c = callbacks.current.get(key)
    if (!c) {
      c = {onState: st => setRemotes(m => ({...m, [key]: st})), onSession: id => setSids(m => (m[key] === id ? m : {...m, [key]: id}))}
      callbacks.current.set(key, c)
    }
    return c
  }
  const addTab = (h: RemoteHost, path?: string) => {
    const key = nextTabKey()
    setTabs(ts => [...ts, {key, host: h, path}])
    setActiveKey(key)
  }
  // a Server panel asked for a host at a folder: only the main SFTP tab answers, in a tab of its own
  const addRef = useRef(addTab)
  addRef.current = addTab
  useEffect(() => {
    if (!main) return
    const open = (e: Event) => { const r = (e as CustomEvent<OpenInSftp>).detail; if (r?.host?.id) addRef.current(r.host, r.path || undefined) }
    window.addEventListener(OPEN_IN_SFTP, open)
    return () => window.removeEventListener(OPEN_IN_SFTP, open)
  }, [main])
  const closeTab = (key: string) => {
    const at = tabs.findIndex(tab => tab.key === key)
    const rest = tabs.filter(tab => tab.key !== key)
    setTabs(rest)
    if (key === activeKey) setActiveKey(rest[Math.max(0, at - 1)]?.key ?? '')
    callbacks.current.delete(key)
    setRemotes(m => { const n = {...m}; delete n[key]; return n })
    setSids(m => { const n = {...m}; delete n[key]; return n })
  }

  const activeRef = useRef(activeKey)
  activeRef.current = activeKey
  useEffect(() => {
    if (!visible) return
    const close = () => { if (activeRef.current) closeTab(activeRef.current) }
    window.addEventListener(CLOSE_HOST_TAB, close)
    return () => window.removeEventListener(CLOSE_HOST_TAB, close)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible, tabs])

  // a host card asked for this host (again): connect to it in a tab of its own (not on the first run: the tab is already there)
  const firstRun = useRef(true)
  useEffect(() => {
    if (firstRun.current) { firstRun.current = false; return }
    if (hostId) addTab({id: hostId, label: hostLabel || hostId})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hostId, nonce])
  useEffect(() => {
    void be.local.home().then(setLocalHome).catch(e => setError(errMsg(e)))
    if (!given) void be.hosts.listHosts({}).then(h => setLoaded(h ?? [])).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [be])
  // a finished job refreshes the side it wrote to and says what it did
  const finished = useCallback((_sid: string, j: QueueJob) => {
    const toRemote = j.direction === 'upload' || j.direction === 'sync-up'
    if (j.state === 'done') toast.info(summarize(verbOf(j.direction), j))
    else if (j.state === 'failed') toast.error(`${j.name}: ${j.error || t('the transfer failed')}`)
    if (j.state === 'canceled' || j.state === 'done' || j.state === 'failed') { if (toRemote) setRemoteKey(k => k + 1); else setLocalKey(k => k + 1) }
  }, [toast, t])
  const queues = useQueues(Object.values(sids), finished)
  const jobs = queues[sid] ?? []
  const queueActions = {
    pause: (id: string) => void be.sftp.pause(sid, id).catch(e => setError(errMsg(e))),
    resume: (id: string) => void be.sftp.resume(sid, id).catch(e => setError(errMsg(e))),
    cancel: (id: string) => void be.sftp.cancel(sid, id).catch(e => setError(errMsg(e))),
    retry: (id: string) => void be.sftp.retry(sid, id).catch(e => setError(errMsg(e))),
    clearFinished: () => void be.sftp.clearFinished(sid).then(() => be.sftp.queue(sid)).catch(e => setError(errMsg(e))),
  }

  // keep one identity across renders so the pane does not reload every time
  const [fs] = useState<PaneFs>(() => localPaneFs(be))
  // "This computer" picked on the right: no connection, so a transfer is a plain copy between two folders of this computer
  const viaLocal = host?.id === LOCAL_HOST_ID

  /** Queues everything given; files that already exist are asked about once. */
  const move = async (direction: 'upload' | 'download', items: Item[]) => {
    if (!sid && !viaLocal) return setError(t('Connect to a host first'))
    const toRemote = direction === 'upload'
    const destDir = toRemote ? remote.cwd : local.cwd
    if (!destDir) return setError(t('Open a folder on the receiving side first'))
    if (items.length === 0) return
    setError('')
    try {
      if (viaLocal) {
        const sources = items.map(i => i.path)
        const clash = await be.local.conflicts(sources, destDir)
        const overwrite = clash.length > 0 && (await confirm(replaceQuestion({skipped: clash}, destDir), {confirmLabel: t('Replace'), danger: true}))
        const r = await be.local.copy(sources, destDir, overwrite)
        toast.info(summarize('Copied', r))
        if (toRemote) setRemoteKey(k => k + 1); else setLocalKey(k => k + 1)
        return
      }
      await queueTransfer(be, confirm, sid, direction, items.map(i => i.path), destDir)
      setQueueOpen(true)
    } catch (err) {
      setError(errMsg(err))
    }
  }

  const onDropToRemote = useCallback((p: DragPayload) => { void move('upload', p.items) }, [sid, remote.cwd, viaLocal]) // eslint-disable-line react-hooks/exhaustive-deps
  const onDropToLocal = useCallback((p: DragPayload) => { void move('download', p.items) }, [sid, local.cwd, viaLocal]) // eslint-disable-line react-hooks/exhaustive-deps

  const canDown = (!!sid || viaLocal) && remote.selected.length > 0
  const nSel = remote.selected.length
  const downText = viaLocal
    ? (nSel > 1 ? t('Copy {n} to the left folder', {n: nSel}) : t('Copy to the left folder'))
    : (nSel > 1 ? t('Download {n} to local folder', {n: nSel}) : t('Download to local folder'))

  return (
    <div className="sftp" data-testid="sftp" aria-label={host ? `SFTP ${displayHost(host)}` : 'SFTP'}>
      <div className="sftp-panes">
        <FilePane side="local" title={t('Local')} icon={<IconFolder size={16} />} fs={localHome ? fs : null} startDir={localHome} reloadKey={localKey} showHiddenDefault={false}
          onState={setLocal} onDropEntry={onDropToLocal} onCopyOut={sid || viaLocal ? items => void move('upload', items) : undefined} copyLabel={viaLocal ? t('Copy to right') : t('Upload')} />
        <div className="rpanes">
          {tabs.map(tab => {
            const cb = cbFor(tab.key)
            return (
              <RemotePane key={tab.key} host={tab.host} startPath={tab.path} hosts={hosts} hidden={tab.key !== activeKey} onNewTab={addTab} onHost={() => closeTab(tab.key)} reloadKey={remoteKey}
                onState={cb.onState} onDropEntry={onDropToRemote} onSession={cb.onSession} onCopyOut={items => void move('download', items)}
                tabsLead={add => (
                  <div className="rtabs" role="tablist" aria-label={t('Remote hosts')}>
                    {tabs.map(x => (
                      <div key={x.key} role="tab" aria-selected={x.key === activeKey} className={'rtab' + (x.key === activeKey ? ' on' : '')} onClick={() => setActiveKey(x.key)}>
                        {x.host.id === LOCAL_HOST_ID ? <IconFolder size={15} /> : <OsIcon os={hosts.find(h => h.id === x.host.id.replace(/^ftp:/, ''))?.os} size={15} />}
                        <span className="rtab-name">{displayHost(x.host)}</span>
                        <button className="icon ghost fpane-x" aria-label={tabs.length === 1 ? t('Change host') : t('Close {name}', {name: displayHost(x.host)})} title={tabs.length === 1 ? t('Disconnect and pick another host') : t('Close this host')}
                          onClick={e => { e.stopPropagation(); closeTab(x.key) }}><IconClose size={13} /></button>
                      </div>
                    ))}
                    <button className="icon ghost fpane-x" aria-label={t('Add host')} title={t('Connect to another host')} onClick={add}><IconPlus size={14} /></button>
                  </div>
                )} />
            )
          })}
          {tabs.length === 0 && (
            <RemotePane host={null} hosts={hosts} onHost={h => { if (h) addTab(h) }} reloadKey={remoteKey}
              onState={cbFor('none').onState} onDropEntry={onDropToRemote} onSession={cbFor('none').onSession} />
          )}
        </div>
      </div>
      <div className="sftp-foot">
        <button disabled={!canDown} onClick={() => void move('download', remote.selected)} aria-label={viaLocal ? t('Copy selected to left') : t('Download selected')}><IconDownload size={15} /> {downText}</button>
        <button disabled={!sid || !local.cwd || !remote.cwd} onClick={() => setSyncing(true)} aria-label={t('Sync folders')}>{t('Sync folders…')}</button>
        {error && <div className="error" role="alert">{error}</div>}
      </div>
      <QueuePanel jobs={jobs} actions={queueActions} open={queueOpen} onToggle={() => setQueueOpen(o => !o)} />
      {syncing && <SyncDialog sid={sid} localDir={local.cwd} remoteDir={remote.cwd} onClose={() => setSyncing(false)} onQueued={() => setQueueOpen(true)} />}
    </div>
  )
}
