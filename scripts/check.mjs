import { resolve } from 'node:path'
import { root, run, npm } from './runtime.mjs'
await run(npm, ['run', 'check', '--workspace', '@signal-atlas/web'])
await run('go', ['test', '-race', './...'], resolve(root, 'api'))
await run(npm, ['run', 'test', '--workspace', '@signal-atlas/web'])
