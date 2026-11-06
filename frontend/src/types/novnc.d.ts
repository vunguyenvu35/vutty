// noVNC ships no types: the part of its API the viewer uses.
declare module '@novnc/novnc' {
  export interface RFBOptions {
    shared?: boolean
    credentials?: {username?: string; password?: string; target?: string}
    wsProtocols?: string[]
  }
  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, urlOrChannel: string, options?: RFBOptions)
    viewOnly: boolean
    scaleViewport: boolean
    clipViewport: boolean
    resizeSession: boolean
    background: string
    focusOnClick: boolean
    disconnect(): void
    sendCredentials(credentials: {username?: string; password?: string; target?: string}): void
    sendCtrlAltDel(): void
    focus(): void
    blur(): void
  }
}
