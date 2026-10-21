// 只执行 get_state，不发送 prompt、不读取宿主机 pi 配置或模型密钥。
import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const [major, minor] = process.versions.node.split('.').map(Number);
if (major < 22 || (major === 22 && minor < 19)) {
  throw new Error('pi 1.0.0 需要 Node >=22.19.0');
}
const dir = mkdtempSync(join(tmpdir(), 'docharbor-pi-smoke-'));
const agent = join(dir, 'agent');
mkdirSync(agent);
writeFileSync(join(agent, 'settings.json'), JSON.stringify({ extensions: ['-builtin:llama.cpp'] }));
const env = {
  PATH: process.env.PATH,
  HOME: dir,
  PI_CODING_AGENT_DIR: agent,
  PI_OFFLINE: '1',
  PI_SKIP_VERSION_CHECK: '1',
  NO_PROXY: '127.0.0.1,localhost',
  no_proxy: '127.0.0.1,localhost',
};
let child;
try {
  const version = execFileSync('pi', ['--version'], { cwd: dir, env, encoding: 'utf8', timeout: 20000 }).trim();
  if (version !== '1.0.0') throw new Error(`pi 版本不匹配：${version}`);
  await new Promise((resolve, reject) => {
    child = spawn('pi', ['--mode', 'rpc', '--no-tools', '--no-context-files', '--no-skills', '--no-session'], {
      cwd: dir, env, stdio: ['pipe', 'pipe', 'pipe'],
    });
    let buffer = '';
    let success = false;
    const timer = setTimeout(() => {
      child.kill('SIGKILL');
      reject(new Error('RPC get_state 在 20 秒内未完成'));
    }, 20000);
    // 诊断只来自当前无凭据子进程；限制长度，便于定位原生模块/musl 错误。
    let stderr = '';
    child.stderr.on('data', chunk => { stderr = (stderr + chunk.toString()).slice(-8000); });
    child.stdout.setEncoding('utf8');
    child.stdout.on('data', chunk => {
      buffer += chunk;
      let index;
      while ((index = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, index).replace(/\r$/, '');
        buffer = buffer.slice(index + 1);
        try {
          const record = JSON.parse(line);
          if (record.type === 'response' && record.id === 'smoke-state') {
            if (record.command !== 'get_state' || record.success !== true) {
              child.kill('SIGKILL');
              reject(new Error('RPC get_state 返回失败'));
            } else {
              success = true;
              // 收到响应后再关闭 stdin，避免 EOF 使异步命令来不及完成。
              child.stdin.end();
            }
          }
        } catch (error) {
          child.kill('SIGKILL');
          reject(error);
        }
      }
    });
    child.on('error', error => { clearTimeout(timer); reject(error); });
    child.on('close', code => {
      clearTimeout(timer);
      if (!success || code !== 0) reject(new Error(`RPC 子进程异常结束 (${code})：${stderr}`));
      else resolve();
    });
    child.stdin.on('error', error => { clearTimeout(timer); reject(error); });
    child.stdin.write(JSON.stringify({ id: 'smoke-state', type: 'get_state' }) + '\n');
  });
  console.log(JSON.stringify({ node: process.versions.node, pi: version, get_state: 'passed', model_calls: 0 }));
} finally {
  if (child && child.exitCode === null) child.kill('SIGKILL');
  rmSync(dir, { recursive: true, force: true });
}
