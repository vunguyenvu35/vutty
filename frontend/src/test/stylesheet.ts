import {readFileSync} from 'node:fs'
import {join} from 'node:path'

/** All of the app's CSS as one string: src/styles.css with each @import replaced by the file it names (the order is the cascade). */
export function stylesheet(): string {
  const src = join(__dirname, '..')
  return readFileSync(join(src, 'styles.css'), 'utf8').replace(/@import '([^']+)';/g, (_, f: string) => readFileSync(join(src, f), 'utf8'))
}
