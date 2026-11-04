import {useSyncExternalStore} from 'react'
import {paneStatusVersion, summarizeStatus, watchPaneStatus} from '../terminal/panes'
import {useT} from '../i18n'
import {leaves, type Tab} from '../layout'

/** A small mark in a tab: a spinner while its session connects, a green dot while it is connected, a red dot once it is closed or lost. */
export function TabStatus({tab}: {tab: Tab}) {
  const t = useT()
  useSyncExternalStore(watchPaneStatus, paneStatusVersion)
  if (tab.kind === 'sftp' || tab.kind === 'new' || tab.kind === 'settings' || tab.kind === 'server' || tab.choose) return null
  const s = summarizeStatus(leaves(tab.root).map(l => l.id))
  if (s === 'connecting') return <span className="tab-status connecting" role="img" aria-label={t('Connecting')} title={t('Connecting…')} />
  if (s === 'closed') return <span className="tab-status closed" role="img" aria-label={t('Disconnected')} title={t('Disconnected: press Enter in the terminal to reconnect')} />
  if (s === 'ready') return <span className="tab-status ready" role="img" aria-label={t('Connected')} title={t('Connected')} />
  return null
}
