import {FormEvent, MouseEvent as ReactMouseEvent, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {useMinBusy} from '../ui/useMinBusy'
import {IconArrowUp, IconDownload, IconEye, IconEyeOff, IconFile, IconFolder, IconLocate, IconRefresh} from '../ui/Icons'
import {errMsg} from '../util'
import {cdCommand, folderOf, formatBytes, joinPath, parentOf, validName} from './format'
import {PermissionsDialog} from './Permissions'
import type {PaneFs} from './FilePane'
import {remoteFs, watchEdits} from './remoteFs'
import {QueuePanel} from './QueuePanel'
import {queueTransfer, summarize, useQueues, verbOf} from './transfer'
import {authenticity} from './RemotePane'
import {AuthPrompt} from '../ui/AuthPrompt'
import {cwdOfPane, paneStatusVersion, sessionOfPane, watchPaneStatus, writeToPane, toTerminalInput} from '../terminal/panes'
import {LOCAL_HOST_ID, localPaneFs} from './localFs'
import type {AuthChallenge, Host, HostKeyPrompt, QueueJob, RemoteEntry} from '../types'

let seq = 0

const HIDDEN_KEY = 'vutty.sidebar.files.hidden'
const loadHidden = () => { try { return localStorage.getItem(HIDDEN_KEY) === '1' } catch { return false } }

/**
 * The Files section of the terminal sidebar: the server of the focused terminal as a compact list, with
 * upload, download and the transfer queue. It has an SFTP connection of its own, opened when the section is
 * shown and closed when it is hidden or the terminal goes away. A local terminal shows this computer's files.
 */
export function FilesTab({host, hostId, paneId}: {host: Host | null; hostId?: string; paneId?: string}) {
  const t = useT()
  if (hostId === LOCAL_HOST_ID) return <Files key={LOCAL_HOST_ID} host={null} paneId={paneId} />
  if (!host) return <div className="side-empty">{t('Files are shown for local terminals and terminals connected to a saved SSH host. Focus one to browse its files.')}</div>
  return <Files key={host.id} host={host} paneId={paneId} />
}

/** host is null for this computer. */
function Files({host, paneId}: {host: Host | null; paneId?: string}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const alive = useRef(true)
  const local = host === null
  const hostId = host ? host.id : null
  const listRef = useRef<HTMLDivElement>(null)
  const loadSeq = useRef(0)
  useSyncExternalStore(watchPaneStatus, paneStatusVersion)
  const termDir = paneId ? cwdOfPane(paneId) : ''
  const cwdRef = useRef('')
  const [attempt, setAttempt] = useState(0)
  const [phase, setPhase] = useState<'connecting' | 'ready' | 'closed'>('connecting')
  const [reason, setReason] = useState('')
  const [prompt, setPrompt] = useState<{key: string; p: HostKeyPrompt} | null>(null)
  const [auth, setAuth] = useState<{key: string; c: AuthChallenge} | null>(null)
  const [sid, setSid] = useState('')
  const [home, setHome] = useState('')
  const [cwd, setCwd] = useState('')
  const [entries, setEntries] = useState<RemoteEntry[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [anchor, setAnchor] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [hidden, setHidden] = useState(loadHidden)
  const [pfs, setPfs] = useState<PaneFs | null>(null)
  const edited = useRef(new Set<string>()) // remote files being edited from here: released when the connection ends
  const [menu, setMenu] = useState<{x: number; y: number} | null>(null)
  const [naming, setNaming] = useState<{mode: 'mkdir' | 'rename'; value: string; err: string} | null>(null)
  const [perms, setPerms] = useState<RemoteEntry | null>(null)

  // a slow answer for a folder the user has already left must not replace the folder they are in now
  const load = useCallback(async (s: string, dir: string) => {
    const mine = ++loadSeq.current
    setLoading(true)
    setError('')
    try {
      const list = await (s === LOCAL_HOST_ID ? be.local.list(dir) : be.sftp.list(s, dir))
      if (!alive.current || mine !== loadSeq.current) return
      setEntries(list ?? [])
      setCwd(dir)
      cwdRef.current = dir
      setSelected([])
      setAnchor('')
      if (listRef.current) listRef.current.scrollTop = 0
    } catch (e) {
      if (alive.current && mine === loadSeq.current) setError(errMsg(e))
    } finally {
      if (alive.current && mine === loadSeq.current) setLoading(false)
    }
  }, [be])

  useEffect(() => {
    alive.current = true
    let opened = ''
    const key = `sftp-side-${Date.now().toString(36)}-${++seq}`
    const offs: Array<() => void> = [be.on(`term:hostkey:${key}`, (p: HostKeyPrompt) => setPrompt({key, p})), be.on(`term:auth:${key}`, (c: AuthChallenge) => setAuth({key, c}))]
    setPhase('connecting')
    setReason('')
    setSid('')
    setPfs(null)
    ;(async () => {
      try {
        if (hostId === null) {
          const home = (await be.local.home()) || '/'
          if (!alive.current) return
          setSid(LOCAL_HOST_ID)
          setPfs(localPaneFs(be))
          setHome(home)
          setPhase('ready')
          await load(LOCAL_HOST_ID, home)
          return
        }
        const info = await be.sftp.open(hostId, key)
        if (!alive.current) { void be.sftp.close(info.sid).catch(() => {}); return }
        opened = info.sid
        offs.push(watchEdits(be, t, info.sid, toast, confirm))
        offs.push(be.on(`sftp:closed:${info.sid}`, () => { setPhase('closed'); setReason(t('The connection was lost.')); setSid(''); setPfs(null); opened = '' }))
        setPfs(remoteFs(be, t, info.sid, edited.current))
        setPrompt(null)
        setAuth(null)
        setSid(info.sid)
        setHome(info.home || '/')
        setPhase('ready')
        await load(info.sid, info.home || '/')
      } catch (e) {
        if (alive.current) { setPrompt(null); setAuth(null); setPhase('closed'); setReason(errMsg(e)) }
      }
    })()
    return () => {
      alive.current = false
      offs.forEach(o => o())
      if (opened) {
        for (const p of edited.current) void be.sftp.editStop(opened, p).catch(() => {})
        void be.sftp.close(opened).catch(() => {})
      }
      edited.current.clear()
    }
  }, [be, hostId, attempt, load])

  const finished = useCallback((_sid: string, j: QueueJob) => {
    if (j.state === 'done') toast.info(summarize(verbOf(j.direction), j))
    else if (j.state === 'failed') toast.error(`${j.name}: ${j.error || t('the transfer failed')}`)
    if ((j.direction === 'upload' || j.direction === 'sync-up') && cwdRef.current && sidRef.current) void load(sidRef.current, cwdRef.current)
  }, [toast, load, t])
  const sidRef = useRef(sid)
  sidRef.current = sid
  const jobs = useQueues(sid && !local ? [sid] : [], finished)[sid] ?? []
  const actions = {
    pause: (id: string) => void be.sftp.pause(sid, id).catch(e => setError(errMsg(e))),
    resume: (id: string) => void be.sftp.resume(sid, id).catch(e => setError(errMsg(e))),
    cancel: (id: string) => void be.sftp.cancel(sid, id).catch(e => setError(errMsg(e))),
    retry: (id: string) => void be.sftp.retry(sid, id).catch(e => setError(errMsg(e))),
    clearFinished: () => void be.sftp.clearFinished(sid).then(() => be.sftp.queue(sid)).catch(e => setError(errMsg(e))),
  }

  const answer = (trust: boolean) => {
    if (!prompt) return
    void be.session.trustHostKey(prompt.key, trust ? prompt.p.fingerprint : '').catch(e => setReason(errMsg(e)))
    setPrompt(null)
  }

  const shown = useMemo(() => (hidden ? entries : entries.filter(e => !e.name.startsWith('.'))), [entries, hidden])
  const toggleHidden = () => setHidden(h => { try { localStorage.setItem(HIDDEN_KEY, h ? '0' : '1') } catch { /* not remembered */ } return !h })
  const chosen = useMemo(() => entries.filter(e => selected.includes(e.path)), [entries, selected])
  const ready = phase === 'ready' && !!sid && !!cwd
  const spinning = useMinBusy(loading)

  const pick = (e: RemoteEntry, ev: ReactMouseEvent) => {
    if (ev.metaKey || ev.ctrlKey) {
      setSelected(s => (s.includes(e.path) ? s.filter(x => x !== e.path) : [...s, e.path]))
      setAnchor(e.path)
    } else if (ev.shiftKey && anchor) {
      const a = shown.findIndex(x => x.path === anchor), b = shown.findIndex(x => x.path === e.path)
      if (a >= 0 && b >= 0) setSelected(shown.slice(Math.min(a, b), Math.max(a, b) + 1).map(x => x.path))
    } else {
      setSelected([e.path])
      setAnchor(e.path)
    }
  }

  const guard = async (fn: () => Promise<unknown>) => {
    setError('')
    try { await fn() } catch (e) { setError(errMsg(e)) }
  }
  const open = (e: RemoteEntry) => (e.is_dir ? void load(sid, e.path) : pfs?.open ? void guard(() => pfs.open!(e)) : undefined)
  // what the shell reported (OSC 7) is the freshest; otherwise the backend asks the shell's process (this computer, or a Linux server)
  const toTerminal = () => guard(async () => {
    const term = paneId ? sessionOfPane(paneId) : ''
    const dir = termDir || (term ? await be.session.cwd(term) : '')
    if (!dir) throw new Error(t('Could not tell which folder the terminal is in.'))
    await load(sid, dir)
  })
  const upload = (folder: boolean) => guard(async () => {
    const p = await (folder ? be.system.pickDirectory(t('Upload folder')) : be.system.pickFile(t('Upload file')))
    if (p) await queueTransfer(be, confirm, sid, 'upload', [p], cwd)
  })
  const download = () => guard(async () => {
    const dir = await be.system.pickDirectory(t('Save to folder'))
    if (dir) await queueTransfer(be, confirm, sid, 'download', chosen.map(e => e.path), dir)
  })

  // the right-click menu closes on any click elsewhere, Escape, scrolling or a resize
  useEffect(() => {
    if (!menu) return
    const off = () => setMenu(null)
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') off() }
    window.addEventListener('mousedown', off)
    window.addEventListener('resize', off)
    window.addEventListener('keydown', key)
    return () => { window.removeEventListener('mousedown', off); window.removeEventListener('resize', off); window.removeEventListener('keydown', key) }
  }, [menu])
  const openMenu = (ev: ReactMouseEvent, entry: RemoteEntry | null) => {
    ev.preventDefault()
    ev.stopPropagation()
    if (!ready) return
    if (entry && !selected.includes(entry.path)) { setSelected([entry.path]); setAnchor(entry.path) }
    setMenu({x: Math.max(4, Math.min(ev.clientX, window.innerWidth - 220)), y: Math.max(4, Math.min(ev.clientY, window.innerHeight - 380))})
  }
  // types `cd` into this sidebar's terminal: a folder goes to itself, a file to the folder it is in
  const goToFolder = (e: RemoteEntry) => {
    if (!paneId || !writeToPane(paneId, toTerminalInput(cdCommand(folderOf(e)), true))) toast.error(t('The terminal is not available'))
  }
  const act = (fn: () => void | Promise<void>) => { setMenu(null); void guard(async () => { await fn() }) }
  const remove = (list: RemoteEntry[]) => guard(async () => {
    const question = list.length > 1 ? t('Delete these {n} items (folders with everything in them)? This cannot be undone.', {n: list.length})
      : list[0].is_dir ? t('Delete the folder "{name}" and everything in it? This cannot be undone.', {name: list[0].name}) : t('Delete "{name}"? This cannot be undone.', {name: list[0].name})
    if (!(await confirm(question, {confirmLabel: t('Delete'), danger: true}))) return
    try {
      for (const e of list) await pfs!.remove(e.path, e.is_dir)
    } finally {
      await load(sid, cwd) // some may have gone even when one failed
    }
  })
  const submitName = (ev: FormEvent) => {
    ev.preventDefault()
    if (!naming || !pfs) return
    const bad = validName(naming.value)
    if (bad) return setNaming({...naming, err: bad})
    const name = naming.value.trim()
    const one = chosen.length === 1 ? chosen[0] : null
    void guard(async () => {
      if (naming.mode === 'mkdir') await pfs.mkdir(joinPath(cwd, name))
      else if (one) await pfs.rename(one.path, joinPath(parentOf(one.path), name))
      setNaming(null)
      await load(sid, cwd)
    })
  }

  return (
    <div className="side-files" aria-label={t('Server files')}>
      <div className="side-files-bar">
        <button className="icon ghost" aria-label={t('Up one folder')} disabled={!ready || cwd === '/'} onClick={() => void load(sid, parentOf(cwd))}><IconArrowUp size={15} /></button>
        <button className="icon ghost" aria-label={t('Refresh files')} aria-busy={spinning} disabled={!ready} onClick={() => void load(sid, cwd)}><IconRefresh size={15} /></button>
        <button className="icon ghost" aria-label={t("Go to the terminal's folder")} disabled={!ready}
          title={t("Go to the terminal's folder")}
          onClick={() => void toTerminal()}><IconLocate size={15} /></button>
        <button className="icon ghost" aria-label={hidden ? t('Hide hidden files') : t('Show hidden files')} aria-pressed={hidden} title={hidden ? t('Hide hidden files') : t('Show hidden files')} onClick={toggleHidden}>
          {hidden ? <IconEye size={15} /> : <IconEyeOff size={15} />}
        </button>
        <code className="side-files-path" title={cwd} aria-label={t('Current folder')}>{cwd ? <bdi>{cwd}</bdi> : phase === 'connecting' ? t('Connecting…') : ''}</code>
      </div>
      {!local && <div className="side-files-bar">
        <button disabled={!ready} onClick={() => void upload(false)}>{t('Upload file…')}</button>
        <button disabled={!ready} onClick={() => void upload(true)}>{t('Upload folder…')}</button>
        <button disabled={!ready || chosen.length === 0} onClick={() => void download()} aria-label={t('Download selected files')}>
          <IconDownload size={14} /> {chosen.length > 1 ? t('Download {n}', {n: chosen.length}) : t('Download')}
        </button>
      </div>}
      {phase === 'connecting' && !prompt && !auth && !local && <div className="side-empty">{t('Connecting to {name}…', {name: host.label})}</div>}
      <AuthPrompt inline challenge={auth?.c ?? null} promptKey={auth?.key ?? ''} onDone={() => setAuth(null)} onError={setReason} />
      {phase === 'closed' && (
        <div className="side-empty" role="alert">
          <div>{reason || t('Disconnected.')}</div>
          <button onClick={() => setAttempt(a => a + 1)}>{t('Reconnect')}</button>
        </div>
      )}
      {prompt && (
        <div className="side-empty" role="alertdialog" aria-label={t('Host key confirmation')}>
          <div>{authenticity(t("The authenticity of {host} can't be established.", {host: '\u0001'}), prompt.p.host)}</div>
          <div className="fp" data-testid="fingerprint">{prompt.p.fingerprint}</div>
          <div className="row"><button className="primary" onClick={() => answer(true)}>{t('Trust and connect')}</button><button onClick={() => answer(false)}>{t('Cancel')}</button></div>
        </div>
      )}
      {naming && (
        <form className="side-files-bar side-files-name" onSubmit={submitName}>
          <input aria-label={t('Name')} autoFocus value={naming.value} placeholder={naming.mode === 'mkdir' ? t('New folder name') : t('New name')}
            onChange={e => setNaming({...naming, value: e.target.value, err: ''})} onKeyDown={ev => { if (ev.key === 'Escape') setNaming(null) }} />
          <button className="primary" type="submit">{naming.mode === 'mkdir' ? t('Create') : t('Rename')}</button>
          <button type="button" onClick={() => setNaming(null)}>{t('Cancel')}</button>
          {naming.err && <span className="error" role="alert">{naming.err}</span>}
        </form>
      )}
      {ready && (
        <div ref={listRef} className="side-list side-files-list" role="listbox" aria-multiselectable="true" aria-label={t('Files')} aria-busy={loading} onContextMenu={ev => openMenu(ev, null)}>
          {shown.map(e => (
            <div key={e.path} role="option" tabIndex={0} aria-selected={selected.includes(e.path)} className={'side-item side-file' + (selected.includes(e.path) ? ' on' : '')}
              onClick={ev => pick(e, ev)} onContextMenu={ev => openMenu(ev, e)} onDoubleClick={() => open(e)} onKeyDown={ev => { if (ev.key === 'Enter') open(e); else if (ev.key === 'Backspace' && cwd !== '/') { ev.preventDefault(); void load(sid, parentOf(cwd)) } }}>
              {e.is_dir ? <IconFolder size={15} /> : <IconFile size={15} />}
              <span className="side-main"><b>{e.name}</b></span>
              <small className="muted">{e.is_dir ? '' : formatBytes(e.size)}</small>
            </div>
          ))}
          {!loading && shown.length === 0 && !error && <div className="side-empty">{t('This folder is empty.')}</div>}
        </div>
      )}
      {error && <div className="error side-files-error" role="alert">{error}</div>}
      {menu && (() => {
        const none = chosen.length === 0
        const entry = chosen.length === 1 ? chosen[0] : null
        return (
          <div className="ctx-menu" role="menu" aria-label={none ? t('Folder menu') : t('File menu')} style={{left: menu.x, top: menu.y}}
            onMouseDown={ev => ev.stopPropagation()} onContextMenu={ev => ev.preventDefault()}>
            <button role="menuitem" disabled={!entry || (!entry.is_dir && !pfs?.open)} onClick={() => act(() => open(entry!))}>{t('Open')}</button>
            <button role="menuitem" disabled={!entry || !paneId} onClick={() => act(() => goToFolder(entry!))}>{t('Go to folder')}</button>
            <button role="menuitem" disabled={!entry || entry.is_dir || !pfs?.openWith} onClick={() => act(() => pfs!.openWith!(entry!))}>{t('Open with…')}</button>
            {pfs?.edit && <button role="menuitem" disabled={!entry || entry.is_dir} onClick={() => act(() => pfs.edit!(entry!))}>{t('Edit and save back')}</button>}
            {!local && <>
              <button role="menuitem" disabled={none} onClick={() => act(download)}>{chosen.length > 1 ? t('Download {n}', {n: chosen.length}) : t('Download')}</button>
              <button role="menuitem" onClick={() => act(() => upload(false))}>{t('Upload file…')}</button>
              <button role="menuitem" onClick={() => act(() => upload(true))}>{t('Upload folder…')}</button>
            </>}
            <button role="menuitem" disabled={none} onClick={() => act(() => be.system.writeClipboard(chosen.map(e => e.path).join('\n')))}>{t('Copy path')}</button>
            <button role="menuitem" disabled={!entry} onClick={() => { setMenu(null); setNaming({mode: 'rename', value: entry!.name, err: ''}) }}>{t('Rename')}</button>
            <button role="menuitem" className="danger-item" disabled={none} onClick={() => { setMenu(null); void remove(chosen) }}>{t('Delete')}</button>
            <button role="menuitem" onClick={() => act(() => load(sid, cwd))}>{t('Refresh')}</button>
            <button role="menuitem" onClick={() => { setMenu(null); setNaming({mode: 'mkdir', value: '', err: ''}) }}>{t('New Folder')}</button>
            <button role="menuitem" onClick={() => { setMenu(null); toggleHidden() }}>{hidden ? t('Hide Hidden Files') : t('Show Hidden Files')}</button>
            <button role="menuitem" disabled={!entry || !pfs?.chmod} onClick={() => { setMenu(null); setPerms(entry) }}>{t('Edit Permissions')}</button>
            <button role="menuitem" disabled={shown.length === 0} onClick={() => { setMenu(null); setSelected(shown.map(e => e.path)) }}>{t('Select All')}</button>
          </div>
        )
      })()}
      {perms && pfs?.chmod && (
        <PermissionsDialog name={perms.name} mode={perms.mode} onClose={() => setPerms(null)}
          onSave={async m => { await pfs.chmod!(perms.path, m); await load(sid, cwd) }} />
      )}
      {!local && <QueuePanel jobs={jobs} actions={actions} open compact />}
    </div>
  )
}
