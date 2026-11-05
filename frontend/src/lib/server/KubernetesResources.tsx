import {Fragment, useCallback, useMemo} from 'react'
import {useT} from '../i18n'
import {Select} from '../ui/Select'
import {Err, Loading, Toolbar, FilterInput} from './shared'
import {age, matches, useK8s, useKept, useKLoad, useSort} from './KubernetesShared'
import type {K8sAPIResource, K8sObjList} from '../types'

/** kinds that are asked for often (the Rancher sidebar has them): one button each, when the cluster serves them */
const COMMON = ['namespaces', 'horizontalpodautoscalers', 'persistentvolumes', 'storageclasses']
const idOf = (r: {group: string; version: string; plural: string}) => `${r.group}|${r.version}|${r.plural}`

/** the objects of one kind: a table whose columns are those the kind asks for (a CRD's printer columns), then the age */
function Objects({res}: {res: K8sAPIResource}) {
  const k = useK8s()
  const t = useT()
  const [filter, setFilter] = useKept('resources.filter', '')
  const ref = useMemo(() => ({group: res.group, version: res.version, plural: res.plural}), [res])
  const load = useCallback(() => k.be.server.k8sObjects(k.sid, k.ctx, k.ns, ref), [k.be, k.sid, k.ctx, k.ns, ref])
  const {data, error, busy, reload} = useKLoad<K8sObjList>(load, false)
  const {th, apply} = useSort<string>(['name', ...(data?.columns ?? []).map((_, i) => 'c' + i)])
  const rows = apply((data?.rows ?? []).filter(r => matches(filter, r.name, r.namespace, ...r.cells)), (r, key) =>
    key === 'name' ? r.name : key === 'age' ? r.age_sec : key === 'ns' ? r.namespace : r.cells[Number(key.slice(1))] ?? '')
  const open = (r: {name: string; namespace: string}) => k.open({k: 'yaml', kind: res.kind, ns: r.namespace, name: r.name, label: `${res.kind} ${r.name}`, back: null, ref})
  const showNs = !!data?.namespaced && (k.ns === '' || k.ns.includes(','))
  return (
    <>
      <Toolbar busy={busy} onReload={reload}>
        <FilterInput label={t('Filter objects')} placeholder={t('Filter objects')} value={filter} onChange={setFilter} />
        <span className="sp-note">{data ? t('{n} objects', {n: data.rows.length}) : ''}</span>
      </Toolbar>
      <Err error={error} />
      {!data && !error && <Loading kind="table" what={t('Loading objects…')} />}
      {data && (
        <div className="sp-table k8s-wrap"><table>
          <thead><tr>{th('name', t('Name'))}{showNs && th('ns', t('Namespace'))}{data.columns.map((c, i) => <Fragment key={i}>{th('c' + i, c)}</Fragment>)}{th('age', t('Age'), 'n')}<th /></tr></thead>
          <tbody>{rows.map(r => (
            <tr key={r.namespace + '/' + r.name}>
              <td className="k8s-name"><b>{r.name}</b></td>
              {showNs && <td>{r.namespace}</td>}
              {r.cells.map((c, i) => <td key={i}>{c}</td>)}
              <td className="n">{age(r.age_sec)}</td>
              <td className="sp-acts"><button aria-label={t('YAML of {name}', {name: r.name})} onClick={() => open(r)}>{t('YAML')}</button></td>
            </tr>))}</tbody>
        </table>{rows.length === 0 && <div className="sp-empty">{t('No objects.')}</div>}</div>
      )}
      {data?.more && <div className="sp-note k8s-hint">{t('The cluster has more objects than are shown: choose a namespace to narrow the list.')}</div>}
    </>
  )
}

/** Resources: any kind the cluster serves, CRDs included, read only. Pick a kind, read its objects, open one as YAML. */
export function KubernetesResources() {
  const k = useK8s()
  const t = useT()
  const [pick, setPick] = useKept('resources.pick', '')
  const load = useCallback(() => k.be.server.k8sResources(k.sid, k.ctx), [k.be, k.sid, k.ctx])
  const {data, error, busy, reload} = useKLoad<K8sAPIResource[]>(load, false)
  const chosen = (data ?? []).find(r => idOf(r) === pick)
  if (!data && !error) return <div className="sp-view"><Loading kind="table" what={t('Loading resources…')} /></div>
  return (
    <div className="sp-view">
      <div className="sp-bar">
        <Select className="sp-pick" label={t('Kind')} value={pick} searchable placeholder={t('Choose a kind')} onChange={setPick}
          options={(data ?? []).map(r => ({value: idOf(r), label: r.kind, hint: r.group ? `${r.plural}.${r.group}` : r.plural}))} />
        {COMMON.map(p => (data ?? []).filter(r => r.plural === p && (r.group === '' || r.group === 'autoscaling' || r.group === 'storage.k8s.io')).map(r => <button key={p} aria-pressed={idOf(r) === pick} className={idOf(r) === pick ? 'primary' : ''} onClick={() => setPick(idOf(r))}>{r.kind}</button>))}
        <span className="sp-note">{t('{n} kinds · read only', {n: data?.length ?? 0})}</span>
        {!chosen && <button onClick={reload} disabled={busy}>{t('Refresh')}</button>}
      </div>
      <Err error={error} />
      {chosen
        ? <Objects key={idOf(chosen) + '|' + k.ns} res={chosen} />
        : data && <div className="sp-empty">{t('Choose a kind to see its objects: every resource the cluster serves is here, custom resources (CRDs) included.')}</div>}
    </div>
  )
}
