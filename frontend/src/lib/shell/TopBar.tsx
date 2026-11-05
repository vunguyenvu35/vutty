import {useEffect, useState} from 'react'
import {IconClose, IconCloud, IconFolder, IconGear, IconKey, IconPlus, IconServer, IconSidebar, IconSplitH, IconSplitV} from '../ui/Icons'
import {draggingPane, isTerminalTab, PANE_DRAG, setDraggingPane, setDraggingTab, TAB_DRAG, type Tab} from '../layout'
import {TabStatus} from './TabStatus'
import {isMac} from '../shortcuts/shortcuts'
import {OS_INFO, OsIcon} from '../ui/OsIcon'
import {leaves} from '../layout'
import {SnippetPicker} from '../snippets/SnippetPicker'
import type {Host, Snippet} from '../types'
import {useT} from '../i18n'

interface Props {
  tabs: Tab[]
  active: string // 'home' or a tab id
  onHome(): void
  onSelect(id: string): void
  onClose(id: string): void
  onNew(): void
  onSplit(dir: 'row' | 'col'): void
  onSettings(): void
  /** a double click on an empty part of the title bar zooms the window, like any Mac app */
  onToggleMaximise?(): void
  /** a tab was given a new name (double-click its title) */
  onRename?(id: string, name: string): void
  /** right-click menu: a new tab on the same host, next to this one */
  onDuplicate?(id: string): void
  /** right-click menu: a new native window with a new session to the same place */
  onDuplicateWindow?(id: string): void
  /** right-click menu: split the tab's focused pane */
  onSplitTab?(id: string, dir: 'row' | 'col'): void
  /** the saved hosts: a tab shows the icon of the server type (Ubuntu, Debian…) it is connected to */
  hosts?: Host[]
  /** a tab was dragged to another place: before `targetId`, or after it */
  onMoveTab?(id: string, targetId: string, after: boolean): void
  /** a pane header was dropped on the tab bar: the pane becomes a tab of its own */
  onPaneOut?(tabId: string, paneId: string): void
  /** a pane header was dropped on another terminal tab: the pane joins that tab */
  onPaneToTab?(fromTabId: string, paneId: string, targetTabId: string): void
  snippets: Snippet[]
  onRunSnippet(s: Snippet, execute: boolean): void
  /** the panel on the right of a terminal (hosts, snippets, history, themes) */
  sidebarOpen?: boolean
  onToggleSidebar?(): void
  /** a newer release exists (version number): shows an Update chip in the corner */
  updateVersion?: string
  onUpdate?(): void
}

/** Title bar with the Vaults (home) tab, one tab per terminal, and split controls. */
export function TopBar(p: Props) {
  const t = useT()
  // a tab being dragged, and the tab it is over (with the side the drop would land on)
  const [dragId, setDragId] = useState('')
  const [over, setOver] = useState<{id: string; after: boolean} | null>(null)
  // a pane header dragged over the bar (to become a tab) or over a terminal tab (to join it)
  const [paneOver, setPaneOver] = useState(false)
  const [paneTab, setPaneTab] = useState('')
  const paneDragging = () => (p.onPaneOut ? draggingPane() : null)
  const hasPane = (e: React.DragEvent) => Array.from(e.dataTransfer.types).includes(PANE_DRAG)
  const endPaneDrag = () => { setPaneOver(false); setPaneTab(''); setDraggingPane(null) }
  const [editing, setEditing] = useState<{id: string; value: string} | null>(null)
  const [menu, setMenu] = useState<{id: string; x: number; y: number} | null>(null)
  // the menu goes away on any click, Esc, or when the window loses focus
  useEffect(() => {
    if (!menu) return
    const off = () => setMenu(null)
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); off() } }
    window.addEventListener('mousedown', off)
    window.addEventListener('keydown', esc, true)
    window.addEventListener('blur', off)
    return () => { window.removeEventListener('mousedown', off); window.removeEventListener('keydown', esc, true); window.removeEventListener('blur', off) }
  }, [menu])
  const menuTab = menu ? p.tabs.find(tb => tb.id === menu.id) : undefined
  const commit = () => {
    if (editing) p.onRename?.(editing.id, editing.value)
    setEditing(null)
  }
  const current = p.tabs.find(tb => tb.id === p.active)
  /** the name a tab shows: Settings is translated now, not when it was opened */
  const tabName = (tb: Tab) => (tb.kind === 'settings' ? t('Settings') : tb.title)
  /** the icon of a tab: the server type of its host, a plug for a serial port, a terminal for this computer */
  const tabIcon = (tb: Tab) => {
    if (tb.kind === 'sftp') return <IconFolder size={15} />
    if (tb.kind === 'new') return <IconPlus size={15} />
    if (tb.kind === 'settings') return <IconGear size={15} />
    if (tb.kind === 'server') return <IconServer size={15} />
    if (tb.kind === 'cluster') return <IconCloud size={15} />
    const hostId = leaves(tb.root)[0]?.hostId ?? ''
    const id = hostId.startsWith('telnet:') ? hostId.slice('telnet:'.length) : hostId
    const os = p.hosts?.find(h => h.id === id)?.os
    if (os && OS_INFO[os]) return <span className="tab-os" style={{background: OS_INFO[os].color}} data-os={os}><OsIcon os={os} size={12} /></span>
    return <IconTerminal />
  }
  const inTerminal = !!current && isTerminalTab(current)
  return (
    <div className={'topbar' + (paneOver ? ' pane-drop' : '')} role="tablist" aria-label={t('Open tabs')} onDoubleClick={e => {
      if (!(e.target as HTMLElement).closest('.tab, button, .top-tools, .snippet-picker, [role="menu"]')) p.onToggleMaximise?.()
    }}
      onDragOver={e => { if (paneDragging() && hasPane(e)) { e.preventDefault(); setPaneOver(true) } }}
      onDragLeave={e => { if (!e.currentTarget.contains(e.relatedTarget as globalThis.Node | null)) { setPaneOver(false); setPaneTab('') } }}
      onDrop={e => {
        const dp = paneDragging()
        if (!dp || !hasPane(e)) return
        e.preventDefault()
        endPaneDrag()
        p.onPaneOut?.(dp.tabId, dp.paneId)
      }}>
      {isMac() && <div className="traffic-space" />} {/* room for the red, yellow and green buttons of macOS: Windows and Linux have a frame of their own */}
      <button role="tab" aria-selected={p.active === 'home'} className={'tab tab-home' + (p.active === 'home' ? ' active' : '')} onClick={p.onHome}>
        <IconKey size={15} /> <span>{t('Vaults')}</span>
      </button>
      {p.tabs.map(tb => (
        <div key={tb.id} role="tab" aria-selected={tb.id === p.active} className={'tab' + (tb.pinned ? ' pinned' : '') + (tb.id === p.active ? ' active' : '') + (dragId === tb.id ? ' dragging' : '') + (paneTab === tb.id ? ' drop-into' : '') + (over?.id === tb.id && dragId !== tb.id ? (over.after ? ' drop-after' : ' drop-before') : '')} onClick={() => p.onSelect(tb.id)}
          draggable={!tb.pinned && !!p.onMoveTab && editing?.id !== tb.id}
          onDragStart={e => { setDragId(tb.id); setDraggingTab(tb.id); e.dataTransfer.setData(TAB_DRAG, tb.id); e.dataTransfer.effectAllowed = 'move' }}
          onDragEnd={() => { setDragId(''); setDraggingTab(''); setOver(null) }}
          onDragOver={e => {
            const dp = paneDragging()
            if (dp && hasPane(e) && p.onPaneToTab && isTerminalTab(tb) && dp.tabId !== tb.id) { e.preventDefault(); e.stopPropagation(); setPaneOver(false); setPaneTab(tb.id); return }
            if (!dragId || dragId === tb.id || !Array.from(e.dataTransfer.types).includes(TAB_DRAG)) return
            e.preventDefault()
            const r = e.currentTarget.getBoundingClientRect()
            setOver({id: tb.id, after: e.clientX > r.left + r.width / 2})
          }}
          onDragLeave={() => setOver(o => (o?.id === tb.id ? null : o))}
          onDrop={e => {
            const dp = paneDragging()
            if (dp && hasPane(e) && p.onPaneToTab && isTerminalTab(tb) && dp.tabId !== tb.id) { e.preventDefault(); e.stopPropagation(); endPaneDrag(); p.onPaneToTab(dp.tabId, dp.paneId, tb.id); return }
            const id = e.dataTransfer.getData(TAB_DRAG)
            if (!id || id === tb.id) return
            e.preventDefault()
            const after = e.clientX > e.currentTarget.getBoundingClientRect().left + e.currentTarget.getBoundingClientRect().width / 2
            setDragId('')
            setOver(null)
            p.onMoveTab?.(id, tb.id, after)
          }}
          onContextMenu={e => { e.preventDefault(); if (!tb.pinned) setMenu({id: tb.id, x: e.clientX, y: e.clientY}) }}>
          <span className="tab-ico">{tabIcon(tb)}<TabStatus tab={tb} /></span>
          {editing?.id === tb.id ? (
            <input className="tab-rename" aria-label={t('Tab name')} autoFocus value={editing.value} maxLength={60}
              onClick={e => e.stopPropagation()} onDoubleClick={e => e.stopPropagation()} onChange={e => setEditing({id: tb.id, value: e.target.value})}
              onFocus={e => e.currentTarget.select()} onBlur={commit}
              onKeyDown={e => { e.stopPropagation(); if (e.key === 'Enter') commit(); else if (e.key === 'Escape') setEditing(null) }} />
          ) : (
            <span className="tab-title" title={tb.pinned || tb.kind === 'settings' ? tabName(tb) : t('{title} (double-click to rename)', {title: tb.title})} onDoubleClick={() => { if (!tb.pinned && tb.kind !== 'settings' && p.onRename) setEditing({id: tb.id, value: tb.title}) }}>{tabName(tb)}</span>
          )}
          {!tb.pinned && (
            <span className="x" role="button" aria-label={t('Close {title}', {title: tabName(tb)})} onClick={e => { e.stopPropagation(); p.onClose(tb.id) }}>
              <IconClose size={13} />
            </span>
          )}
        </div>
      ))}
      {menu && menuTab && (
        <div className="ctx-menu" role="menu" aria-label={t('Tab {title}', {title: menuTab.title})} style={{left: menu.x, top: menu.y}} onMouseDown={e => e.stopPropagation()} onContextMenu={e => e.preventDefault()}>
          <button role="menuitem" disabled={!isTerminalTab(menuTab) || !p.onDuplicate} onClick={() => { setMenu(null); p.onDuplicate?.(menuTab.id) }}>{t('Duplicate')}</button>
          <button role="menuitem" disabled={!isTerminalTab(menuTab) || !p.onDuplicateWindow} onClick={() => { setMenu(null); p.onDuplicateWindow?.(menuTab.id) }}>{t('Duplicate in new window')}</button>
          <button role="menuitem" disabled={!p.onRename} onClick={() => { setMenu(null); setEditing({id: menuTab.id, value: menuTab.title}) }}>{t('Rename')}</button>
          <button role="menuitem" disabled={!isTerminalTab(menuTab) || !p.onSplitTab} onClick={() => { setMenu(null); p.onSplitTab?.(menuTab.id, 'row') }}>{t('Split right')}</button>
          <button role="menuitem" disabled={!isTerminalTab(menuTab) || !p.onSplitTab} onClick={() => { setMenu(null); p.onSplitTab?.(menuTab.id, 'col') }}>{t('Split down')}</button>
          <div className="ctx-sep" role="separator" />
          <button role="menuitem" onClick={() => { setMenu(null); p.onClose(menuTab.id) }}>{t('Close')}</button>
        </div>
      )}
      <button className="tab add" aria-label={t('New connection')} title={t('New connection')} onClick={p.onNew}><IconPlus size={16} /></button>
      <div className="spacer" />
      <div className="top-tools">
        {p.updateVersion && <button className="update-chip" title={t('Version {version} is available', {version: p.updateVersion})} aria-label={t('Update available: version {version}', {version: p.updateVersion})} onClick={p.onUpdate}>{t('Update')}</button>}
        <SnippetPicker snippets={p.snippets} disabled={!inTerminal} onRun={p.onRunSnippet} />
        <button className="icon" title={t('Settings (⌘/Ctrl + ,)')} aria-label={t('Open settings')} onClick={p.onSettings}><IconGear /></button>
        <button className="icon" disabled={!inTerminal} title={t('Split right')} aria-label={t('Split right')} onClick={() => p.onSplit('row')}><IconSplitH /></button>
        <button className="icon" disabled={!inTerminal} title={t('Split down')} aria-label={t('Split down')} onClick={() => p.onSplit('col')}><IconSplitV /></button>
        {p.onToggleSidebar && <button className={'icon' + (p.sidebarOpen ? ' on' : '')} disabled={!inTerminal} title={p.sidebarOpen ? t('Hide sidebar') : t('Show sidebar')} aria-label={t('Toggle sidebar')} aria-pressed={!!p.sidebarOpen} onClick={p.onToggleSidebar}><IconSidebar /></button>}
      </div>
    </div>
  )
}

function IconTerminal() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="4" width="18" height="16" rx="2" /><path d="m7 9 3 3-3 3M13 15h4" />
    </svg>
  )
}
