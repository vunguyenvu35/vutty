import type {Backend} from '../backend'
import type {TFn} from '../i18n'
import {errMsg} from '../util'
import type {EditEvent} from '../types'
import {joinPath} from './format'
import type {PaneFs} from './FilePane'

/**
 * What a file list needs from an SFTP connection. A remote file is fetched into a private temporary folder to be opened;
 * Edit also watches it, so that each save goes back to the server. `edited` collects the files being watched: the owner
 * releases them (editStop) when the connection ends.
 */
export function remoteFs(be: Backend, t: TFn, s: string, edited: Set<string>): PaneFs {
  const fetch = async (path: string, name: string) => {
    const dir = await be.local.tempDir()
    const res = await be.sftp.downloadAll(s, [path], dir, true)
    if (res.files < 1) throw new Error(t('Could not download the file'))
    return joinPath(dir, name)
  }
  return {
    list: d => be.sftp.list(s, d),
    mkdir: p => be.sftp.mkdir(s, p),
    rename: (a, b) => be.sftp.rename(s, a, b),
    remove: (p, isDir) => be.sftp.remove(s, p, isDir),
    chmod: (p, m) => be.sftp.chmod(s, p, m),
    open: async e => { await be.system.openPath(await fetch(e.path, e.name)) },
    openWith: async e => {
      const app = await be.system.pickFile(t('Open with…'))
      if (app) await be.system.openPathWith(await fetch(e.path, e.name), app)
    },
    edit: async e => {
      const local = await fetch(e.path, e.name)
      await be.system.openPath(local)
      await be.sftp.editStart(s, e.path, local)
      edited.add(e.path)
    },
  }
}

/** Tells the user how a watched file went (saved, failed, or changed on the server meanwhile: overwrite?). Returns the unsubscribe. */
export function watchEdits(be: Backend, t: TFn, s: string, toast: {info(m: string): void; error(m: string): void},
  confirm: (q: string, o?: {confirmLabel?: string; danger?: boolean}) => Promise<boolean>): () => void {
  return be.on(`sftp:edit:${s}`, (ev: EditEvent) => {
    const name = ev.remote.slice(ev.remote.lastIndexOf('/') + 1)
    if (ev.state === 'uploaded') toast.info(t('Saved {name} to the server', {name}))
    else if (ev.state === 'error') toast.error(`${name}: ${ev.message || t('could not be saved to the server')}`)
    else void confirm(t('{name} changed on the server since you opened it. Overwrite with your version?', {name}), {confirmLabel: t('Overwrite'), danger: true})
      .then(yes => (yes ? be.sftp.editPush(s, ev.remote, true) : undefined))
      .catch(e => toast.error(`${name}: ${errMsg(e)}`))
  })
}
