import {useCallback, useEffect, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {DEFAULT_SETTINGS} from './defaults'
import {useAppearance} from './appearance'
import {SettingsWindow} from './SettingsWindow'
import type {Settings, VaultStatus} from '../types'

/** What the window shows until the real status arrives (nothing can be changed then). */
const PLACEHOLDER_VAULT: VaultStatus = {exists: true, unlocked: true, keychain_enabled: false, auto_lock_minutes: 15}

const CACHE_KEY = 'vutty.settingsWindow'

/** What this window last read, so the next one opens with real values instead of waiting for the main process. */
function readCache(): {settings: Settings; vault: VaultStatus} | null {
  try {
    const c = JSON.parse(localStorage.getItem(CACHE_KEY) ?? 'null')
    return c?.settings && c?.vault ? c : null
  } catch { return null }
}

/** A window focused again re-reads the data, but not more often than this (every read goes to the main process). */
const REFRESH_EVERY_MS = 3000

/**
 * Root of the Settings window process. It shows the same settings as the in-app window, but as the whole
 * content of its own native window. Data comes from the main window's process (see bridgeBackend), so the
 * values are re-read whenever this window is focused again.
 */
export default function SettingsApp() {
  const be = useBackend()
  const [cached] = useState(readCache)
  const [settings, setSettings] = useState<Settings>(cached?.settings ?? DEFAULT_SETTINGS)
  const [vault, setVault] = useState<VaultStatus | null>(cached?.vault ?? null)
  const [fresh, setFresh] = useState(false) // true once the main process has answered
  const appearance = useAppearance(settings.theme)

  const last = useRef(0)
  const refresh = useCallback(async () => {
    last.current = Date.now()
    try {
      const [s, v] = await Promise.all([be.settings.get(), be.vault.status()])
      setSettings(s)
      setVault(v)
      setFresh(true)
      try { localStorage.setItem(CACHE_KEY, JSON.stringify({settings: s, vault: v})) } catch { /* not remembered */ }
    } catch { /* the main window is gone: the process ends by itself */ }
  }, [be])

  useEffect(() => {
    void refresh()
    const onFocus = () => { if (Date.now() - last.current >= REFRESH_EVERY_MS) void refresh() }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [refresh])

  useEffect(() => {
    document.documentElement.dataset.theme = appearance
    try { localStorage.setItem('vutty.appearance', appearance) } catch { /* not remembered */ }
  }, [appearance])
  useEffect(() => { document.documentElement.style.setProperty('zoom', String((settings.zoom || 100) / 100)) }, [settings.zoom])
  useEffect(() => { document.documentElement.dataset.motion = settings.reduce_motion ? 'reduced' : 'full' }, [settings.reduce_motion])
  useEffect(() => { document.title = 'Settings' }, [])

  // the window is up at once with the values it showed last time; nothing can be saved until the real ones arrive (see SettingsWindow stale)
  return (
    <SettingsWindow native ready={!!vault} stale={!fresh} settings={settings} appearance={appearance} vault={vault ?? PLACEHOLDER_VAULT} onSettings={setSettings}
      onVault={() => void refresh()} onChanged={() => void refresh()} onClose={() => window.runtime?.Quit?.()} />
  )
}
