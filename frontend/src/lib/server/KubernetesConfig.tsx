import {useCallback, useEffect, useRef, useState} from 'react'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {Modal} from '../ui/Modal'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Card, Err, Loading, RefreshButton, Toolbar} from './shared'
import {level} from './format'
import {ActBtn, Age, matches, Pill, useK8s, useKept, useKLoad} from './KubernetesShared'
import type {K8sConfig, K8sConfigItem} from '../types'

/** how long a revealed Secret key stays on the screen */
export const REVEAL_MS = 30000

function ConfigMapDialog({item, onClose}: {item: K8sConfigItem; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const [data, setData] = useState<Record<string, string> | null>(null)
  const [error, setError] = useState('')
  useEffect(() => { k.be.server.k8sConfigMap(k.sid, k.ctx, item.namespace, item.name).then(setData, e => setError(errMsg(e))) }, [k.be, k.sid, k.ctx, item.namespace, item.name])
  return (
    <Modal wide title={t('ConfigMap {name}', {name: item.name})} onClose={onClose} footer={<button onClick={onClose}>{t('Close')}</button>}>
      <Err error={error} />
      {data && Object.keys(data).length === 0 && <div className="sp-note">{t('No keys.')}</div>}
      {data && Object.entries(data).map(([key, v]) => <div key={key}><h3 className="k8s-key">{key}</h3><pre className="sp-pre">{v}</pre></div>)}
    </Modal>
  )
}

/** Screen 13: ConfigMaps (View), Secrets (names and keys only; Reveal shows one key for 30 s) and volumes with their use */
export function KubernetesConfig() {
  const k = useK8s()
  const t = useT()
  const confirm = useConfirm()
  const toast = useToast()
  const [viewing, setViewing] = useState<K8sConfigItem | null>(null)
  const [choosing, setChoosing] = useState('')
  const [fc, setFc] = useKept('config.maps.filter', '') // what each box is searched for, and which are closed
  const [fsec, setFsec] = useKept('config.secrets.filter', '')
  const [fv, setFv] = useKept('config.volumes.filter', '')
  const [closedM, setClosedM] = useKept('config.maps.closed', false)
  const [closedS, setClosedS] = useKept('config.secrets.closed', false)
  const [closedV, setClosedV] = useKept('config.volumes.closed', false)
  const [shown, setShown] = useState<{secret: string; key: string; value: string} | null>(null)
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const load = useCallback(() => k.be.server.k8sConfig(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data, error, busy, reload} = useKLoad<K8sConfig>(load, false)
  const hide = () => { window.clearTimeout(timer.current); setShown(null) }
  const reveal = async (s: K8sConfigItem, key: string) => {
    setChoosing('')
    if (!(await confirm(t('Reveal the key {key} of the Secret {name} in {ns} on {ctx}? It is shown for 30 seconds and written to the audit log.', {key, name: s.name, ns: s.namespace, ctx: k.ctx}), {confirmLabel: t('Reveal'), danger: true}))) return
    let value = ''
    const ok = await k.run('reveal:' + s.namespace + '/' + s.name, async () => { value = await k.be.server.k8sSecretKey(k.sid, k.ctx, s.namespace, s.name, key) })
    if (!ok) return
    window.clearTimeout(timer.current)
    setShown({secret: s.name, key, value})
    timer.current = window.setTimeout(() => { setShown(null); toast.info(t('The secret is hidden again.')) }, REVEAL_MS)
  }
  const used = (l: string[]) => (l.length ? l.join(', ') : '–')
  const maps = (data?.configmaps ?? []).filter(c => matches(fc, c.name, c.namespace, ...c.keys, ...c.used_by))
  const secrets = (data?.secrets ?? []).filter(s => matches(fsec, s.name, s.namespace, s.type, ...s.keys, ...s.used_by))
  const volumes = (data?.volumes ?? []).filter(v => matches(fv, v.name, v.namespace, v.status, v.storage_class, ...v.used_by))
  const counted = (shown: number, all: number, q: string) => (q.trim() ? `${shown} / ${all}` : all)
  if (!data && !error) return <div className="sp-view"><Loading kind="table" what={t('Loading config and storage…')} /></div>
  return (
    <div className="sp-view">
      <Err error={error} />
      <Card title={t('ConfigMaps')} collapsible closed={closedM} onToggle={() => setClosedM(!closedM)} count={counted(maps.length, data?.configmaps.length ?? 0, fc)} filter={{value: fc, onChange: setFc, label: t('Search ConfigMaps')}} right={<RefreshButton busy={busy} onReload={reload} />}>
        <div className="sp-table"><table>
          <thead><tr><th>{t('Name')}</th><th>{t('Keys')}</th><th>{t('Used by')}</th><th className="n">{t('Age')}</th><th /></tr></thead>
          <tbody>{maps.map(c => (
            <tr key={c.namespace + '/' + c.name}>
              <td className="k8s-name"><b>{c.name}</b><small>{c.namespace}</small></td><td className="m">{used(c.keys)}</td><td className="m">{used(c.used_by)}</td><td className="n"><Age sec={c.age_sec} /></td>
              <td><div className="sp-acts"><button aria-label={t('View {name}', {name: c.name})} onClick={() => setViewing(c)}>{t('View')}</button></div></td>
            </tr>))}</tbody>
        </table>{data && data.configmaps.length === 0 && <div className="sp-empty">{t('No ConfigMaps.')}</div>}{data && data.configmaps.length > 0 && maps.length === 0 && <div className="sp-empty">{t('No ConfigMap matches.')}</div>}</div>
      </Card>
      <Card title={t('Secrets')} collapsible closed={closedS} onToggle={() => setClosedS(!closedS)} count={counted(secrets.length, data?.secrets.length ?? 0, fsec)} filter={{value: fsec, onChange: setFsec, label: t('Search Secrets')}} right={<span className="sp-note">{t('names and keys only')}</span>}>
        <div className="sp-table"><table>
          <thead><tr><th>{t('Name')}</th><th>{t('Type')}</th><th>{t('Keys')}</th><th>{t('Used by')}</th><th className="n">{t('Age')}</th><th /></tr></thead>
          <tbody>{secrets.map(s => {
            const id = 'reveal:' + s.namespace + '/' + s.name
            return (
              <tr key={s.namespace + '/' + s.name}>
                <td className="k8s-name"><b>{s.name}</b><small>{s.namespace}</small></td><td className="m">{s.type}</td>
                <td className="m">{choosing === s.name
                  ? <span className="k8s-keys">{s.keys.map(key => <button key={key} onClick={() => void reveal(s, key)}>{key}</button>)}<button className="ghost" onClick={() => setChoosing('')}>{t('Cancel')}</button></span>
                  : used(s.keys)}</td>
                <td className="m">{used(s.used_by)}</td><td className="n"><Age sec={s.age_sec} /></td>
                <td><div className="sp-acts"><ActBtn id={id} right="secrets" disabled={s.keys.length === 0} label={t('Reveal a key of {name}', {name: s.name})}
                  onClick={() => (s.keys.length === 1 ? void reveal(s, s.keys[0]) : setChoosing(s.name))}>{t('Reveal')}</ActBtn></div></td>
              </tr>
            )
          })}</tbody>
        </table>{data && data.secrets.length === 0 && <div className="sp-empty">{t('No Secrets.')}</div>}{data && data.secrets.length > 0 && secrets.length === 0 && <div className="sp-empty">{t('No Secret matches.')}</div>}</div>
        {shown && (
          <div className="k8s-reveal" role="status" aria-label={t('Revealed secret')}>
            <div><b>{shown.secret}</b> · <span className="m">{shown.key}</span></div>
            <pre className="sp-pre">{shown.value}</pre>
            <div className="sp-acts"><span className="sp-note">{t('Hidden after 30 seconds.')}</span><button onClick={hide}>{t('Hide')}</button></div>
          </div>
        )}
        <div className="sp-note">{t('Values are never loaded with the list. Reveal asks first, shows one key for 30 seconds, is written to the audit log and is locked when the login may not get secrets.')}</div>
      </Card>
      <Card title={t('Volumes')} collapsible closed={closedV} onToggle={() => setClosedV(!closedV)} count={counted(volumes.length, data?.volumes.length ?? 0, fv)} filter={{value: fv, onChange: setFv, label: t('Search Volumes')}}>
        <div className="sp-table"><table>
          <thead><tr><th>{t('Claim')}</th><th>{t('Status')}</th><th>{t('Size and use')}</th><th>{t('Storage class')}</th><th>{t('Used by')}</th></tr></thead>
          <tbody>{volumes.map(v => (
            <tr key={v.namespace + '/' + v.name}>
              <td className="k8s-name"><b>{v.name}</b><small>{v.namespace}</small></td>
              <td><Pill tone={v.status === 'Bound' ? 'ok' : /Lost|Failed/.test(v.status) ? 'bad' : 'warn'}>{v.status}</Pill></td>
              <td>{v.used_pct !== null
                ? <div className="k8s-use"><div className={'sp-track ' + level(v.used_pct)} role="meter" aria-label={t('Use of {name}', {name: v.name})} aria-valuemin={0} aria-valuemax={100} aria-valuenow={v.used_pct}><i style={{width: v.used_pct + '%'}} /></div><span>{v.used || v.size}</span></div>
                : <span className="sp-note">{v.used_by.length === 0 ? t('not used by any pod') : v.size}</span>}</td>
              <td className="m">{v.storage_class || '–'}</td>
              <td className={v.used_by.length ? 'm' : 'sp-note'}>{v.used_by.length ? v.used_by.join(', ') : t('nobody')}</td>
            </tr>))}</tbody>
        </table>{data && data.volumes.length === 0 && <div className="sp-empty">{t('No volumes.')}</div>}{data && data.volumes.length > 0 && volumes.length === 0 && <div className="sp-empty">{t('No volume matches.')}</div>}</div>
        <div className="sp-note">{t('Use comes from the kubelet metrics, so it is shown only when the cluster reports it.')}</div>
      </Card>
      {viewing && <ConfigMapDialog item={viewing} onClose={() => setViewing(null)} />}
    </div>
  )
}
