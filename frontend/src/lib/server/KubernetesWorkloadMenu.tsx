import {useCallback, useEffect, useRef, useState, type ReactNode} from 'react'
import {createPortal} from 'react-dom'
import {useT, type TFn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {Modal} from '../ui/Modal'
import {Select} from '../ui/Select'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Err, Loading} from './shared'
import {Pill, lockText, useK8s, useMenuAnchor} from './KubernetesShared'
import {useCanOpenShell, useStartShell} from './KubernetesPods'
import type {K8sContainerEdit, K8sContainerSpec, K8sEditPreview, K8sPod, K8sWorkload, K8sWorkloadKind} from '../types'

/**
 * The menu of a workload that Rancher offers (Execute shell, Show configuration, Edit config, Add sidecar, Edit YAML, Download YAML),
 * as far as the login's rights go: a button follows the rights the cluster reports, Vutty adds no limit of its own, and what the cluster
 * refuses is shown in its own words. Every change asks first. The changes are requests to the API, so on a host (kubectl over SSH) they
 * are locked with the reason; reading and Download work for both.
 */
type Who = Pick<K8sWorkload, 'kind' | 'name' | 'namespace'>
type Dlg = 'shell' | 'show' | 'config' | 'sidecar' | 'yaml'

const rid = (w: Who) => w.namespace + '/' + w.name
const kindOf = (w: Who) => w.kind.toLowerCase() as K8sWorkloadKind
const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/
const IMAGE_RE = /^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,511}$/
const ENV_RE = /^[-._a-zA-Z][-._a-zA-Z0-9]{0,252}$/
const QTY_RE = /^[0-9]+(\.[0-9]+)?(m|k|M|G|T|P|E|Ki|Mi|Gi|Ti|Pi|Ei)?$/

export const kubeconfigOnly = (t: TFn) => t('Needs a cluster opened with a kubeconfig: on a host use kubectl in a terminal')

/** the pods that a workload owns: the owner text of a pod ends with "Deployment api" (after the ReplicaSet), "StatefulSet db" or "DaemonSet node-exporter" */
const ownedBy = (p: K8sPod, w: Who) => { const o = `${w.kind} ${w.name}`; return p.owner === o || p.owner.endsWith(' ' + o) }

export function WorkloadMenu({w}: {w: Who}) {
  const k = useK8s()
  const t = useT()
  const toast = useToast()
  const {btn, menu, at, setAt, toggle: open, style: menuStyle} = useMenuAnchor()
  const [dlg, setDlg] = useState<Dlg | null>(null)
  const canShell = useCanOpenShell()
  useEffect(() => {
    if (!at) return
    const away = (e: MouseEvent) => { if (!(e.target as Element).closest?.('.k8s-menu') && !btn.current?.contains(e.target as Node)) setAt(null) }
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') setAt(null) }
    document.addEventListener('mousedown', away)
    window.addEventListener('keydown', esc)
    return () => { document.removeEventListener('mousedown', away); window.removeEventListener('keydown', esc) }
  }, [at])
  const download = () => {
    setAt(null)
    void k.run('download:' + rid(w), async () => { toast.info(t('Saved to {path}', {path: await k.be.server.k8sSaveYAML(k.sid, k.ctx, kindOf(w), w.namespace, w.name)})) })
  }
  /** a change is a request to the API: locked on a host, and by the right it needs */
  const item = (id: string, label: string, hint: string, o: {write?: boolean; right?: string; need?: string; run(): void}) => {
    const reason = o.write && !k.direct ? kubeconfigOnly(t) : o.right && !k.can(o.right) ? (o.right === 'edit' ? t('This login may not edit this workload') : o.right === 'restart' ? t('This login may not change this workload') : lockText(t, o.right)) : o.need ?? ''
    return <button key={id} role="menuitem" className="k8s-mi" disabled={!!reason} title={reason || undefined} onClick={() => { setAt(null); o.run() }}><b>{label}</b><small>{hint}</small></button>
  }
  const show = (d: Dlg) => () => setDlg(d)
  const close = () => setDlg(null)
  return (
    <>
      <button ref={btn} aria-haspopup="menu" aria-expanded={!!at} aria-label={t('More actions for {name}', {name: w.name})} title={t('More actions')} onClick={open}>⋮</button>
      {at && createPortal(
        <div ref={menu} className="k8s-menu" role="menu" aria-label={t('Actions for {name}', {name: w.name})} style={menuStyle}>
          <h4>{t('{kind} {name}', {kind: w.kind, name: w.name})}</h4>
          {item('shell', t('Execute shell'), t('A terminal in one of its pods'), {right: 'exec', need: canShell ? '' : t('A terminal cannot be opened from here'), run: show('shell')})}
          {item('show', t('Show configuration'), t('Containers, images, variables, resources'), {run: show('show')})}
          {item('config', t('Edit config'), t('Image, variables, requests and limits of a container'), {write: true, right: 'restart', run: show('config')})}
          {item('sidecar', t('Add sidecar'), t('Another container in the same pods'), {write: true, right: 'restart', run: show('sidecar')})}
          {item('yaml', t('Edit YAML'), t('Change the whole spec, with a preview of what changes'), {write: true, right: 'edit', run: show('yaml')})}
          {item('download', t('Download YAML'), t('Save it in your Downloads folder'), {run: download})}
        </div>, document.body)}
      {dlg && createPortal(<>
        {dlg === 'shell' && <ExecShellDialog w={w} onClose={close} />}
        {dlg === 'show' && <ShowConfigDialog w={w} onClose={close} />}
        {dlg === 'config' && <EditConfigDialog w={w} onClose={close} />}
        {dlg === 'sidecar' && <SidecarDialog w={w} onClose={close} />}
        {dlg === 'yaml' && <EditYamlDialog w={w} onClose={close} />}
      </>, document.body)}
    </>
  )
}

/** loads something once for a dialog; `again` loads it anew */
function useOnce<T>(load: () => Promise<T>) {
  const [state, setState] = useState<{data: T | null; error: string}>({data: null, error: ''})
  const [n, setN] = useState(0)
  const ref = useRef(load)
  ref.current = load
  useEffect(() => {
    let off = false
    setState({data: null, error: ''})
    ref.current().then(data => { if (!off) setState({data, error: ''}) }, e => { if (!off) setState({data: null, error: errMsg(e)}) })
    return () => { off = true }
  }, [n])
  return {...state, again: () => setN(x => x + 1)}
}

// ---- Execute shell ----

/** Execute shell: a pod that is running, and a container when it has several; a single pod of one container opens at once */
function ExecShellDialog({w, onClose}: {w: Who; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const startShell = useStartShell()
  const opened = useRef(false)
  const {data, error} = useOnce(async () => (await k.be.server.k8sPods(k.sid, k.ctx, w.namespace)).filter(p => ownedBy(p, w)).map(p => ({p, usable: p.containers.filter(c => !c.init && c.running)})).filter(x => x.usable.length > 0))
  const go = useCallback((p: K8sPod, container: string) => { startShell(p, container); onClose() }, [startShell, onClose])
  useEffect(() => {
    if (!data || opened.current || data.length !== 1 || data[0].usable.length !== 1) return
    opened.current = true
    go(data[0].p, data[0].usable[0].name)
  }, [data, go])
  const single = !!data && data.length === 1 && data[0].usable.length === 1
  return (
    <Modal title={t('Execute shell in {name}', {name: w.name})} onClose={onClose} footer={<button onClick={onClose}>{t('Cancel')}</button>}>
      <Err error={error} />
      {!data && !error && <Loading kind="lines" />}
      {data && data.length === 0 && <div className="sp-note" role="status">{t('No running pod of {name} was found.', {name: w.name})}</div>}
      {data && data.length > 0 && !single && <>
        <p>{t('Choose the pod, and the container if it has several.')}</p>
        <div className="k8s-pick-list">
          {data.map(({p, usable}) => (
            <div key={p.name} className="k8s-pick-row">
              <span><b>{p.name}</b> <Pill tone={p.tone}>{p.ready.replace('/', ' / ')}</Pill></span>
              <span className="sp-acts">
                {usable.length === 1
                  ? <button aria-label={t('Open a shell in {name}', {name: p.name})} onClick={() => go(p, usable[0].name)}>{t('Shell')}</button>
                  : usable.map(c => <button key={c.name} aria-label={t('Open a shell in {name}, container {container}', {name: p.name, container: c.name})} onClick={() => go(p, c.name)}>{c.name}</button>)}
              </span>
            </div>
          ))}
        </div>
      </>}
    </Modal>
  )
}

// ---- Show configuration ----

const kv = (m: Record<string, string>) => Object.entries(m).map(([a, b]) => a + ' ' + b).join(' · ') || '–'

function ContainerCard({c}: {c: K8sContainerSpec}) {
  const t = useT()
  const row = (label: string, v: ReactNode) => <tr><td>{label}</td><td className="m">{v}</td></tr>
  return (
    <section className="k8s-ctn" aria-label={c.name}>
      <h3>{c.name} {c.init && <Pill>{t('init container')}</Pill>}</h3>
      <table className="sp-kv"><tbody>
        {row(t('Image'), c.image)}
        {c.command.length > 0 && row(t('Command'), c.command.join(' '))}
        {c.args.length > 0 && row(t('Arguments'), c.args.join(' '))}
        {row(t('Ports'), c.ports.join(' · ') || '–')}
        {row(t('Requests'), kv(c.requests))}
        {row(t('Limits'), kv(c.limits))}
        {row(t('Volume mounts'), c.mounts.join(' · ') || '–')}
        {c.env_from.length > 0 && row(t('Variables from'), c.env_from.join(' · '))}
      </tbody></table>
      {c.env.length > 0 && (
        <table className="sp-kv k8s-env" aria-label={t('Variables of {name}', {name: c.name})}><tbody>
          {c.env.map(e => <tr key={e.name}><td className="m">{e.name}</td><td className="m">{e.from ? <span className="sp-note">{t('from {source}', {source: e.from})}</span> : e.value || <span className="sp-note">{t('empty')}</span>}</td></tr>)}
        </tbody></table>
      )}
    </section>
  )
}

function ShowConfigDialog({w, onClose}: {w: Who; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const {data, error} = useOnce(() => k.be.server.k8sWorkloadSpec(k.sid, k.ctx, kindOf(w), w.namespace, w.name))
  return (
    <Modal wide title={t('Configuration of {name}', {name: w.name})} onClose={onClose} footer={<button onClick={onClose}>{t('Close')}</button>}>
      <Err error={error} />
      {!data && !error && <Loading kind="lines" />}
      {data && <>
        <table className="sp-kv"><tbody>
          <tr><td>{t('Namespace')}</td><td className="m">{data.namespace}</td></tr>
          {data.replicas !== null && <tr><td>{t('Replicas')}</td><td className="m">{data.replicas}</td></tr>}
          <tr><td>{t('Service account')}</td><td className="m">{data.service_account || 'default'}</td></tr>
        </tbody></table>
        {data.containers.map(c => <ContainerCard key={(c.init ? 'i:' : 'c:') + c.name} c={c} />)}
      </>}
    </Modal>
  )
}

// ---- Edit config ----

interface EnvRow {name: string; value: string; from: string; original: string | null; removed: boolean}

/** what the form changed in a container: only that is sent */
export function editOf(c: K8sContainerSpec, d: {image: string; requests: Record<string, string>; limits: Record<string, string>; env: EnvRow[]}): {edit: K8sContainerEdit; problem: string; changed: boolean} {
  const edit: K8sContainerEdit = {container: c.name}
  if (c.init) edit.init = true
  let problem = ''
  const image = d.image.trim()
  if (image !== c.image) { if (IMAGE_RE.test(image)) edit.image = image; else problem = 'image' }
  for (const [key, now, was] of [['requests', d.requests, c.requests], ['limits', d.limits, c.limits]] as const) {
    const out: Record<string, string> = {}
    for (const r of ['cpu', 'memory']) {
      const n = (now[r] ?? '').trim()
      if (n === (was[r] ?? '')) continue
      if (n !== '' && !QTY_RE.test(n)) problem = problem || key + ' ' + r
      out[r] = n
    }
    if (Object.keys(out).length) edit[key] = out
  }
  const env: NonNullable<K8sContainerEdit['env']> = []
  const names = new Set(d.env.filter(r => r.original !== null && !r.removed).map(r => r.name))
  for (const r of d.env) {
    if (r.original === null) {
      if (r.removed || (r.name.trim() === '' && r.value === '')) continue
      if (!ENV_RE.test(r.name.trim())) { problem = problem || 'env ' + r.name; continue }
      if (names.has(r.name.trim())) { problem = problem || 'env duplicate ' + r.name; continue }
      names.add(r.name.trim())
      env.push({name: r.name.trim(), value: r.value})
    } else if (r.removed) env.push({name: r.name, value: '', remove: true})
    else if (!r.from && r.value !== r.original) env.push({name: r.name, value: r.value})
  }
  if (env.length) edit.env = env
  return {edit, problem, changed: !!(edit.image || edit.requests || edit.limits || edit.env)}
}

function EditConfigDialog({w, onClose}: {w: Who; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const {data, error} = useOnce(() => k.be.server.k8sWorkloadSpec(k.sid, k.ctx, kindOf(w), w.namespace, w.name))
  const [pick, setPick] = useState('')
  const c = data?.containers.find(x => (x.init ? 'i:' : 'c:') + x.name === pick) ?? data?.containers.find(x => !x.init) ?? data?.containers[0]
  return (
    <Modal wide title={t('Edit config of {name}', {name: w.name})} onClose={onClose}>
      <Err error={error} />
      {!data && !error && <Loading kind="lines" />}
      {data && c && <>
        <div className="sp-note">{t('Only what you change is sent. The pods are replaced by new ones that have the new settings.')}</div>
        <Select label={t('Container')} value={(c.init ? 'i:' : 'c:') + c.name} onChange={setPick}
          options={data.containers.map(x => ({value: (x.init ? 'i:' : 'c:') + x.name, label: x.name + (x.init ? ' (' + t('init container') + ')' : '')}))} />
        <ConfigForm key={(c.init ? 'i:' : 'c:') + c.name} w={w} c={c} onClose={onClose} />
      </>}
    </Modal>
  )
}

function ConfigForm({w, c, onClose}: {w: Who; c: K8sContainerSpec; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const confirm = useConfirm()
  const [image, setImage] = useState(c.image)
  const [requests, setRequests] = useState<Record<string, string>>({cpu: c.requests.cpu ?? '', memory: c.requests.memory ?? ''})
  const [limits, setLimits] = useState<Record<string, string>>({cpu: c.limits.cpu ?? '', memory: c.limits.memory ?? ''})
  const [env, setEnv] = useState<EnvRow[]>(c.env.map(e => ({name: e.name, value: e.value, from: e.from, original: e.value, removed: false})))
  const {edit, problem, changed} = editOf(c, {image, requests, limits, env})
  const setRow = (i: number, p: Partial<EnvRow>) => setEnv(r => r.map((x, j) => (j === i ? {...x, ...p} : x)))
  const apply = async () => {
    if (!(await confirm(t('Change the container {container} of the {kind} {name} in {ns} on {ctx}? Its pods are replaced by new ones with the new settings.', {container: c.name, kind: w.kind, name: w.name, ns: w.namespace, ctx: k.ctx}), {confirmLabel: t('Apply')}))) return
    onClose()
    void k.run('config:' + rid(w), () => k.be.server.k8sSetContainer(k.sid, k.ctx, kindOf(w), w.namespace, w.name, edit), t('{name}: configuration changed', {name: w.name}))
  }
  const q = (label: string, set: typeof setRequests, v: Record<string, string>, key: string) => (
    <label className="field"><span>{label}</span><input aria-label={label} value={v[key]} placeholder="–" spellCheck={false} onChange={e => set(x => ({...x, [key]: e.target.value}))} /></label>
  )
  return (
    <>
      <label className="field"><span>{t('Image')}</span><input aria-label={t('Image')} value={image} spellCheck={false} onChange={e => setImage(e.target.value)} /></label>
      <div className="k8s-two">
        {q(t('CPU request'), setRequests, requests, 'cpu')}{q(t('Memory request'), setRequests, requests, 'memory')}
        {q(t('CPU limit'), setLimits, limits, 'cpu')}{q(t('Memory limit'), setLimits, limits, 'memory')}
      </div>
      <h3 className="k8s-h2">{t('Variables')}</h3>
      <table className="sp-kv k8s-env" aria-label={t('Variables')}><tbody>
        {env.map((r, i) => r.removed ? null : (
          <tr key={i}>
            <td className="m">{r.original === null ? <input aria-label={t('Variable name')} value={r.name} placeholder={t('NAME')} spellCheck={false} onChange={e => setRow(i, {name: e.target.value})} /> : r.name}</td>
            <td className="m">
              {r.from ? <span className="sp-note">{t('from {source}', {source: r.from})}</span> : <input aria-label={r.original === null ? t('Variable value') : t('Value of {name}', {name: r.name})} value={r.value} spellCheck={false} onChange={e => setRow(i, {value: e.target.value})} />}
            </td>
            <td><button aria-label={t('Remove {name}', {name: r.name || t('the variable')})} onClick={() => (r.original === null ? setEnv(x => x.filter((_, j) => j !== i)) : setRow(i, {removed: true}))}>{t('Remove')}</button></td>
          </tr>
        ))}
      </tbody></table>
      <button onClick={() => setEnv(r => [...r, {name: '', value: '', from: '', original: null, removed: false}])}>{t('Add variable')}</button>
      {problem && <div className="sp-note k8s-hint" role="alert">{problem.startsWith('image') ? t('That is not an image name.') : problem.startsWith('env duplicate') ? t('A variable name is given twice.') : problem.startsWith('env') ? t('A variable name is letters, digits, -, _ and . and does not start with a digit.') : t('A quantity looks like 250m, 1, 256Mi or 1Gi.')}</div>}
      <div className="modal-foot-inline">
        <button onClick={onClose}>{t('Cancel')}</button>
        <button className="primary" disabled={!changed || !!problem} onClick={() => void apply()}>{t('Apply')}</button>
      </div>
    </>
  )
}

// ---- Add sidecar ----

const lines = (s: string) => s.split('\n').map(x => x.trim()).filter(Boolean)

function SidecarDialog({w, onClose}: {w: Who; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const confirm = useConfirm()
  const [name, setName] = useState('')
  const [image, setImage] = useState('')
  const [command, setCommand] = useState('')
  const [args, setArgs] = useState('')
  const nameOk = NAME_RE.test(name) && name.length <= 63
  const imageOk = IMAGE_RE.test(image)
  const add = async () => {
    if (!nameOk || !imageOk) return
    if (!(await confirm(t('Add the sidecar {container} ({image}) to the {kind} {name} in {ns} on {ctx}? Its pods are replaced by new ones that run it too.', {container: name, image, kind: w.kind, name: w.name, ns: w.namespace, ctx: k.ctx}), {confirmLabel: t('Add sidecar')}))) return
    onClose()
    void k.run('sidecar:' + rid(w), () => k.be.server.k8sAddSidecar(k.sid, k.ctx, kindOf(w), w.namespace, w.name, {name, image, ...(lines(command).length ? {command: lines(command)} : {}), ...(lines(args).length ? {args: lines(args)} : {})}), t('{name}: sidecar {container} added', {name: w.name, container: name}))
  }
  return (
    <Modal title={t('Add a sidecar to {name}', {name: w.name})} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={!nameOk || !imageOk} onClick={() => void add()}>{t('Add sidecar')}</button></>}>
      <div className="sp-note">{t('A sidecar is another container that runs in the same pods. To change a container that is there, use Edit config.')}</div>
      <label className="field"><span>{t('Container name')}</span><input aria-label={t('Container name')} value={name} spellCheck={false} onChange={e => setName(e.target.value)} /></label>
      {name !== '' && !nameOk && <div className="sp-note k8s-hint" role="alert">{t('A container name is lowercase letters, digits and - (63 at most).')}</div>}
      <label className="field"><span>{t('Image')}</span><input aria-label={t('Image')} value={image} placeholder="busybox:1.36" spellCheck={false} onChange={e => setImage(e.target.value)} /></label>
      {image !== '' && !imageOk && <div className="sp-note k8s-hint" role="alert">{t('That is not an image name.')}</div>}
      <label className="field"><span>{t('Command (one word per line, optional)')}</span><textarea aria-label={t('Command')} rows={3} value={command} spellCheck={false} onChange={e => setCommand(e.target.value)} /></label>
      <label className="field"><span>{t('Arguments (one per line, optional)')}</span><textarea aria-label={t('Arguments')} rows={3} value={args} spellCheck={false} onChange={e => setArgs(e.target.value)} /></label>
    </Modal>
  )
}

// ---- Edit YAML ----

function EditYamlDialog({w, onClose}: {w: Who; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const confirm = useConfirm()
  const {data, error: loadError, again} = useOnce(() => k.be.server.k8sEditable(k.sid, k.ctx, kindOf(w), w.namespace, w.name))
  const [text, setText] = useState('')
  const [preview, setPreview] = useState<(K8sEditPreview & {text: string}) | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (data) { setText(data.yaml); setPreview(null); setErr('') } }, [data])
  const conflict = /the object has been modified/i.test(err)
  const run = async () => {
    setBusy(true); setErr('')
    try { setPreview({...(await k.be.server.k8sEditPreview(k.sid, k.ctx, kindOf(w), w.namespace, w.name, text)), text}) } catch (e) { setPreview(null); setErr(errMsg(e)) }
    setBusy(false)
  }
  const ready = !!preview && preview.text === text && preview.changed > 0 && !busy
  const apply = async () => {
    if (!preview || !(await confirm(t('Replace the {kind} {name} in {ns} on {ctx} with this YAML? {n} lines change; if the pod template changed, the pods are replaced.', {kind: w.kind, name: w.name, ns: w.namespace, ctx: k.ctx, n: preview.changed}), {confirmLabel: t('Apply')}))) return
    const ok = await k.run('yaml:' + rid(w), async () => { try { await k.be.server.k8sEditApply(k.sid, k.ctx, kindOf(w), w.namespace, w.name, text) } catch (e) { setErr(errMsg(e)); throw e } }, t('{name}: updated', {name: w.name}))
    if (ok) onClose()
  }
  const reload = <button onClick={again}>{t('Reload')}</button>
  return (
    <Modal wide title={t('Edit YAML of {name}', {name: w.name})} onClose={onClose}
      footer={<>{!conflict && reload}<button onClick={onClose}>{t('Cancel')}</button><button disabled={!data || busy} onClick={() => void run()}>{t('Preview')}</button><button className="primary" disabled={!ready} onClick={() => void apply()}>{t('Apply')}</button></>}>
      <Err error={loadError} />
      {!data && !loadError && <Loading kind="lines" />}
      {data && <>
        <div className="sp-note">{t('Preview asks the cluster to check the YAML without changing anything, and shows what would change. Apply is possible after a preview of this text.')}</div>
        <textarea className="k8s-yaml-edit" aria-label={t('YAML')} spellCheck={false} value={text} onChange={e => { setText(e.target.value) }} />
        {err && <div className="k8s-warnbox" role="alert">{conflict ? <>{t('Changed by someone else: reload')} {reload}</> : err}</div>}
        {preview && preview.text === text && (preview.changed === 0
          ? <div className="sp-note" role="status">{t('No changes: this is what the cluster has.')}</div>
          : <pre className="k8s-diff" aria-label={t('Changes')}>{preview.diff.map((l, i) => <div key={i} className={l.op === '+' ? 'add' : l.op === '-' ? 'del' : l.op === '~' ? 'skip' : ''}>{l.op === '~' ? '…' : l.op + ' ' + l.text}</div>)}</pre>)}
      </>}
    </Modal>
  )
}

