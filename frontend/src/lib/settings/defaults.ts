import type {Settings} from '../types'

/** Used until the saved settings arrive, and by tests. Mirrors the backend defaults. */
export const DEFAULT_SETTINGS: Settings = {
  theme: 'dark', terminal_theme: 'auto', font_size: 13, font_family: "Menlo, Monaco, 'Courier New', monospace",
  cursor_style: 'block', cursor_blink: true, scrollback: 10000, auto_lock_minutes: 15, sync_url: '',
  language: 'auto', zoom: 100, confirm_tab_close: false, confirm_prod: true, lock_on_sleep: true, session_log: 'off', session_log_dir: '', ask_protocol: false, biometric_on_open: true, suggest: true, suggest_host: false, highlight: true, gpu_renderer: true, copy_on_select: true, right_click: 'paste', reduce_motion: false, auto_update_check: true, account_name: '', account_email: '', shortcuts: {},
}
