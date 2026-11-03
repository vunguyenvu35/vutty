import {useCallback, useEffect, useState} from 'react'
import {Modal} from '../ui/Modal'
import {Brand} from '../ui/Brand'
import {useBackend, type SelfUpdateStatus, type UpdateInfo, type UpdateProgress} from '../backend'
import {errMsg} from '../util'

const REPO = 'https://github.com/vunguyenvu35/vutty'

/** About Vutty: version, what it is, the project's links, and a check for a newer release on GitHub. */
export function AboutPanel({onClose, checkNow = false}: {onClose(): void; checkNow?: boolean}) {
  const be = useBackend()
  const [version, setVersion] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<UpdateInfo | null>(null)
  const [err, setErr] = useState('')
  const [canInstall, setCanInstall] = useState<SelfUpdateStatus | null>(null)
  const [installing, setInstalling] = useState(false)
  const [progress, setProgress] = useState<UpdateProgress | null>(null)

  const check = useCallback(async () => {
    setBusy(true)
    setErr('')
    try { setResult(await be.system.checkUpdate()) } catch (e) { setErr(errMsg(e)) } finally { setBusy(false) }
  }, [be])

  useEffect(() => { void be.system.info().then(i => setVersion(i.version)).catch(() => {}) }, [be])
  useEffect(() => { if (checkNow) void check() }, [checkNow, check])
  useEffect(() => { void be.system.selfUpdateStatus().then(setCanInstall).catch(() => {}) }, [be])
  useEffect(() => be.on('update:progress', (p: UpdateProgress) => setProgress(p)), [be])
  // the app quits when the new copy is in place: only a failure comes back here
  const install = async () => {
    setInstalling(true)
    setErr('')
    setProgress({stage: 'checking'})
    try { await be.system.installUpdate() } catch (e) { setErr(errMsg(e)); setInstalling(false); setProgress(null) }
  }
  const progressText = (p: UpdateProgress) => {
    switch (p.stage) {
      case 'checking': return 'Checking the release…'
      case 'downloading': return p.total ? `Downloading… ${Math.min(100, Math.floor(((p.done ?? 0) / p.total) * 100))}%` : 'Downloading…'
      case 'verifying': return 'Checking the signature and the checksum…'
      case 'installing': return 'Installing…'
      default: return 'Restarting Vutty…'
    }
  }

  return (
    <Modal title="About Vutty" onClose={onClose} footer={<button onClick={onClose}>Close</button>}>
      <div style={{textAlign: 'center', display: 'grid', gap: 10, justifyItems: 'center'}}>
        <Brand size="lg" />
        <div>Version <b data-testid="about-version">{version || '…'}</b></div>
        <div className="muted">An SSH client for people who want to own their data. Free and open source (MIT). Your hosts, keys and snippets stay on this computer.</div>
        <div className="muted">Made by Vu Nguyen Vu.</div>
        {busy && <div className="muted" role="status">Checking for updates…</div>}
        {err && <div className="error" role="alert">{err}</div>}
        {result && !busy && (result.available
          ? (
            <div style={{display: 'grid', gap: 8, justifyItems: 'center'}}>
              <div role="status">Version <b>{result.latest}</b> is available. <button className="link" onClick={() => void be.system.openUrl(result.url)}>Open the release page</button></div>
              {canInstall?.supported && !installing && <button className="primary" onClick={() => void install()}>Install and restart</button>}
              {installing && progress && <div role="progressbar" aria-label="Update" aria-valuetext={progressText(progress)}>{progressText(progress)}</div>}
              {canInstall && !canInstall.supported && canInstall.reason && <div className="muted" style={{fontSize: 12}}>It cannot be installed from here: {canInstall.reason}.</div>}
            </div>
          )
          : <div className="muted" role="status">You are on the latest version.</div>)}
        <div>
          <button disabled={busy} onClick={() => void check()}>Check for updates</button>
        </div>
        <div className="muted">
          <button className="link" onClick={() => void be.system.openUrl(REPO)}>GitHub</button>{' · '}
          <button className="link" onClick={() => void be.system.openUrl(REPO + '/issues/new')}>Report a bug</button>{' · '}
          <button className="link" onClick={() => void be.system.openUrl(REPO + '/blob/main/LICENSE')}>License</button>{' · '}
          <button className="link" onClick={() => void be.system.openUrl('https://ko-fi.com/vunguyenvu')}>Ko-fi</button>
        </div>
      </div>
    </Modal>
  )
}
