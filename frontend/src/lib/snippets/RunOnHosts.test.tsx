import {act, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {RunOnHostsPanel} from './RunOnHostsPanel'
import {host, makeFake, Providers} from '../../test/fakeBackend'
import type {ExecRun, Snippet} from '../types'

const hosts = [
  host({id: 'h1', label: 'web-1', hostname: 'w1.example.com', env: 'dev'}),
  host({id: 'h2', label: 'web-2', hostname: 'w2.example.com', env: 'prod'}),
  host({id: 'h3', label: 'db-1', hostname: 'db.example.com', env: 'staging'}),
]
const disk: Snippet = {id: 's1', label: 'Disk', description: 'free space', script: 'df -h {{path|/}}'}

function setup(over: {snippet?: Snippet; snippets?: Snippet[]} = {}) {
  const be = makeFake({hosts})
  const onClose = vi.fn()
  render(<Providers be={be}><RunOnHostsPanel hosts={hosts} snippets={over.snippets ?? [disk]} snippet={over.snippet} onClose={onClose} /></Providers>)
  return {be, onClose}
}
const runBtn = () => screen.getByRole('button', {name: /^Run on/})
const calls = (be: ReturnType<typeof makeFake>, n: string) => be.state.calls.filter(c => c[0] === n)
const update = (be: ReturnType<typeof makeFake>, id: string, run: Partial<ExecRun>) => act(() => be.emit(`exec:update:${id}`, {...be.state.execRuns[id], ...run}))

describe('Run on hosts: the form', () => {
  it('needs a command and at least one host, and counts the hosts chosen', async () => {
    setup()
    expect(runBtn()).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Command'), 'uptime')
    expect(runBtn()).toBeDisabled() // no host yet
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-1'}))
    expect(runBtn()).toBeEnabled()
    expect(runBtn()).toHaveTextContent('Run on 1 host')
    expect(screen.getByText('1 host chosen')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('checkbox', {name: 'db-1'}))
    expect(runBtn()).toHaveTextContent('Run on 2 hosts')
  })

  it('filters the hosts, and All shown picks only the ones that show', async () => {
    setup()
    await userEvent.type(screen.getByLabelText('Filter hosts'), 'web')
    expect(screen.queryByRole('checkbox', {name: 'db-1'})).toBeNull()
    await userEvent.click(screen.getByRole('checkbox', {name: 'Select all shown'}))
    expect(screen.getByRole('checkbox', {name: 'web-1'})).toBeChecked()
    expect(screen.getByRole('checkbox', {name: 'web-2'})).toBeChecked()
    await userEvent.clear(screen.getByLabelText('Filter hosts'))
    expect(screen.getByRole('checkbox', {name: 'db-1'})).not.toBeChecked()
    await userEvent.type(screen.getByLabelText('Filter hosts'), 'zzz')
    expect(screen.getByText('No host matches.')).toBeInTheDocument()
  })

  it('starts from a snippet (given, or chosen), asks for its blanks, and leaves the per-host names for each host', async () => {
    const {be} = setup({snippet: {...disk, script: 'df -h {{path|/}} # {{host}} {{user}}'}})
    expect(screen.getByLabelText('Command')).toHaveValue('df -h {{path|/}} # {{host}} {{user}}')
    expect(screen.getByLabelText('path')).toBeInTheDocument()
    expect(screen.queryByLabelText('host')).toBeNull() // not asked: every host fills its own in
    await userEvent.type(screen.getByLabelText('path'), '/var')
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-1'}))
    await userEvent.click(runBtn())
    await waitFor(() => expect(calls(be, 'exec.run')).toHaveLength(1))
    expect(calls(be, 'exec.run')[0][1]).toEqual({host_ids: ['h1'], command: 'df -h /var # {{host}} {{user}}', timeout_sec: 60})
  })

  it('picks a snippet from the list', async () => {
    setup()
    await userEvent.click(screen.getByRole('combobox', {name: /Start from a snippet/}))
    await userEvent.click(screen.getByRole('option', {name: /Disk/}))
    expect(screen.getByLabelText('Command')).toHaveValue('df -h {{path|/}}')
    expect(screen.getByLabelText('path')).toBeInTheDocument()
  })

  it('sends the timeout that was chosen', async () => {
    const {be} = setup()
    await userEvent.type(screen.getByLabelText('Command'), 'sleep 1')
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-1'}))
    await userEvent.click(screen.getByRole('combobox', {name: /Timeout/}))
    await userEvent.click(screen.getByRole('option', {name: '5 minutes'}))
    await userEvent.click(runBtn())
    await waitFor(() => expect(calls(be, 'exec.run')[0][1]).toMatchObject({timeout_sec: 300}))
  })
})

describe('Run on hosts: production', () => {
  it('asks before running on a production host, and stays on the form when the answer is no', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await userEvent.type(screen.getByLabelText('Command'), 'systemctl restart nginx')
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-1'}))
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-2'})) // production
    await userEvent.click(runBtn())
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1))
    expect(confirm.mock.calls[0][0]).toMatch(/1 of the 2 hosts is? production \(web-2\)|1 of the 2 hosts are production \(web-2\)/)
    expect(calls(be, 'exec.run')).toHaveLength(0)
    expect(screen.getByLabelText('Command')).toBeInTheDocument() // still the form
    confirm.mockReturnValue(true)
    await userEvent.click(runBtn())
    await waitFor(() => expect(calls(be, 'exec.run')).toHaveLength(1))
  })

  it('does not ask when no host is production', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await userEvent.type(screen.getByLabelText('Command'), 'id')
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-1'}))
    await userEvent.click(runBtn())
    await waitFor(() => expect(calls(be, 'exec.run')).toHaveLength(1))
    expect(confirm).not.toHaveBeenCalled()
  })
})

describe('Run on hosts: the results', () => {
  async function started() {
    const ctx = setup()
    await userEvent.type(screen.getByLabelText('Command'), 'uptime')
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-1'}))
    await userEvent.click(screen.getByRole('checkbox', {name: 'db-1'}))
    await userEvent.click(runBtn())
    await screen.findByRole('list', {name: 'Results'})
    return {...ctx, id: Object.keys(ctx.be.state.execRuns)[0]}
  }
  const row = (label: string) => screen.getByRole('listitem', {name: new RegExp('^' + label)})

  it('shows each host as it finishes, with the output of a failure open and a success one click away', async () => {
    const {be, id} = await started()
    expect(screen.getByRole('status')).toHaveTextContent('Running: 0 of 2 hosts done')
    update(be, id, {results: [
      {host_id: 'h1', label: 'web-1', state: 'ok', exit_code: 0, output: ' 10:00 up 3 days\n', duration_ms: 1200},
      {host_id: 'h3', label: 'db-1', state: 'running', exit_code: 0, output: '', duration_ms: 0},
    ]})
    expect(screen.getByRole('status')).toHaveTextContent('Running: 1 of 2 hosts done')
    expect(row('web-1')).toHaveTextContent('exit 0 · 1.2s')
    expect(screen.queryByLabelText('Output of web-1')).toBeNull() // a success stays closed
    await userEvent.click(within(row('web-1')).getByRole('button'))
    expect(screen.getByLabelText('Output of web-1')).toHaveTextContent('up 3 days')

    update(be, id, {done: true, results: [
      {host_id: 'h1', label: 'web-1', state: 'ok', exit_code: 0, output: ' 10:00 up 3 days\n', duration_ms: 1200},
      {host_id: 'h3', label: 'db-1', state: 'failed', exit_code: 2, output: 'no such unit\n', error: 'exited with status 2', duration_ms: 300},
    ]})
    expect(screen.getByRole('status')).toHaveTextContent('Finished: 2 of 2 hosts done, 1 failed')
    expect(screen.getByLabelText('Output of db-1')).toHaveTextContent('no such unit') // a failure opens by itself
    expect(row('db-1')).toHaveTextContent('exited with status 2')
    await userEvent.click(within(row('db-1')).getByRole('button')) // and can be closed
    expect(screen.queryByLabelText('Output of db-1')).toBeNull()
  })

  it('shows a refused host with its reason, strips terminal colours, and says when the output was cut', async () => {
    const {be, id} = await started()
    update(be, id, {done: true, results: [
      {host_id: 'h1', label: 'web-1', state: 'failed', exit_code: -1, output: '', error: 'the host key of "web-1" is not trusted yet: open the host once in a terminal tab and trust it', duration_ms: 5},
      {host_id: 'h3', label: 'db-1', state: 'ok', exit_code: 0, output: '\u001b[32mgreen\u001b[0m text', truncated: true, duration_ms: 10},
    ]})
    expect(row('web-1')).toHaveTextContent('not trusted yet')
    await userEvent.click(within(row('db-1')).getByRole('button'))
    const out = screen.getByLabelText('Output of db-1')
    expect(out).toHaveTextContent('green text')
    expect(out.textContent).not.toContain('\u001b')
    expect(out).toHaveTextContent('output cut at 64 KB')
  })

  it('says so when a host printed nothing', async () => {
    const {be, id} = await started()
    update(be, id, {done: true, results: [
      {host_id: 'h1', label: 'web-1', state: 'ok', exit_code: 0, output: '', duration_ms: 10},
      {host_id: 'h3', label: 'db-1', state: 'ok', exit_code: 0, output: 'x', duration_ms: 10},
    ]})
    await userEvent.click(within(row('web-1')).getByRole('button'))
    expect(screen.getByLabelText('Output of web-1')).toHaveTextContent('(no output)')
  })

  it('Cancel the run stops it; Copy all output copies what finished, without colours', async () => {
    const write = vi.fn(async (_text: string) => {})
    const {be, id} = await started()
    be.system.writeClipboard = write
    update(be, id, {results: [
      {host_id: 'h1', label: 'web-1', state: 'ok', exit_code: 0, output: '\u001b[1mup\u001b[0m\n', duration_ms: 10},
      {host_id: 'h3', label: 'db-1', state: 'running', exit_code: 0, output: '', duration_ms: 0},
    ]})
    await userEvent.click(screen.getByRole('button', {name: 'Cancel the run'}))
    expect(calls(be, 'exec.cancel')).toEqual([['exec.cancel', id]])
    await userEvent.click(screen.getByRole('button', {name: 'Copy all output'}))
    await waitFor(() => expect(write).toHaveBeenCalledTimes(1))
    expect(write.mock.calls[0][0]).toContain('== web-1 (ok) ==\nup')
    expect(String(write.mock.calls[0][0])).not.toContain('\u001b')
    update(be, id, {done: true})
    expect(screen.queryByRole('button', {name: 'Cancel the run'})).toBeNull()
  })

  it('Run again goes back to the form with the command and the hosts still there, and forgets the old run', async () => {
    const {be, id} = await started()
    update(be, id, {done: true})
    await userEvent.click(screen.getByRole('button', {name: 'Run again…'}))
    expect(screen.getByLabelText('Command')).toHaveValue('uptime')
    expect(screen.getByRole('checkbox', {name: 'web-1'})).toBeChecked()
    await waitFor(() => expect(calls(be, 'exec.forget')).toContainEqual(['exec.forget', id]))
  })

  it('shows the reason when the run cannot start', async () => {
    const {be} = setup()
    be.exec.run = async () => { throw 'The vault is locked. Unlock it first.' }
    await userEvent.type(screen.getByLabelText('Command'), 'id')
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-1'}))
    await userEvent.click(runBtn())
    expect(await screen.findByRole('alert')).toHaveTextContent('The vault is locked')
  })
})

describe('Run on hosts: leaving', () => {
  it('stops and forgets a run that is still going when the panel goes away', async () => {
    const be = makeFake({hosts})
    const {unmount} = render(<Providers be={be}><RunOnHostsPanel hosts={hosts} snippets={[]} onClose={vi.fn()} /></Providers>)
    await userEvent.type(screen.getByLabelText('Command'), 'id')
    await userEvent.click(screen.getByRole('checkbox', {name: 'web-1'}))
    await userEvent.click(runBtn())
    await screen.findByRole('list', {name: 'Results'})
    const id = Object.keys(be.state.execRuns)[0]
    unmount()
    await waitFor(() => expect(calls(be, 'exec.cancel')).toContainEqual(['exec.cancel', id]))
    expect(calls(be, 'exec.forget')).toContainEqual(['exec.forget', id])
  })
})
