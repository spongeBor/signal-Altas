import { existsSync, readdirSync } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn, spawnSync } from 'node:child_process'

export const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
// Cursor may inherit an older shell Node. Prefer a supported installed runtime.
const [major, minor] = process.versions.node.split('.').map(Number)
if (major < 22 || (major === 22 && minor < 18)) {
  const base = resolve(homedir(), '.nvm/versions/node')
  const versions = existsSync(base)
    ? readdirSync(base)
        .filter((v) => /^v(2[4-9]|[3-9]\d)\./.test(v))
        .sort((a, b) => b.localeCompare(a, undefined, { numeric: true }))
        .map((v) => resolve(base, v, 'bin/node'))
    : []
  const candidates = [
    ...versions,
    '/opt/homebrew/opt/node@24/bin/node',
    resolve(homedir(), '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node'),
  ]
  const binary = candidates.find((p) => existsSync(p))
  if (!binary) {
    console.error('请安装 Node.js 24，然后运行 nvm use 或重新打开 Cursor 终端。')
    process.exit(1)
  }
  const result = spawnSync(binary, process.argv.slice(1), {
    stdio: 'inherit',
    env: { ...process.env, PATH: dirname(binary) + ':' + process.env.PATH },
  })
  process.exit(result.status ?? 1)
}
export const env = { ...process.env, PATH: dirname(process.execPath) + ':' + process.env.PATH }
export const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm'
export function run(command, args, cwd = root) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd, env, stdio: 'inherit' })
    child.on('error', reject)
    child.on('exit', (code, signal) =>
      code === 0 ? resolve() : reject(new Error(`${command} 退出（${code ?? signal}）`)),
    )
  })
}
