import {useCallback, useState} from 'react'
import {useT} from '../i18n'
import {Err, Loading, Toolbar, FilterInput} from './shared'
import {Age, matches, Pill, useK8s, useKept, useKLoad, useSort} from './KubernetesShared'
import type {K8sEvent} from '../types'

type EventSort = 'age' | 'reason' | 'object'
const HOUR = 3600

/** Screen 17: what happened in the last hour, newest first; Warnings on, Normal off to start with */
export function Events() {
  const k = useK8s()
  const t = useT()
  const [warnings, setWarnings] = useKept('events.warnings', true)
  const [normal, setNormal] = useKept('events.normal', false)
  const [filter, setFilter] = useKept('events.filter', '')
  const load = useCallback(() => k.be.server.k8sEvents(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data, error, busy, reload} = useKLoad<K8sEvent[]>(load, false)
  const {th, apply} = useSort<EventSort>(['reason', 'object'])
  const [opened, setOpened] = useState<Set<number>>(new Set()) // the long messages that are shown whole
  const recent = [...(data ?? [])].filter(e => e.age_sec <= HOUR && ((warnings && e.type === 'Warning') || (normal && e.type === 'Normal')) && matches(filter, e.reason, e.object, e.message, e.namespace)).sort((a, b) => a.age_sec - b.age_sec)
  const rows = apply(recent, (e, key) => key === 'age' ? e.age_sec : key === 'reason' ? e.reason : e.object)
  if (!data && !error) return <div className="sp-view"><Loading kind="table" what={t('Loading events…')} /></div>
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>
        <FilterInput label={t('Filter events')} placeholder={t('Filter events')} value={filter} onChange={setFilter} />
        <button aria-pressed={warnings} className={warnings ? 'primary' : ''} onClick={() => setWarnings(!warnings)}>{t('Warnings')}</button>
        <button aria-pressed={normal} className={normal ? 'primary' : ''} onClick={() => setNormal(!normal)}>{t('Normal')}</button>
        <span className="sp-note">{t('Newest first, last hour')}</span>
      </Toolbar>
      <Err error={error} />
      <div className="sp-table k8s-wrap k8s-events"><table>
        <thead><tr>{th('reason', t('Reason'))}{th('object', t('Object'))}<th>{t('Message')}</th>{th('age', t('Last seen'), 'n')}</tr></thead>
        <tbody>{rows.map((e, i) => {
          const tone = e.type === 'Warning' ? (/BackOff|NotReady|Failed(?!Sched)|Evicted|OOM/.test(e.reason) ? 'bad' : 'warn') : 'ok'
          const [kind, ...rest] = e.object.split('/')
          const long = e.message.length > 140
          const open = opened.has(i)
          return (
            <tr key={i} className={'ev-' + tone}>
              <td><Pill tone={tone === 'ok' ? '' : tone}>{e.reason}</Pill></td>
              <td className="k8s-name">{rest.length > 0 ? <><span className="ev-kind">{kind}</span><b>{rest.join('/')}</b></> : <b>{e.object}</b>}<small>{e.namespace || t('cluster')}</small></td>
              <td className="k8s-msg">{long
                ? <button className={'ev-msg' + (open ? ' open' : '')} aria-expanded={open} title={open ? t('Show less') : t('Show all')} onClick={() => setOpened(o => { const n = new Set(o); if (open) n.delete(i); else n.add(i); return n })}>{e.message}</button>
                : e.message}</td>
              <td className="n ev-seen"><Age sec={e.age_sec} />{e.count > 1 && <small title={t('{n} times', {n: e.count})}>×{e.count}</small>}</td>
            </tr>)
        })}</tbody>
      </table>{data && rows.length === 0 && <div className="sp-empty">{t('No events.')}</div>}</div>
    </div>
  )
}
