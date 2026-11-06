/** A screen asks the SFTP tab to open a host at a folder (the Server panel has a Files button and "Open in SFTP" next to folders). */
export const OPEN_IN_SFTP = 'vutty:sftp-open'

export interface OpenInSftp {
  host: {id: string; label: string}
  /** '' opens the home folder */
  path: string
}

export function openInSftp(host: {id: string; label: string}, path = '') {
  window.dispatchEvent(new CustomEvent<OpenInSftp>(OPEN_IN_SFTP, {detail: {host: {id: host.id, label: host.label}, path}}))
}
