import {useCallback, useEffect, useState} from 'react'
import {Field, Modal} from '../ui/Modal'
import {useBackend} from '../backend'
import {errMsg} from '../util'
import type {SyncStatus, VaultStatus} from '../types'

/** Sync through Git or a cloud folder. Either place only ever receives bundle.enc and vault-header.json (ciphertext). */
export function SyncPanel({vault, onClose, onChanged, page = false}: {vault: VaultStatus; onClose(): void; onChanged(): void; page?: boolean}) {
  const Wrap = page ? PageWrap : ModalWrap
  const be = useBackend()
  const [st, setSt] = useState<SyncStatus | null>(null)
  const [url, setUrl] = useState('')
  const [mode, setMode] = useState<'git' | 'folder' | null>(null)
  const [dir, setDir] = useState('')
  const [pw, setPw] = useState('')
  const [joinPw, setJoinPw] = useState('')
  const [replaceWord, setReplaceWord] = useState('')
  // the join / replace box stays once it has appeared, until a sync works: a failed join must not take the way out away
  const [offerJoin, setOfferJoin] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    const s = await be.sync.status()
    setSt(s)
    setUrl(u => u || s.repo_url)
    setDir(d => d || s.folder)
    setMode(m => m ?? (s.folder ? 'folder' : 'git'))
  }, [be])
  useEffect(() => { void load() }, [load])
  const lastError = st?.last_error ?? ''
  useEffect(() => {
    if (`${err} ${lastError}`.includes('different vault key')) setOfferJoin(true)
  }, [err, lastError])

  const run = async (fn: () => Promise<void>, syncs = false) => {
    setErr('')
    setBusy(true)
    try {
      await fn()
      if (syncs) setOfferJoin(false) // a sync worked: the problem is gone
      onChanged()
    } catch (e) {
      setErr(errMsg(e))
    } finally {
      setBusy(false)
      void load()
    }
  }

  return (
    <Wrap onClose={onClose}>
      <div className="segmented" role="group" aria-label="Sync method">
        <button className={mode === 'git' ? 'on' : ''} onClick={() => setMode('git')}>Git repository</button>
        <button className={mode === 'folder' ? 'on' : ''} onClick={() => setMode('folder')}>Cloud folder</button>
      </div>
      {mode === 'folder' ? (
        <>
          <p className="muted">Pick a folder that Google Drive, iCloud Drive, Dropbox or OneDrive keeps in step. Vutty writes only the two encrypted files there, with no sign-in and no token. If two Macs save at once, the extra copies the cloud client creates are merged automatically.</p>
          <div className="row">
            <Field label="Folder"><input aria-label="Sync folder" value={dir} onChange={e => setDir(e.target.value)} placeholder="/Users/you/Google Drive/Vutty" /></Field>
            <div className="fixed"><button disabled={busy} onClick={() => run(async () => { const p = await be.system.pickDirectory('Choose the sync folder'); if (p) setDir(p) })}>Browse…</button></div>
          </div>
        </>
      ) : (
        <>
          <p className="muted">Sync your encrypted hosts and keys through a private Git repository you own. Authentication uses your ssh-agent or git credential helper.</p>
          <Field label="Repository URL"><input aria-label="Repository URL" value={url} onChange={e => setUrl(e.target.value)} placeholder="git@github.com:you/vutty-sync.git" /></Field>
        </>
      )}
      <div className="row">
        {mode === 'folder'
          ? <button disabled={busy || !dir.trim()} onClick={() => run(() => be.sync.configureFolder(dir.trim()))}>Use this folder</button>
          : <button disabled={busy || !url.trim()} onClick={() => run(() => be.sync.configure(url.trim()))}>Save repository</button>}
        <button className="primary" disabled={busy || !st?.configured || !vault.unlocked} onClick={() => run(() => be.sync.syncNow(), true)}>{busy ? 'Syncing…' : 'Sync now'}</button>
      </div>
      {st && (
        <p className="muted" data-testid="sync-status">
          {st.configured ? (st.folder ? `Syncing through ${st.folder}.` : 'Configured.') : 'Not configured.'}{' '}
          {st.last_sync && !st.last_sync.startsWith('0001') ? `Last sync: ${new Date(st.last_sync).toLocaleString()}` : 'Never synced.'}
          {st.last_error && <span className="error"> {st.last_error}</span>}
        </p>
      )}
      {vault.unlocked && offerJoin && (
        <div className="card-section" role="group" aria-label="Join the synced vault">
          <h4>{st?.local_empty ? 'Use the vault of this repository' : 'This repository belongs to another vault'}</h4>
          {st?.local_empty ? (
            <p className="muted">
              This Mac has nothing in it yet, so there is nothing to merge. Enter the master password of the vault in the repository: its hosts,
              keys and snippets come here, and that master password is the one you use from now on.
            </p>
          ) : (
            <p className="muted">
              Its data is locked with a different key, so the two cannot be merged as they are. To sync with it, enter the master password of
              the vault that made it. This Mac keeps its own hosts and saved keys and merges them in, then uses that master password from now on.
              A copy of this vault is saved first, in the <b>backups</b> folder of the app's data.
            </p>
          )}
          <Field label="Master password of the synced vault"><input aria-label="Master password of the repository's vault" type="password" value={joinPw} onChange={e => setJoinPw(e.target.value)} /></Field>
          <button className="primary" disabled={busy || !joinPw} onClick={() => run(async () => { await be.sync.joinRemoteVault(joinPw); setJoinPw('') }, true)}>{st?.local_empty ? "Use the repository's vault" : 'Join this vault and sync'}</button>
          <details open={err.includes('use Replace below')}>
            <summary>The password does not work, or nobody can open that repository any more</summary>
            <p className="muted">
              Then the repository can be <b>replaced</b> by the data of this Mac. What is there now is lost to the repository (a copy of its two files is kept in the
              <b> backups</b> folder of the app's data). Other machines will have to use <b>Restore from repository</b> with this vault's master password.
              Type <b>REPLACE</b> to confirm.
            </p>
            <Field label="Type REPLACE"><input aria-label="Type REPLACE to confirm" value={replaceWord} onChange={e => setReplaceWord(e.target.value)} /></Field>
            <button disabled={busy || replaceWord !== 'REPLACE'} onClick={() => run(async () => { await be.sync.replaceRemote(); setReplaceWord('') }, true)}>Replace the repository with this Mac's data</button>
          </details>
        </div>
      )}
      <div className="section">New machine?</div>
      <p className="muted">If the repository or folder already holds a bundle, enter its master password to restore everything on this machine (only possible before creating a local vault).</p>
      <Field label="Master password of the synced vault"><input aria-label="Synced master password" type="password" value={pw} onChange={e => setPw(e.target.value)} /></Field>
      <button disabled={busy || !st?.configured || vault.exists || !pw} onClick={() => run(() => be.sync.join(pw), true)}>Restore from {mode === 'folder' ? 'folder' : 'repository'}</button>
      {err && <div className="error" role="alert">{err}</div>}
    </Wrap>
  )
}

function ModalWrap({onClose, children}: {onClose(): void; children: React.ReactNode}) {
  return <Modal title="Sync" onClose={onClose}>{children}</Modal>
}

function PageWrap({children}: {onClose(): void; children: React.ReactNode}) {
  return <section className="card-section" aria-label="Sync"><h4>Sync</h4>{children}</section>
}
