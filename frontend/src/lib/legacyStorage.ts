// The app was called VVTerm and then VuTerm before it became Vutty: its saved view settings, history and so on were
// stored under "vvterm." / "vuterm." (and ":") keys. Copy them to the new names once so nobody starts from scratch.
const OLD = ['vvterm.', 'vvterm:', 'vuterm.', 'vuterm:']

export function migrateLegacyStorage(store: Storage = localStorage): void {
  try {
    const keys = Array.from({length: store.length}, (_, i) => store.key(i) ?? '')
    for (const key of keys) {
      const prefix = OLD.find(p => key.startsWith(p))
      if (!prefix) continue
      const next = 'vutty' + key.slice('vuterm'.length)
      if (store.getItem(next) === null) store.setItem(next, store.getItem(key) ?? '')
      store.removeItem(key)
    }
  } catch { /* storage unavailable: nothing to carry over */ }
}
