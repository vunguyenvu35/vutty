import {useCallback, useState} from 'react'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {Modal} from '../ui/Modal'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Err, Loading, Toolbar, FilterInput} from './shared'
import {Age, Crumb, matches, Pill, useK8s, useKept, useKLoad, useSort, type K8sPage} from './KubernetesShared'
import type {K8sHelmDetail, K8sHelmRelease} from '../types'

type HelmPage = Extract<K8sPage, {k: 'helm'}>

const tone = (status: string) => (status === 'deployed' ? 'ok' : status === 'failed' ? 'bad' : status.startsWith('pending') || status === 'uninstalling' ? 'warn' : '')

/** Apps: the Helm releases the cluster keeps (read only). The list reads labels only; a release is read when it is opened. */
export function KubernetesHelm() {
  const k = useK8s()
  const t = useT()
  const [filter, setFilter] = useKept('apps.filter', '')
  const load = useCallback(() => k.be.server.k8sHelmReleases(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data, error, busy, reload} = useKLoad<K8sHelmRelease[]>(load, false)
  const {th, apply} = useSort<'name' | 'revision' | 'status' | 'age'>(['name', 'status'])
  const rows = apply((data ?? []).filter(r => matches(filter, r.name, r.namespace, r.status)), (r, key) => (key === 'name' ? r.name : key === 'status' ? r.status : key === 'revision' ? r.revision : r.age_sec))
  if (!data && !error) return <div className="sp-view"><Loading kind="table" what={t('Loading Helm releases…')} /></div>
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>
        <FilterInput label={t('Filter releases')} placeholder={t('Filter releases')} value={filter} onChange={setFilter} />
        <span className="sp-note">{data ? t('{n} releases · read only', {n: data.length}) : ''}</span>
      </Toolbar>
      <Err error={error} />
      <div className="sp-table k8s-wrap"><table>
        <thead><tr>{th('name', t('Release'))}{th('revision', t('Revision'), 'n')}{th('status', t('Status'))}{th('age', t('Updated'), 'n')}</tr></thead>
        <tbody>{rows.map(r => (
          <tr key={r.namespace + '/' + r.name}>
            <td className="k8s-name"><button className="sp-link" onClick={() => k.open({k: 'helm', ns: r.namespace, name: r.name, rev: 0})}><b>{r.name}</b></button><small>{r.namespace}</small></td>
            <td className="n m">{r.revision}</td>
            <td><Pill tone={tone(r.status)}>{r.status}</Pill></td>
            <td className="n"><Age sec={r.age_sec} /></td>
          </tr>))}</tbody>
      </table>{data && rows.length === 0 && <div className="sp-empty">{t('No Helm releases.')}</div>}</div>
      <div className="sp-note k8s-hint">{t('These are the releases Helm 3 stores in the cluster. Chart repositories are kept by Helm or Rancher, not by the cluster, so they are not listed here.')}</div>
    </div>
  )
}

/** One release at one revision: chart, status, manifest (Secrets without their data), notes, and the revisions kept */
export function HelmDetail({page}: {page: HelmPage}) {
  const k = useK8s()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [shown, setShown] = useState<{title: string; text: string} | null>(null)
  const load = useCallback(() => k.be.server.k8sHelmRelease(k.sid, k.ctx, page.ns, page.name, page.rev), [k.be, k.sid, k.ctx, page.ns, page.name, page.rev])
  const {data: d, error, busy, reload} = useKLoad<K8sHelmDetail>(load, false)
  const copy = async (text: string, done: string) => { try { await k.be.system.writeClipboard(text); toast.info(done) } catch (e) { toast.error(errMsg(e)) } }
  const values = async () => {
    if (!d) return
    if (!(await confirm(t('Show the values of the release {name} in {ns} on {ctx}? They can hold passwords. The audit log records that they were shown, never the values.', {name: page.name, ns: page.ns, ctx: k.ctx}), {confirmLabel: t('Show values'), danger: true}))) return
    try { setShown({title: t('Values of {name}', {name: page.name}), text: await k.be.server.k8sHelmValues(k.sid, k.ctx, page.ns, page.name, d.release.revision)}) } catch (e) { toast.error(errMsg(e)) }
  }
  const back = () => k.open(null)
  if (!d) return <div className="sp-view"><Toolbar busy={busy} onReload={reload}><Crumb path={[t('Back'), page.name]} onBack={back} /></Toolbar><Err error={error} />{!error && <Loading kind="detail" />}</div>
  const r = d.release
  const latest = d.history[0]?.revision
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}><Crumb path={[t('Back'), page.name, t('Revision {n}', {n: r.revision})]} onBack={back} /></Toolbar>
      <Err error={error} />
      <div className="sp-grid">
        <div className="sp-card"><h3>{t('Release')}</h3>
          <table className="sp-kv"><tbody>
            <tr><td>{t('Status')}</td><td><Pill tone={tone(r.status)}>{r.status}</Pill></td></tr>
            <tr><td>{t('Chart')}</td><td>{d.chart} {d.chart_version}</td></tr>
            <tr><td>{t('App version')}</td><td>{d.app_version || '–'}</td></tr>
            <tr><td>{t('Namespace')}</td><td>{r.namespace}</td></tr>
            <tr><td>{t('Updated')}</td><td><Age sec={r.age_sec} /></td></tr>
          </tbody></table>
          {d.description && <div className="sp-note">{d.description}</div>}
        </div>
      </div>
      <div className="sp-bar">
        <button onClick={() => setShown({title: t('Manifest of {name}', {name: page.name}), text: d.manifest})}>{t('Manifest')}</button>
        <button disabled={!d.has_values} title={d.has_values ? t('Asks first: values often hold passwords') : t('No values were given: the chart defaults are used')} onClick={() => void values()}>{t('Values')}</button>
        <button disabled={!d.notes} onClick={() => setShown({title: t('Notes of {name}', {name: page.name}), text: d.notes})}>{t('Notes')}</button>
      </div>
      <div className="sp-table k8s-wrap"><table>
        <caption className="sr-only">{t('History')}</caption>
        <thead><tr><th className="n">{t('Revision')}</th><th>{t('Status')}</th><th className="n">{t('Updated')}</th><th /></tr></thead>
        <tbody>{d.history.map(h => (
          <tr key={h.revision} aria-current={h.revision === r.revision || undefined}>
            <td className="n m">{h.revision}</td><td><Pill tone={tone(h.status)}>{h.status}</Pill></td><td className="n"><Age sec={h.age_sec} /></td>
            <td><div className="sp-acts">
              {h.revision !== r.revision && <button aria-label={t('View revision {n}', {n: h.revision})} onClick={() => k.open({k: 'helm', ns: page.ns, name: page.name, rev: h.revision})}>{t('View')}</button>}
              {h.revision !== latest && <button aria-label={t('Copy the rollback command to revision {n}', {n: h.revision})} title={t('Vutty does not change releases: run it in a terminal')} onClick={() => void copy(`helm rollback ${page.name} ${h.revision} -n ${page.ns}`, t('Command copied'))}>{t('Copy rollback')}</button>}
            </div></td>
          </tr>))}</tbody>
      </table></div>
      {shown && (
        <Modal wide title={shown.title} onClose={() => setShown(null)} footer={<><button onClick={() => void copy(shown.text, t('Copied'))}>{t('Copy')}</button><button onClick={() => setShown(null)}>{t('Close')}</button></>}>
          <pre className="sp-pre">{shown.text}</pre>
        </Modal>
      )}
    </div>
  )
}
