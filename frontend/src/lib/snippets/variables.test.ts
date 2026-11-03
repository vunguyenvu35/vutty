import {fillVariables, parseVariables} from './variables'

describe('snippet variables', () => {
  it('finds each blank once, in order, with its default', () => {
    expect(parseVariables('scp {{file}} {{host}}:{{dir|/tmp}}\nls {{dir}} {{ file }}')).toEqual([
      {name: 'file', def: ''}, {name: 'host', def: ''}, {name: 'dir', def: '/tmp'},
    ])
  })

  it('leaves the per-host names out when a run fills them in itself', () => {
    expect(parseVariables('ssh {{user}}@{{hostname}} {{cmd}} {{host}}', true).map(v => v.name)).toEqual(['cmd'])
  })

  it.each([
    'docker ps --format "{{.Names}}"', 'helm template {{ .Values.name }}', 'kubectl get -o go-template={{json .}}', 'echo {{}} {{1st}} {{a b}}', 'no blanks at all',
  ])('does not take %j for a blank', script => expect(parseVariables(script)).toEqual([]))

  it('fills the blanks, uses the default for an empty answer, and keeps what it was told to keep', () => {
    const s = 'cp {{src}} {{dest|/backup}} # {{host}}'
    expect(fillVariables(s, {src: 'a.txt', dest: ''})).toBe('cp a.txt /backup # ')
    expect(fillVariables(s, {src: 'a.txt', dest: '/x', host: 'h'})).toBe('cp a.txt /x # h')
    expect(fillVariables(s, {src: 'a.txt'}, ['host'])).toBe('cp a.txt /backup # {{host}}') // left for the run on each host
  })

  it('puts the answer in as it is: no second round of blanks, no dollar-sign tricks', () => {
    expect(fillVariables('echo {{a}}', {a: '{{b}} $& $1'})).toBe('echo {{b}} $& $1')
  })

  it('works on a script with the same blank many times and with many lines', () => {
    expect(fillVariables('a {{x}}\nb {{x}}\n', {x: '1'})).toBe('a 1\nb 1\n')
  })
})
