/**
 * The colour codes that programs put in their output (NestJS, npm, docker, grep --color …): ESC [ 32 m is "green", ESC [ 0 m is "back to
 * normal". A log shown as plain text would draw the ESC as a box and leave "[32m" in the line; here they become colours, and every other
 * escape sequence (moving the cursor, clearing a line, a window title) is dropped.
 */

export interface AnsiStyle {color?: string; background?: string; fontWeight?: number; fontStyle?: 'italic'; textDecoration?: 'underline'; opacity?: number}
export interface AnsiSpan {text: string; style?: AnsiStyle}

// the 16 colours: dark enough to read on a light page, light enough on a dark one
const BASE = ['#6b7280', '#e06c75', '#5fbf80', '#d9a441', '#5aa0e6', '#b877db', '#4fb3bf', '#c9ccd2']
const BRIGHT = ['#8a919c', '#ff8088', '#7ddc9c', '#f0c060', '#7cb8f5', '#d08cf0', '#6fd0dc', '#eef0f4']
const PALETTE = [...BASE, ...BRIGHT]

// ESC [ params final (CSI), ESC ] … BEL or ESC \ (a title, a link), and the escapes of one character (save and restore the cursor, a keypad mode …)
// eslint-disable-next-line no-control-regex
const ESC = /\u001b(?:\[([0-9;:?]*)([@-~])|\][^\u0007\u001b]*(?:\u0007|\u001b\\)|[0-9=>@-Z\\-_])/g

/** the text without any escape sequence */
export const stripAnsi = (s: string): string => (s.includes('\u001b') ? s.replace(ESC, '') : s)

const hex = (n: number) => n.toString(16).padStart(2, '0')

/** a colour of the 256 table: the 16 above, a 6×6×6 cube, then 24 greys */
function colour256(n: number): string | undefined {
  if (n < 0 || n > 255) return undefined
  if (n < 16) return PALETTE[n]
  if (n >= 232) { const v = 8 + (n - 232) * 10; return `#${hex(v)}${hex(v)}${hex(v)}` }
  const c = n - 16
  const step = (x: number) => (x === 0 ? 0 : 55 + x * 40)
  return `#${hex(step(Math.floor(c / 36)))}${hex(step(Math.floor(c / 6) % 6))}${hex(step(c % 6))}`
}

/** the line cut into pieces of one style each; a line without escapes is one piece */
export function ansiSpans(line: string): AnsiSpan[] {
  if (!line.includes('\u001b')) return [{text: line}]
  const out: AnsiSpan[] = []
  let st: AnsiStyle = {}
  let at = 0
  const push = (text: string) => { if (text) out.push(Object.keys(st).length ? {text, style: {...st}} : {text}) }
  for (const m of line.matchAll(ESC)) {
    push(line.slice(at, m.index))
    at = m.index! + m[0].length
    if (m[2] !== 'm') continue // anything but a colour is dropped
    const p = (m[1] === '' ? '0' : m[1]).split(/[;:]/).map(x => (x === '' ? 0 : Number(x)))
    for (let i = 0; i < p.length; i++) {
      const c = p[i]
      if (c === 0) st = {}
      else if (c === 1) st.fontWeight = 700
      else if (c === 2) st.opacity = 0.6
      else if (c === 3) st.fontStyle = 'italic'
      else if (c === 4) st.textDecoration = 'underline'
      else if (c === 22) { delete st.fontWeight; delete st.opacity }
      else if (c === 23) delete st.fontStyle
      else if (c === 24) delete st.textDecoration
      else if (c >= 30 && c <= 37) st.color = BASE[c - 30]
      else if (c >= 90 && c <= 97) st.color = BRIGHT[c - 90]
      else if (c >= 40 && c <= 47) st.background = BASE[c - 40]
      else if (c >= 100 && c <= 107) st.background = BRIGHT[c - 100]
      else if (c === 39) delete st.color
      else if (c === 49) delete st.background
      else if (c === 38 || c === 48) {
        const key = c === 38 ? 'color' : 'background'
        if (p[i + 1] === 5) { const v = colour256(p[i + 2]); if (v) st[key] = v; i += 2 }
        else if (p[i + 1] === 2) { st[key] = `rgb(${p[i + 2] & 255}, ${p[i + 3] & 255}, ${p[i + 4] & 255})`; i += 4 }
      }
    }
  }
  push(line.slice(at))
  return out
}
