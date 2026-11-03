import type {Backend} from '../backend'
import type {RemoteHost} from './RemotePane'
import type {PaneFs} from './FilePane'

/** The id of the "host" that is this computer: picked on the right side, it makes that pane a second view of local files. */
export const LOCAL_HOST_ID = 'local'
export const LOCAL_HOST: RemoteHost = {id: LOCAL_HOST_ID, label: 'This computer'}

/** What a pane needs to browse and edit this computer's files. */
export function localPaneFs(be: Backend): PaneFs {
  return {
    list: d => be.local.list(d),
    mkdir: p => be.local.mkdir(p),
    rename: (a, b) => be.local.rename(a, b),
    remove: (p, isDir) => be.local.remove(p, isDir),
    chmod: (p, m) => be.local.chmod(p, m),
    open: e => be.system.openPath(e.path),
    openWith: async e => {
      const app = await be.system.pickFile('Open with…')
      if (app) await be.system.openPathWith(e.path, app)
    },
  }
}
