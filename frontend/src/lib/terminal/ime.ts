/** Input methods (Vietnamese Telex, pinyin, kana…) in the terminal: composing text and replacement events. */

/**
 * What is typed or pasted goes to the shell composed (NFC). On macOS the input methods of a web view can give a letter in
 * pieces, such as the Vietnamese ệ as e + dot below + circumflex; zsh cannot edit a line with such pieces and shows them
 * as <0087>. Composed text is what every other terminal sends. Control sequences are plain ASCII and are not changed.
 */
export function composedInput(d: string): string {
  return /[\u0300-\u036f\u1ab0-\u1aff\u1dc0-\u1dff\u20d0-\u20ff]/.test(d) ? d.normalize('NFC') : d
}

/** The part of a terminal that followReplacements uses (xterm's Terminal has all of it). */
export interface ComposingTerm {
  textarea?: HTMLTextAreaElement
  onData(cb: (d: string) => void): {dispose(): void}
  input(data: string, wasUserInput?: boolean): void
}

/**
 * Some input methods (the Vietnamese ones of macOS in WKWebView, for one) do not compose in xterm's hidden text box:
 * every key replaces the whole word being typed there ("tieng" becomes "tiéng") with an `insertReplacementText` input
 * event. xterm.js ignores those events, so only the first letter of each word and the spaces reached the shell:
 * "gõ tiếng việt" arrived as "g t v". Here the word as the shell has it is tracked, and every replacement sends what
 * differs: backspaces for the letters that changed, then the new letters (what Unikey or OpenKey do too).
 * Returns what undoes it.
 */
export function followReplacements(t: ComposingTerm): () => void {
  const ta = t.textarea
  if (!ta) return () => {}
  let word = '' // the word typed so far, as the shell has it
  let ours = false // data that we send ourselves is not xterm's
  const sub = t.onData(d => {
    if (ours) return
    // letters extend the word; a space, Enter, Backspace, a control key or an escape sequence ends it
    word = /^[^\s\u0000-\u001f\u007f]+$/.test(d) ? word + d : ''
  })
  const onInput = (e: Event) => {
    const ev = e as InputEvent
    if (ev.inputType !== 'insertReplacementText' || !ev.data) return
    const want = Array.from(ev.data.normalize('NFC'))
    const have = Array.from(word)
    let same = 0
    while (same < want.length && same < have.length && want[same] === have[same]) same++
    const out = '\x7f'.repeat(have.length - same) + want.slice(same).join('')
    word = want.join('')
    if (!out) return
    ours = true
    try { t.input(out, true) } finally { ours = false }
  }
  ta.addEventListener('input', onInput)
  return () => { ta.removeEventListener('input', onInput); sub.dispose() }
}

/**
 * Text being composed with an input method (Vietnamese Telex, Chinese pinyin, Japanese…) is shown at the cursor, highlighted,
 * until it is committed, like in other terminals. xterm.js draws it in a small element; the colours (the theme's own, inverted)
 * are handed to the stylesheet through two custom properties.
 */
export function imeColours(el: HTMLElement | null, theme: {foreground?: string; background?: string}) {
  el?.style.setProperty('--ime-bg', theme.foreground || '#ffffff')
  el?.style.setProperty('--ime-fg', theme.background || '#000000')
}
