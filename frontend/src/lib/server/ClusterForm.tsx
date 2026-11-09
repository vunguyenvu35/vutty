import {useEffect, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {Section, SidePanel} from '../ui/Modal'
import {Select} from '../ui/Select'
import {errMsg} from '../util'
import {IconUpload} from '../ui/Icons'
import type {Cluster, ClusterPreview, Env, Group, KubeContext} from '../types'

/** how a kubeconfig signs in, in words */
function authText(t: ReturnType<typeof useT>, c: KubeContext): string {
  switch (c.auth) {
    case 'token': return t('a token')
    case 'certificate': return t('a client certificate')
    case 'password': return t('a user name and password')
    case 'exec': return t('a login program')
    default: return t('no credentials')
  }
}

/** The contexts that a kubeconfig holds: the one to use, how each signs in (a login program is shown in full), and why a refused one is refused. */
function Contexts({preview, choice, onChoice}: {preview: ClusterPreview; choice: string; onChoice(name: string): void}) {
  const t = useT()
  const refused = Object.entries(preview.problems)
  return (
    <>
      {preview.contexts.length > 0 && (
        <div role="radiogroup" aria-label={t('Context')} className="k8s-opts">
          {preview.contexts.map(c => (
            <button key={c.name} className="k8s-opt" role="radio" aria-checked={choice === c.name} onClick={() => onChoice(c.name)}>
              <span className="k8s-rad" />
              <span>
                <b>{c.name}</b>
                <small>{c.server} · {t('user {name}', {name: c.user})} · {t('signs in with {how}', {how: authText(t, c)})}</small>
                {c.exec && <small className="m" aria-label={t('Login program')}>{c.exec}</small>}
                {c.insecure && <small className="warn-text" role="alert">{t('The certificate of this server is not checked (insecure-skip-tls-verify). Anyone between you and the server could read what is sent.')}</small>}
              </span>
              {c.current && <span className="sp-pill">{t('current in kubeconfig')}</span>}
            </button>
          ))}
        </div>
      )}
      {preview.contexts.some(c => c.exec) && <div className="muted">{t('A login program runs on this computer to sign in. The first time you open the cluster you are asked to approve it.')}</div>}
      {refused.length > 0 && (
        <div role="alert" className="k8s-raw">
          <b>{t('Not usable:')}</b>
          {refused.map(([name, why]) => <div key={name}>{name}: {why}</div>)}
        </div>
      )}
      {preview.contexts.length === 0 && <div className="error" role="alert">{t('This kubeconfig has no context that can be used.')}</div>}
    </>
  )
}

/** Chooses a kubeconfig: a file picked in the dialog of the system (its text never passes through this screen), or pasted text when no dialog is available. */
function Picker({onPreview}: {onPreview(r: {path: string; text: string; preview: ClusterPreview} | null): void}) {
  const be = useBackend()
  const t = useT()
  const [path, setPath] = useState('')
  const [text, setText] = useState('')
  const [paste, setPaste] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const read = async (p: string, x: string) => {
    setErr('')
    setBusy(true)
    try {
      const preview = await be.clusters.preview(p, x)
      onPreview({path: p, text: x, preview})
    } catch (e) {
      onPreview(null)
      setErr(errMsg(e))
    } finally { setBusy(false) }
  }
  const choose = async () => {
    let p = ''
    try { p = await be.system.pickFile(t('Choose a kubeconfig file')) } catch { setPaste(true); return } // no dialog here: paste it
    if (!p) return
    setPath(p)
    setText('')
    await read(p, '')
  }
  return (
    <>
      <div className="row">
        <button onClick={() => void choose()} disabled={busy}>{t('Choose file…')}</button>
        {path && <code className="muted" aria-label={t('Chosen file')}>{path}</code>}
        <button className="ghost" aria-pressed={paste} onClick={() => setPaste(p => !p)}>{paste ? t('Hide the text box') : t('Paste the text instead')}</button>
      </div>
      {paste && (
        <>
          <textarea aria-label={t('Kubeconfig text')} className="script-area" rows={8} spellCheck={false} value={text} placeholder={'apiVersion: v1\nkind: Config\n…'}
            onChange={e => { setText(e.target.value); setPath('') }} />
          <div className="row"><button disabled={busy || !text.trim()} onClick={() => void read('', text)}>{t('Read the text')}</button></div>
        </>
      )}
      {err && <div className="error" role="alert">{err}</div>}
    </>
  )
}

/** not the id of a group: the choice that opens the box for a new one */
const NEW_GROUP = '\u0000new-group'

/** The group of a cluster: the groups that exist, and "New group…", which asks for a name and makes the group right here (so the first group does not need another page). */
function GroupPicker({groups, value, onChange, onCreated}: {groups: Group[]; value: string; onChange(id: string): void; onCreated?(g: Group): void}) {
  const be = useBackend()
  const t = useT()
  const [made, setMade] = useState<Group[]>([])
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [err, setErr] = useState('')
  const all = [...groups, ...made.filter(m => !groups.some(g => g.id === m.id))]
  const cancel = () => { setAdding(false); setName(''); setErr('') }
  const create = async () => {
    const n = name.trim()
    if (!n) { setErr(t('A group needs a name.')); return }
    try {
      const g = await be.hosts.saveGroup({name: n})
      setMade(m => [...m, g])
      onChange(g.id)
      onCreated?.(g)
      cancel()
    } catch (e) { setErr(errMsg(e)) }
  }
  return (
    <>
      <Select label={t('Group')} value={adding ? NEW_GROUP : value} onChange={v => { if (v === NEW_GROUP) setAdding(true); else { setAdding(false); onChange(v) } }}
        options={[{value: '', label: t('No group')}, ...all.map(g => ({value: g.id, label: g.name})), {value: NEW_GROUP, label: t('New group…')}]} />
      {adding && (
        <div className="k8s-add-ns">
          <input autoFocus aria-label={t('Group name')} value={name} placeholder={t('Group name')} onChange={e => { setName(e.target.value); setErr('') }}
            onKeyDown={e => { if (e.key === 'Enter') void create(); if (e.key === 'Escape') cancel() }} />
          <button onClick={() => void create()}>{t('Create')}</button>
          <button onClick={cancel}>{t('Cancel')}</button>
        </div>
      )}
      {err && <div className="error" role="alert">{err}</div>}
    </>
  )
}

/** The namespace a cluster opens on. Empty leaves the choice to the screen; the namespace the kubeconfig names for its context is shown as the hint. */
function DefaultNamespace({value, onChange, fallback}: {value: string; onChange(v: string): void; fallback?: string}) {
  const t = useT()
  return (
    <>
      <input aria-label={t('Default namespace')} value={value} spellCheck={false} autoComplete="off" onChange={e => onChange(e.target.value)} placeholder={fallback ? t('Default namespace (the file names {ns})', {ns: fallback}) : t('Default namespace (optional)')} />
      <div className="muted">{t('The namespace this cluster opens on. Leave it empty to choose each time.')}</div>
    </>
  )
}

const ENV_OPTIONS = (t: ReturnType<typeof useT>) => [{value: '', label: t('No environment')}, {value: 'dev', label: 'dev'}, {value: 'staging', label: 'staging'}, {value: 'prod', label: t('prod (production)')}]

/** Add a cluster from a kubeconfig, or (with `replace`) give a cluster a new kubeconfig. */
export function ClusterAddForm({groups, replace, onClose, onSaved, onGroupCreated}: {groups: Group[]; replace?: Cluster; onClose(): void; onSaved(c: Cluster): void; onGroupCreated?(g: Group): void}) {
  const be = useBackend()
  const t = useT()
  const [read, setRead] = useState<{path: string; text: string; preview: ClusterPreview} | null>(null)
  const [choice, setChoice] = useState('')
  const [label, setLabel] = useState('')
  const [env, setEnv] = useState<Env>('')
  const [groupId, setGroupId] = useState('')
  const [namespace, setNamespace] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const preview = read?.preview
  const onPreview = (r: typeof read) => {
    setRead(r)
    setErr('')
    const list = r?.preview.contexts ?? []
    setChoice((list.find(c => c.current) ?? list[0])?.name ?? '')
  }
  const chosen = preview?.contexts.find(c => c.name === choice)
  const save = async () => {
    if (!read || !chosen) return
    setErr('')
    setBusy(true)
    try {
      const c = replace
        ? await be.clusters.replaceKubeconfig(replace.id, read.path, read.text)
        : await be.clusters.add({label: label.trim() || chosen.cluster, group_id: groupId, env, path: read.path, kubeconfig: read.text, context: choice, default_namespace: namespace.trim()})
      onSaved(c)
    } catch (e) { setErr(errMsg(e)); setBusy(false) }
  }
  return (
    <SidePanel title={replace ? t('Replace the kubeconfig of {name}', {name: replace.label}) : t('Add cluster')} onClose={onClose}
      footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={!chosen || busy} onClick={() => void save()}>{replace ? t('Replace') : t('Add')}</button></>}>
      <Section title={t('Kubeconfig')}>
        <div className="muted">{t('Stored encrypted in your vault and never shown again. Use only a kubeconfig from someone you trust.')}</div>
        <Picker onPreview={onPreview} />
      </Section>
      {preview && (
        <Section title={t('Contexts in this file')}>
          <Contexts preview={preview} choice={choice} onChoice={setChoice} />
        </Section>
      )}
      {!replace && preview && chosen && (
        <Section title={t('General')}>
          <input aria-label={t('Cluster name')} value={label} onChange={e => setLabel(e.target.value)} placeholder={chosen.cluster} />
          <Select label={t('Environment')} value={env} onChange={v => setEnv(v as Env)} options={ENV_OPTIONS(t)} />
          <GroupPicker groups={groups} value={groupId} onChange={setGroupId} onCreated={onGroupCreated} />
          <DefaultNamespace value={namespace} onChange={setNamespace} fallback={chosen.namespace} />
        </Section>
      )}
      {replace && <div className="muted">{t('Login programs that you approved for the old file are not approved for the new one.')}</div>}
      {err && <div className="error" role="alert">{err}</div>}
    </SidePanel>
  )
}

/** Name, environment, group and the context a cluster opens with. The kubeconfig stays as it is. */
export function ClusterEditForm({cluster, groups, onClose, onSaved, onGroupCreated, onReplace, onChanged}: {cluster: Cluster; groups: Group[]; onClose(): void; onSaved(c: Cluster): void; onGroupCreated?(g: Group): void; /** the panel of a new kubeconfig for this cluster */ onReplace?(): void; /** something was changed without saving the form (an approval taken back) */ onChanged?(): void}) {
  const be = useBackend()
  const t = useT()
  const [label, setLabel] = useState(cluster.label)
  const [env, setEnv] = useState<Env>(cluster.env ?? '')
  const [groupId, setGroupId] = useState(cluster.group_id ?? '')
  const [context, setContext] = useState(cluster.context ?? '')
  const [namespace, setNamespace] = useState(cluster.default_namespace ?? '')
  const [contexts, setContexts] = useState<KubeContext[]>([])
  const [approved, setApproved] = useState<string[]>(cluster.approved_exec ?? [])
  const [err, setErr] = useState('')
  const revoke = async (command: string) => {
    setErr('')
    try { const c = await be.clusters.revokeExec(cluster.id, command); setApproved(c.approved_exec ?? []); onChanged?.() } catch (e) { setErr(errMsg(e)) }
  }
  useEffect(() => { let off = false; be.clusters.describe(cluster.id).then(p => { if (!off) setContexts(p.contexts) }, () => {}); return () => { off = true } }, [be, cluster.id])
  const save = async () => {
    setErr('')
    if (!label.trim()) return setErr(t('Name is required'))
    try { onSaved(await be.clusters.update({...cluster, label: label.trim(), env, group_id: groupId || undefined, context, default_namespace: namespace.trim()})) } catch (e) { setErr(errMsg(e)) }
  }
  return (
    <SidePanel title={t('Edit cluster')} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" onClick={() => void save()}>{t('Save')}</button></>}>
      <Section title={t('General')}>
        <input aria-label={t('Cluster name')} value={label} onChange={e => setLabel(e.target.value)} autoFocus />
        <Select label={t('Environment')} value={env} onChange={v => setEnv(v as Env)} options={ENV_OPTIONS(t)} />
        <GroupPicker groups={groups} value={groupId} onChange={setGroupId} onCreated={onGroupCreated} />
        <DefaultNamespace value={namespace} onChange={setNamespace} />
        {contexts.length > 1 && <Select label={t('Context')} value={context} onChange={setContext} options={contexts.map(c => ({value: c.name, label: c.name, hint: c.server}))} />}
      </Section>
      <Section title={t('Kubeconfig')}>
        <div className="muted">{t('Stored encrypted in the vault and never shown again. It can only be replaced by a new file.')}</div>
        {contexts.map(c => (
          <div key={c.name} className="k8s-ctx-line">
            <b>{c.name}</b> <span className="m">{c.server}</span>
            <small>{[c.user, c.auth === 'exec' ? t('login program: {command}', {command: c.exec ?? ''}) : c.auth === 'none' ? '' : c.auth, c.insecure ? t('certificate not checked') : ''].filter(Boolean).join(' · ')}</small>
          </div>
        ))}
        {onReplace && <button aria-label={t('Replace the kubeconfig')} onClick={onReplace}><IconUpload size={14} /> {t('Replace kubeconfig…')}</button>}
      </Section>
      <Section title={t('Login programs you approved')}>
        {approved.length === 0 && <div className="muted">{t('No login program was approved for this cluster.')}</div>}
        {approved.map(command => (
          <div key={command} className="k8s-ctx-line">
            <span className="m">{command}</span>
            <button className="ghost" aria-label={t('Revoke {command}', {command})} onClick={() => void revoke(command)}>{t('Revoke')}</button>
          </div>
        ))}
      </Section>
      {err && <div className="error" role="alert">{err}</div>}
    </SidePanel>
  )
}
