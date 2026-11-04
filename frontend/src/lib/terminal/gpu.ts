import type {Terminal} from '@xterm/xterm'
import {WebglAddon} from '@xterm/addon-webgl'

export interface Gpu {
  dispose(): void
}

/**
 * Draws a terminal with WebGL, which keeps 60 frames per second on a screen redrawn with every cell coloured
 * (htop, mc, vim) where the DOM renderer drops to about 20. Returns null when WebGL is not available or the
 * context cannot be made: the terminal then stays on the DOM renderer, which draws the same thing.
 *
 * A browser allows about 16 WebGL contexts per page, so only the pane that has the focus should hold one.
 * When the system takes the context away (GPU reset, sleep), the addon is dropped and the DOM renderer returns.
 */
export function attachGpu(term: Terminal, onLost?: () => void): Gpu | null {
  let addon: WebglAddon | null = null
  try {
    addon = new WebglAddon()
    addon.onContextLoss(() => {
      try { addon?.dispose() } catch { /* already gone */ }
      addon = null
      onLost?.()
    })
    term.loadAddon(addon)
  } catch {
    try { addon?.dispose() } catch { /* never loaded */ }
    return null
  }
  return {
    dispose() {
      const a = addon
      addon = null
      try { a?.dispose() } catch { /* terminal already disposed */ }
    },
  }
}
