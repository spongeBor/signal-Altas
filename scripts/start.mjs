import { existsSync } from 'node:fs'
import { resolve } from 'node:path'
import { root, run } from './runtime.mjs'
const binary = resolve(root, '.local/bin/signal-atlas')
if (!existsSync(binary)) {
  console.error('请先运行 npm run build')
  process.exit(1)
}
const args = ['-data-dir', resolve(root, '.local/data'), '-web-dir', resolve(root, 'web/dist')]
if (process.argv.includes('--ask-db')) args.push('-ask-db')
await run(binary, args)
