import type {Backend} from '../backend'
import type {TFn} from '../i18n'
import type {PaneFs} from './FilePane'

/** The id of a terminal that is a shell in a pod of a cluster opened with a kubeconfig. */
export const POD_PREFIX = 'kubeexec:'

/**
 * The files inside a pod, read only: folders are listed and a file is read by running sh, stat and cat in the container, through the
 * exec call of the shell. A file is copied into a private temporary folder to be opened; nothing is written to the pod.
 */
export function podFs(be: Backend, t: TFn, hostId: string): PaneFs {
  const readOnly = () => Promise.reject(new Error(t('The files of a pod can be looked at and copied out, not changed.')))
  const fetch = async (path: string) => be.server.podFilesFetch(hostId, path, await be.local.tempDir())
  return {
    readOnly: true,
    list: d => be.server.podFilesList(hostId, d),
    mkdir: readOnly,
    rename: readOnly,
    remove: readOnly,
    open: async e => { await be.system.openPath(await fetch(e.path)) },
    openWith: async e => {
      const app = await be.system.pickFile(t('Open with…'))
      if (app) await be.system.openPathWith(await fetch(e.path), app)
    },
  }
}
