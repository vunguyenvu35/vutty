import {vi} from 'vitest'
import {attachGpu} from './gpu'

const made: {dispose: ReturnType<typeof vi.fn>; lose?: () => void; failOnLoad?: boolean}[] = []
vi.mock('@xterm/addon-webgl', () => ({
  WebglAddon: class {
    dispose = vi.fn()
    lose?: () => void
    failOnLoad = (globalThis as {__gpuFail?: boolean}).__gpuFail === true
    onContextLoss(cb: () => void) { this.lose = cb }
    constructor() { made.push(this as never) }
  },
}))

const term = (fail = false) => ({loadAddon: vi.fn(() => { if (fail) throw new Error('no webgl2') })}) as never

describe('attachGpu', () => {
  beforeEach(() => { made.length = 0 })

  it('loads the addon into the terminal and disposes it once', () => {
    const t = term()
    const g = attachGpu(t)!
    expect(g).not.toBeNull()
    expect((t as {loadAddon: unknown}).loadAddon).toHaveBeenCalledWith(made[0])
    g.dispose(); g.dispose()
    expect(made[0].dispose).toHaveBeenCalledTimes(1)
  })

  it('returns null, and leaves nothing loaded, when WebGL cannot be used', () => {
    expect(attachGpu(term(true))).toBeNull()
    expect(made[0].dispose).toHaveBeenCalled()
  })

  it('drops the addon and says so when the context is lost', () => {
    const lost = vi.fn()
    const g = attachGpu(term(), lost)!
    made[0].lose!()
    expect(made[0].dispose).toHaveBeenCalledTimes(1)
    expect(lost).toHaveBeenCalled()
    g.dispose() // nothing left to dispose
    expect(made[0].dispose).toHaveBeenCalledTimes(1)
  })
})
