import {ansiSpans, stripAnsi} from './ansi'

const ESC = '\u001b'

describe('stripAnsi', () => {
  it('leaves a line without escapes as it is, and takes the colour codes out of one that has them', () => {
    expect(stripAnsi('plain line')).toBe('plain line')
    expect(stripAnsi(`${ESC}[32mNest${ESC}[0m - ${ESC}[2m04/11/2026${ESC}[0m  ${ESC}[33m[ChatAiService]${ESC}[0m : ${ESC}[32mINFO${ESC}[0m: ok`))
      .toBe('Nest - 04/11/2026  [ChatAiService] : INFO: ok')
  })

  it('drops the other escape sequences too: cursor moves, clearing a line, a window title, a link', () => {
    expect(stripAnsi(`a${ESC}[2Kb${ESC}[1;5Hc${ESC}[?25ld`)).toBe('abcd')
    expect(stripAnsi(`x${ESC}]0;my title\u0007y`)).toBe('xy')
    expect(stripAnsi(`${ESC}]8;;http://a${ESC}\\link${ESC}]8;;${ESC}\\`)).toBe('link')
    expect(stripAnsi(`k${ESC}7${ESC}8`)).toBe('k')
  })
})

describe('ansiSpans', () => {
  it('is one piece, with no style, for a line without escapes', () => {
    expect(ansiSpans('hello')).toEqual([{text: 'hello'}])
    expect(ansiSpans('')).toEqual([{text: ''}])
  })

  it('colours what is between a colour and the reset, and nothing else', () => {
    const s = ansiSpans(`${ESC}[32mNest${ESC}[0m - ok`)
    expect(s.map(x => x.text)).toEqual(['Nest', ' - ok'])
    expect(s[0].style?.color).toBeTruthy()
    expect(s[1].style).toBeUndefined()
  })

  it('keeps colour and weight together, and drops them one at a time', () => {
    const s = ansiSpans(`${ESC}[1;31mfail${ESC}[22m red${ESC}[39m plain`)
    expect(s.map(x => x.text)).toEqual(['fail', ' red', ' plain'])
    expect(s[0].style).toMatchObject({fontWeight: 700})
    expect(s[0].style?.color).toBe(s[1].style?.color)
    expect(s[1].style?.fontWeight).toBeUndefined()
    expect(s[2].style).toBeUndefined()
  })

  it('reads dim, italic, underline, backgrounds, bright colours, the 256 table and true colour', () => {
    expect(ansiSpans(`${ESC}[2mdim`)[0].style).toMatchObject({opacity: 0.6})
    expect(ansiSpans(`${ESC}[3;4mx`)[0].style).toMatchObject({fontStyle: 'italic', textDecoration: 'underline'})
    expect(ansiSpans(`${ESC}[41mx`)[0].style?.background).toBeTruthy()
    expect(ansiSpans(`${ESC}[91mx`)[0].style?.color).not.toBe(ansiSpans(`${ESC}[31mx`)[0].style?.color) // bright is not the same red
    expect(ansiSpans(`${ESC}[38;5;196mx`)[0].style?.color).toBe('#ff0000')
    expect(ansiSpans(`${ESC}[38;5;244mx`)[0].style?.color).toBe('#808080')
    expect(ansiSpans(`${ESC}[38;2;10;20;30mx`)[0].style?.color).toBe('rgb(10, 20, 30)')
    expect(ansiSpans(`${ESC}[48;5;21mx`)[0].style?.background).toBe('#0000ff')
  })

  it('takes "ESC[m" as a reset, and survives broken codes', () => {
    expect(ansiSpans(`${ESC}[31ma${ESC}[mb`).map(x => x.style === undefined)).toEqual([false, true])
    expect(ansiSpans(`${ESC}[38;5;999mx`)[0].text).toBe('x') // out of the table: no colour, no crash
    expect(ansiSpans(`${ESC}[38;2;1mx`)[0].text).toBe('x')
    expect(ansiSpans(`only ${ESC}[`).map(x => x.text).join('')).toContain('only')
  })

  it('never loses a character of the text', () => {
    const line = `${ESC}[32mNest${ESC}[0m${ESC}[2K - ${ESC}[1;33m[Svc]${ESC}[0m : ${ESC}[31mERROR${ESC}[0m boom`
    expect(ansiSpans(line).map(x => x.text).join('')).toBe(stripAnsi(line))
  })
})
