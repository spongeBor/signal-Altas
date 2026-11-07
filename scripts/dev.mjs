import { spawn } from 'node:child_process'
import { createServer } from 'node:net'
import { resolve } from 'node:path'
import { root, env, npm } from './runtime.mjs'
import { databaseFromSSH } from './postgres-ssh.mjs'

for (const port of [8790, 5173]) {
  await new Promise((res, rej) => {
    const s = createServer()
    s.once('error', () => rej(new Error(`本机端口 ${port} 已被占用，请先退出另一个实例。`)))
    s.listen(port, '127.0.0.1', () => s.close(res))
  })
}
const children = []
let closing = false
function stop(code = 0) {
  if (closing) return
  closing = true
  for (const c of children) {
    try {
      process.platform === 'win32' ? c.kill('SIGTERM') : process.kill(-c.pid, 'SIGTERM')
    } catch {}
  }
  setTimeout(() => {
    for (const c of children) {
      try {
        process.platform === 'win32' ? c.kill('SIGKILL') : process.kill(-c.pid, 'SIGKILL')
      } catch {}
    }
    process.exit(code)
  }, 1500)
}
function start(command, args, cwd, pipe = false) {
  const c = spawn(command, args, {
    cwd,
    env,
    stdio: pipe ? ['pipe', 'inherit', 'inherit'] : 'inherit',
    detached: process.platform !== 'win32',
  })
  children.push(c)
  c.on('error', (e) => {
    console.error(e.message)
    stop(1)
  })
  c.on('exit', (code) => {
    if (!closing) stop(code || 0)
  })
  return c
}
const apiArgs = ['run', './cmd/server', '-data-dir', resolve(root, '.local/data')]
const viaSSH = process.argv.includes('--db-ssh'),
  ask = process.argv.includes('--ask-db')
if (viaSSH && ask) throw new Error('请选择一种数据库输入方式。')
let dsn = viaSSH ? await databaseFromSSH() : null
if (ask) apiArgs.push('-ask-db')
if (viaSSH) apiArgs.push('-database-stdin')
process.on('SIGINT', () => stop())
process.on('SIGTERM', () => stop())
const api = start('go', apiArgs, resolve(root, 'api'), viaSSH)
if (viaSSH) {
  api.stdin.end(dsn + '\n')
  dsn = null
}
// Keep Vite from reading the terminal while Go accepts a hidden database URL.
if (ask || viaSSH) {
  const end = Date.now() + 300000
  while (!closing) {
    try {
      const r = await fetch('http://127.0.0.1:8790/api/v1/health')
      if (r.ok) break
    } catch {}
    if (Date.now() > end) {
      console.error('等待数据库输入超时。')
      stop(1)
      break
    }
    await new Promise((r) => setTimeout(r, 250))
  }
}
if (!closing) start(npm, ['run', 'dev', '--workspace', '@signal-atlas/web'], root)
console.log(
  '\nSignal Atlas · http://127.0.0.1:5173\n按 Ctrl+C 同时停止前后端。设备密码在“采集连接”中输入。\n',
)
