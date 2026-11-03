import {Terminal} from '../../frontend/node_modules/@xterm/xterm'
import {FitAddon} from '../../frontend/node_modules/@xterm/addon-fit'
import {WebglAddon} from '@xterm/addon-webgl'
import {createHighlighter, HighlightTerm} from '../../frontend/src/lib/terminal/highlight'
import {b64ToBytes} from '../../frontend/src/lib/util'

const w = window as any
let t: Terminal
let hl: ReturnType<typeof createHighlighter> | undefined
let lags: number[] = [], gaps: number[] = []
let timer = 0, raf = 0, running = false, t0 = 0, fed = 0, chunks = 0

w.setup = (o: {renderer: string; highlight: boolean}) => {
  document.getElementById('term')!.innerHTML = ''
  if (t) t.dispose()
  t = new Terminal({fontSize: 13, fontFamily: "Menlo, Monaco, 'Courier New', monospace", cursorBlink: true, cursorStyle: 'block',
    cursorInactiveStyle: 'bar', cursorWidth: 2, scrollback: 10000, allowProposedApi: true})
  const f = new FitAddon(); t.loadAddon(f)
  t.open(document.getElementById('term')!)
  if (o.renderer === 'webgl') t.loadAddon(new WebglAddon())
  f.fit()
  let hlTimer = 0
  hl = createHighlighter(t as unknown as HighlightTerm)
  t.onWriteParsed(() => { clearTimeout(hlTimer); hlTimer = window.setTimeout(() => hl!.scan(), 60) })
  if (!o.highlight) hl.dispose()
  return t.cols + 'x' + t.rows
}
w.start = () => {
  lags = []; gaps = []; fed = 0; chunks = 0; running = true; t0 = performance.now()
  let last = performance.now()
  const tick = () => { if (!running) return; const n = performance.now(); lags.push(n - last - 4); last = n; timer = window.setTimeout(tick, 4) }
  timer = window.setTimeout(tick, 4)
  let lr = performance.now()
  const fr = () => { if (!running) return; const n = performance.now(); gaps.push(n - lr); lr = n; raf = requestAnimationFrame(fr) }
  raf = requestAnimationFrame(fr)
}
// exactly what TerminalPane does with a term:data event
w.feed = (b64: string) => { fed += b64.length * 3 / 4; chunks++; t.write(b64ToBytes(b64)) }
const pct = (a: number[], p: number) => { if (!a.length) return 0; const s = [...a].sort((x, y) => x - y); return s[Math.min(s.length - 1, Math.floor(s.length * p))] }
w.finish = () => new Promise(res => t.write('', () => {
  const tFeedDone = performance.now() - t0
  // let the renderer catch up before measuring the end
  requestAnimationFrame(() => requestAnimationFrame(() => {
    running = false; clearTimeout(timer); cancelAnimationFrame(raf)
    const total = performance.now() - t0
    const sum = (a: number[]) => a.reduce((x, y) => x + y, 0)
    res(JSON.stringify({
      mb: +(fed / 1048576).toFixed(1), chunks, parseSec: +(tFeedDone / 1000).toFixed(2), totalSec: +(total / 1000).toFixed(2),
      mbps: +(fed / 1048576 / (tFeedDone / 1000)).toFixed(1),
      lagP50: +pct(lags, .5).toFixed(0), lagP99: +pct(lags, .99).toFixed(0), lagMax: +Math.max(0, ...lags).toFixed(0),
      frames: gaps.length, fps: +(gaps.length / (sum(gaps) / 1000)).toFixed(1), gapP99: +pct(gaps, .99).toFixed(0), gapMax: +Math.max(0, ...gaps).toFixed(0),
      lines: t.buffer.active.length,
    }))
  }))
}))
w.ready = true
