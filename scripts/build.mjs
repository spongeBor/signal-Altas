import { mkdirSync } from 'node:fs'
import { root, run, npm } from './runtime.mjs'
import { resolve } from 'node:path'
await run(npm, ['run', 'build', '--workspace', '@signal-atlas/web'])
mkdirSync(resolve(root, '.local/bin'), { recursive: true })
await run(
  'go',
  ['build', '-trimpath', '-o', resolve(root, '.local/bin/signal-atlas'), './cmd/server'],
  resolve(root, 'api'),
)
console.log('构建完成。npm start 可运行同源生产版本。')
