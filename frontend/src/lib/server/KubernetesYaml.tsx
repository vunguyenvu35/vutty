import {useCallback, useMemo, useState, type ReactNode} from 'react'
import {useT} from '../i18n'
import {IconCopy, IconDownload} from '../ui/Icons'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Err, Toolbar} from './shared'
import {Crumb, useK8s, useKLoad, type K8sPage} from './KubernetesShared'

type YamlPage = Extract<K8sPage, {k: 'yaml'}>

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** a line with the text found marked; without a search the keys and comments get their colour */
function Line({text, find}: {text: string; find: RegExp | null}): ReactNode {
  if (find) {
    const out: ReactNode[] = []
    let at = 0
    find.lastIndex = 0
    for (let m = find.exec(text); m; m = find.exec(text)) {
      if (m[0] === '') { find.lastIndex++; continue }
      if (m.index > at) out.push(text.slice(at, m.index))
      out.push(<mark key={m.index}>{m[0]}</mark>)
      at = m.index + m[0].length
    }
    if (at > 0) { if (at < text.length) out.push(text.slice(at)); return out }
  }
  const c = text.indexOf(' #')
  const body = c < 0 ? text : text.slice(0, c)
  const m = /^(\s*(?:- )*)([A-Za-z0-9_.\-/]+:)(.*)$/.exec(body)
  return <>{m ? <>{m[1]}<span className="k">{m[2]}</span>{m[3]}</> : body}{c >= 0 && <span className="c">{text.slice(c)}</span>}</>
}

/** Screen 18: the YAML of an object, read only, with a find box, Copy and Download */
export function YamlPage({page}: {page: YamlPage}) {
  const k = useK8s()
  const t = useT()
  const toast = useToast()
  const [q, setQ] = useState('')
  const load = useCallback(() => k.be.server.k8sYAML(k.sid, k.ctx, page.kind, page.ns, page.name), [k.be, k.sid, k.ctx, page.kind, page.ns, page.name])
  const {data, error, busy, reload} = useKLoad<string>(load, false)
  const lines = useMemo(() => (data ?? '').replace(/\n$/, '').split('\n'), [data])
  const find = useMemo(() => (q.trim() ? new RegExp(escapeRe(q.trim()), 'gi') : null), [q])
  const hits = find ? lines.filter(l => { find.lastIndex = 0; return find.test(l) }).length : 0
  const copy = async () => { try { await k.be.system.writeClipboard(data ?? ''); toast.info(t('Copied')) } catch (e) { toast.error(errMsg(e)) } }
  const save = async () => {
    try {
      const path = await k.be.system.saveText(t('Save YAML'), `${page.kind}-${page.name}.yaml`, data ?? '')
      if (path) toast.info(t('Saved to {path}', {path}))
    } catch (e) { toast.error(errMsg(e)) }
  }
  const cmd = `kubectl get ${page.kind} ${page.name}${page.ns ? ' -n ' + page.ns : ''} -o yaml`
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}><Crumb path={[t('Back'), page.label, t('YAML')]} onBack={() => k.open(page.back)} /></Toolbar>
      <div className="sp-bar">
        <input className="sp-filter" aria-label={t('Find in YAML')} placeholder={t('Find in YAML')} value={q} onChange={e => setQ(e.target.value)} />
        <span className="sp-note" role="status">{find ? t('{n} lines match', {n: hits}) : t('read-only · {cmd}', {cmd})}</span>
        <span className="sp-acts k8s-grow">
          <button disabled={!data} onClick={() => void copy()}><IconCopy size={14} /> {t('Copy')}</button>
          <button disabled={!data} onClick={() => void save()}><IconDownload size={14} /> {t('Download')}</button>
        </span>
      </div>
      <Err error={error} />
      <div className="k8s-code" role="region" aria-label={t('YAML of {name}', {name: page.label})}>
        {data !== null && lines.map((l, i) => <div key={i} className="ln"><i>{i + 1}</i><span><Line text={l} find={find} /></span></div>)}
      </div>
      <div className="sp-note">{t('managedFields are left out. Secret objects are shown with their data replaced by "redacted". Nothing here can be edited: a change goes through the buttons, which ask first.')}</div>
    </div>
  )
}
