import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { execFileSync } from 'node:child_process';

console.log = console.error;
let [entry, work, checkout] = process.argv.slice(2);
process.chdir(work);

async function main() {
  if (entry === 'cursor') {
    // npm only locates the installed package. It never installs or runs a script.
    const npm = process.platform === 'win32' ? ['cmd.exe', ['/d', '/s', '/c', 'npm root -g']] : ['npm', ['root', '-g']];
    const root = execFileSync(...npm, { cwd: work, encoding: 'utf8', timeout: 15000, maxBuffer: 65536, stdio: ['ignore', 'pipe', 'pipe'] }).trim();
    entry = join(root, '@cursor', 'sdk', 'dist', 'esm', 'index.js');
  }
  const packageDir = resolve(dirname(entry), '..', '..');
  const manifest = JSON.parse(readFileSync(join(packageDir, 'package.json'), 'utf8'));
  if (manifest.name !== '@cursor/sdk' || manifest.version !== '1.0.31' ||
      resolve(entry) !== join(packageDir, 'dist', 'esm', 'index.js')) {
    throw new Error('Cursor adapter requires @cursor/sdk 1.0.31; --cursor-executable must name its dist/esm/index.js');
  }
  const { Agent, JsonlLocalAgentStore } = await import(pathToFileURL(entry));
  let run;
  let cancelled = false;
  const cancel = () => { cancelled = true; void run?.cancel().catch(error => console.error(String(error))); };
  process.on('SIGTERM', cancel);
  process.on('SIGINT', cancel);
  // The SDK resolves its own stored login. HOME and authentication stay native.
  const agent = await Agent.create({
    model: { id: process.env.CURSOR_MODEL || 'composer-2.5' },
    tools: ['read', 'ls', 'grep', 'glob'], mcpServers: {}, agents: {},
    local: { cwd: checkout, settingSources: [], sandboxOptions: { enabled: false },
      store: new JsonlLocalAgentStore(join(work, 'store')), enableAgentRetries: false },
  });
  try {
    let prompt = '';
    for await (const chunk of process.stdin) prompt += chunk;
    if (cancelled) throw new Error('Cursor review cancelled before the run started');
    let turnEnded = 0;
    const pending = new Set();
    run = await agent.send(prompt, { onDelta: ({ update }) => {
      if (update.type === 'turn-ended') turnEnded++;
      if (update.type === 'tool-call-started') pending.add(update.callId);
      if (update.type === 'tool-call-completed') pending.delete(update.callId);
    } });
    if (cancelled) await run.cancel();
    const result = await run.wait();
    const output = JSON.stringify({ type: 'result', status: result.status, turnEnded, pending: pending.size,
      cancelled, error: result.error, text: result.result });
    await new Promise((resolve, reject) => process.stdout.write(output, error => error ? reject(error) : resolve()));
    process.exitCode = result.status === 'finished' && !cancelled ? 0 : 1;
  } finally {
    await agent[Symbol.asyncDispose]();
  }
}

main().catch(error => { console.error(String(error)); process.exitCode = 1; }).finally(() => process.exit(process.exitCode ?? 1));
