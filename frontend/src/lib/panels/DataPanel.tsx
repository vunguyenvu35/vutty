import {useState} from 'react'
import {Field, Modal} from '../ui/Modal'
import {Select} from '../ui/Select'
import {IconCheckCircle, IconDownload, IconFile, IconKey, IconUpload, IconWarn} from '../ui/Icons'
import {useBackend} from '../backend'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import type {ImportReport} from '../types'

type Tab = 'import' | 'export' | 'backup'
const TABS: {id: Tab; label: string}[] = [{id: 'import', label: 'Import'}, {id: 'export', label: 'Export'}, {id: 'backup', label: 'Backup'}]
const MIN_BACKUP_PW = 8

/**
 * Import / export. Nothing is locked in: ssh config, plain metadata JSON and encrypted backups all round-trip.
 * Three tabs, one job each: bring hosts in from an ssh config, take them out (ssh config or a plain list), or make and
 * restore an encrypted backup of everything.
 */
export function DataPanel({onClose, onChanged}: {onClose(): void; onChanged(): void}) {
  const be = useBackend()
  const toast = useToast()
  const [tab, setTab] = useState<Tab>('import')
  const [source, setSource] = useState<'ssh' | 'inventory'>('ssh')
  const [cfgPath, setCfgPath] = useState('')
  const [invPath, setInvPath] = useState('')
  const [invUser, setInvUser] = useState('')
  const [invAddress, setInvAddress] = useState<'public' | 'private'>('public')
  const [invGroup, setInvGroup] = useState('')
  const [invStopped, setInvStopped] = useState(false)
  const [conflict, setConflict] = useState('skip')
  const [report, setReport] = useState<ImportReport | null>(null)
  const [withKeys, setWithKeys] = useState(false)
  const [bpw, setBpw] = useState('')
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)

  const guard = async (fn: () => Promise<void>) => {
    setErr('')
    setMsg('')
    setBusy(true)
    try {
      await fn()
    } catch (e) {
      setErr(errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  const go = (t: Tab) => { setTab(t); setErr(''); setMsg('') }
  const pickSource = (v: 'ssh' | 'inventory') => { setSource(v); setReport(null); setErr('') }

  const choose = async (title: string, kind: 'file' | 'dir' | 'save', name = '') => {
    const p = kind === 'file' ? await be.system.pickFile(title) : kind === 'dir' ? await be.system.pickDirectory(title) : await be.system.saveFile(title, name)
    return p
  }

  const pwShort = bpw.length > 0 && bpw.length < MIN_BACKUP_PW

  return (
    <Modal title="Import & export" wide onClose={onClose}>
      <div className="dx-tabs" role="tablist" aria-label="Import and export">
        {TABS.map(t => (
          <button key={t.id} role="tab" aria-selected={tab === t.id} className={tab === t.id ? 'on' : ''} onClick={() => go(t.id)}>{t.label}</button>
        ))}
      </div>

      {tab === 'import' && (
        <div className="dx-page" role="tabpanel" aria-label="Import">
          <div className="segmented keep-case" role="group" aria-label="What to import from">
            <button className={source === 'ssh' ? 'on' : ''} aria-pressed={source === 'ssh'} onClick={() => pickSource('ssh')}>SSH config</button>
            <button className={source === 'inventory' ? 'on' : ''} aria-pressed={source === 'inventory'} onClick={() => pickSource('inventory')}>Cloud and inventory</button>
          </div>
          {source === 'ssh' ? (
            <p className="dx-lead">Bring the hosts, groups, port forwards and keys of an OpenSSH config file into Vutty.</p>
          ) : (
            <p className="dx-lead">Add the servers of a listing you made yourself: Vutty never contacts a cloud and never sees its credentials, so nothing but the file is read.</p>
          )}
          <div className="dx-card">
            {source === 'ssh' ? (
              <Field label="Config file">
                <div className="input-row">
                  <input aria-label="Config path" value={cfgPath} onChange={e => setCfgPath(e.target.value)} placeholder="~/.ssh/config" />
                  <button disabled={busy} onClick={() => guard(async () => { const p = await choose('Choose ssh config', 'file'); if (p) setCfgPath(p) })}>Browse…</button>
                </div>
              </Field>
            ) : (
              <>
                <details className="dx-how">
                  <summary>How to make the file</summary>
                  <div className="muted">
                    <div><b>AWS</b>: <code>aws ec2 describe-instances --output json &gt; aws.json</code></div>
                    <div><b>Google Cloud</b>: <code>gcloud compute instances list --format=json &gt; gcp.json</code></div>
                    <div><b>Ansible</b> (any inventory, and the cloud plugins): <code>ansible-inventory -i inventory --list &gt; inventory.json</code>, or the INI file itself</div>
                    <div><b>CSV</b>: a header line with <code>hostname</code> and, if you like, <code>label, user, port, group, env, tags</code></div>
                  </div>
                </details>
                <Field label="Listing file">
                  <div className="input-row">
                    <input aria-label="Inventory path" value={invPath} onChange={e => setInvPath(e.target.value)} placeholder="aws.json, inventory.ini or hosts.csv" />
                    <button disabled={busy} onClick={() => guard(async () => { const p = await choose('Choose the listing', 'file'); if (p) setInvPath(p) })}>Browse…</button>
                  </div>
                </Field>
                <div className="dx-two">
                  <Field label="User name for the hosts">
                    <input aria-label="User name" value={invUser} onChange={e => setInvUser(e.target.value)} placeholder="ec2-user for AWS; the file's own wins" autoComplete="off" />
                  </Field>
                  <Field label="Group">
                    <input aria-label="Group name" value={invGroup} onChange={e => setInvGroup(e.target.value)} placeholder="aws, gcp, or the Ansible group" autoComplete="off" />
                  </Field>
                </div>
                <div className="dx-two">
                  <Field label="Address of a cloud instance">
                    <Select label="Address to use" value={invAddress} onChange={v => setInvAddress(v as 'public' | 'private')}
                      options={[{value: 'public', label: 'Public, or private if it has none'}, {value: 'private', label: 'Private only', hint: 'You reach them through a VPN or a jump host'}]} />
                  </Field>
                  <label className="dx-check" style={{alignSelf: 'end', paddingBottom: 10}}><input type="checkbox" checked={invStopped} onChange={e => setInvStopped(e.target.checked)} /> Include stopped instances</label>
                </div>
              </>
            )}
            <Field label="If a host with the same name already exists">
              <Select label="Conflict policy" value={conflict} onChange={setConflict}
                options={[{value: 'skip', label: 'Skip it', hint: 'Keep the host you have'}, {value: 'overwrite', label: 'Overwrite it', hint: 'Replace it with the one from the file'}, {value: 'duplicate', label: 'Create a copy', hint: 'Keep both'}]} />
            </Field>
            <div className="dx-actions">
              {!(source === 'ssh' ? cfgPath : invPath) && <span className="muted">Choose a file to import.</span>}
              <button className="primary" disabled={!(source === 'ssh' ? cfgPath : invPath) || busy} onClick={() => guard(async () => {
                const r = source === 'ssh'
                  ? await be.data.importSSHConfig(cfgPath, conflict)
                  : await be.data.importInventory(invPath, conflict, {user: invUser.trim(), address: invAddress, group: invGroup.trim(), include_stopped: invStopped})
                setReport(r)
                onChanged()
              })}>Import</button>
            </div>
          </div>
          {report && (
            <div className="dx-result" data-testid="import-report">
              <div className="dx-result-head"><IconCheckCircle size={16} /> Import finished{report.format ? ` (read as ${report.format})` : ''}</div>
              Added {report.hosts_added} hosts, {report.groups_added} groups, {report.forwards_added} forwards; imported {report.keys_imported} keys.
              {(report.skipped?.length ?? 0) > 0 && <div>Skipped: {report.skipped.join(', ')}</div>}
              {(report.warnings ?? []).map((w, i) => <div key={i} className="muted warn-line"><IconWarn size={14} /> {w}</div>)}
            </div>
          )}
        </div>
      )}

      {tab === 'export' && (
        <div className="dx-page" role="tabpanel" aria-label="Export">
          <p className="dx-lead">Take your hosts out of Vutty in a format other tools can read.</p>
          <div className="dx-card">
            <div className="dx-head"><span className="dx-ico"><IconFile size={17} /></span><div><b>SSH config</b><div className="muted">Config files for OpenSSH, with your hosts, groups and port forwards. They go into a folder you choose.</div></div></div>
            <label className="dx-check indent"><input type="checkbox" checked={withKeys} onChange={e => setWithKeys(e.target.checked)} /> Also export private keys</label>
            {withKeys && <div className="dx-warn indent"><IconWarn size={15} /> Private keys will be written to disk unencrypted (mode 0600). Only do this to a location you trust.</div>}
            <div className="dx-actions">
              <button className="primary" disabled={busy} onClick={() => guard(async () => {
                const dir = await choose('Export folder', 'dir')
                if (!dir) return
                const r = await be.data.exportSSHConfig(dir, withKeys)
                setMsg(`Wrote ${(r.files ?? []).length} config file(s) and ${(r.key_files ?? []).length} key file(s).${r.warnings?.length ? ' Warnings: ' + r.warnings.join('; ') : ''}`)
              })}>Export…</button>
            </div>
          </div>
          <div className="dx-card">
            <div className="dx-head"><span className="dx-ico"><IconDownload size={17} /></span><div><b>Host list</b><div className="muted">One JSON file with your hosts and groups. It has no passwords and no keys, so it is safe to share or keep in a repository.</div></div></div>
            <div className="dx-actions">
              <button disabled={busy} onClick={() => guard(async () => {
                const p = await choose('Save host list', 'save', 'vutty-hosts.json')
                if (!p) return
                await be.data.exportPlainJSON(p)
                setMsg('Host list (no secrets) exported.')
              })}>Export host list (JSON, no secrets)</button>
            </div>
          </div>
        </div>
      )}

      {tab === 'backup' && (
        <div className="dx-page" role="tabpanel" aria-label="Backup">
          <p className="dx-lead">An encrypted copy of everything, including saved keys and passwords. Use it to keep a safe copy or to move to another computer.</p>
          <div className="dx-card">
            <Field label="Backup password">
              <input aria-label="Backup password" type="password" autoComplete="new-password" value={bpw} onChange={e => setBpw(e.target.value)} placeholder="At least 8 characters" />
            </Field>
            <div className="dx-hint">
              {pwShort && <b className="bad">{`${MIN_BACKUP_PW - bpw.length} more character${MIN_BACKUP_PW - bpw.length === 1 ? '' : 's'} needed.`} </b>}
              This is not your master password. It locks the backup file, and you will need it again to restore it, so do not lose it.
            </div>
          </div>
          <div className="dx-pair">
            <div className="dx-card">
              <div className="dx-head"><span className="dx-ico"><IconUpload size={17} /></span><div><b>Create a backup</b><div className="muted">Saves a <code>.vubak</code> file.</div></div></div>
              <div className="dx-actions">
                <button className="primary" disabled={bpw.length < MIN_BACKUP_PW || busy} onClick={() => guard(async () => {
                  const p = await choose('Save backup', 'save', 'vutty.vubak')
                  if (!p) return
                  await be.data.exportBackup(p, bpw)
                  setMsg('Backup written.')
                })}>Export backup…</button>
              </div>
            </div>
            <div className="dx-card">
              <div className="dx-head"><span className="dx-ico"><IconKey size={17} /></span><div><b>Restore a backup</b><div className="muted">Brings back its hosts, keys and passwords.</div></div></div>
              <div className="dx-actions">
                <button disabled={!bpw || busy} onClick={() => guard(async () => {
                  const p = await choose('Choose backup', 'file')
                  if (!p) return
                  await be.data.importBackup(p, bpw)
                  onChanged()
                  toast.info('Backup restored')
                })}>Restore backup…</button>
              </div>
            </div>
          </div>
          {!bpw && <div className="muted">Type the backup password first: both buttons use it.</div>}
        </div>
      )}

      {msg && <div className="dx-ok ok" role="status"><IconCheckCircle size={16} /> {msg}</div>}
      {err && <div className="error" role="alert">{err}</div>}
    </Modal>
  )
}
