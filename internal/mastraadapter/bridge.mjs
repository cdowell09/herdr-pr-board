import { readFileSync, realpathSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';

console.log = console.error;
const [nodeMajor, nodeMinor] = process.versions.node.split('.').map(Number);
if (nodeMajor < 22 || (nodeMajor === 22 && nodeMinor < 19)) throw new Error('Mastra Code adapter requires Node.js 22.19.0 or later');

// Resolve the installed CLI package without executing its headless entrypoint.
// Mastra Code 0.39.0's CLI does not forward the SDK isolation controls.
const [entry, work, checkout, settingsPath] = process.argv.slice(2);
process.chdir(work);
let packageDir = dirname(realpathSync(entry));
let manifest;
for (;;) {
  try { manifest = JSON.parse(readFileSync(join(packageDir, 'package.json'), 'utf8')); }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
  if (manifest?.name === 'mastracode') break;
  const parent = dirname(packageDir);
  if (parent === packageDir) throw new Error('Install mastracode 0.39.0 with npm; cannot locate its package');
  packageDir = parent;
}
if (manifest.version !== '0.39.0') throw new Error('Mastra Code adapter requires exactly mastracode 0.39.0');
const require = createRequire(join(packageDir, 'package.json'));
const sdkManifest = require.resolve('@mastra/code-sdk/package.json');
const sdk = JSON.parse(readFileSync(sdkManifest, 'utf8'));
if (sdk.version !== '1.7.0') throw new Error('Mastra Code adapter requires exactly @mastra/code-sdk 1.7.0');
const importSDK = subpath => import(pathToFileURL(resolve(dirname(sdkManifest),
  subpath ? sdk.exports['./*'].import.default.replace('*', subpath) : sdk.exports['.'].import.default)));
const sdkRequire = createRequire(sdkManifest);
const coreManifest = sdkRequire.resolve('@mastra/core/package.json');
const core = JSON.parse(readFileSync(coreManifest, 'utf8'));
if (core.version !== '1.65.0') throw new Error('Mastra Code adapter requires exactly @mastra/core 1.65.0');
const { createMastraCode, runMC, denyPolicy } = await importSDK();
const { Workspace, LocalFilesystem, WORKSPACE_TOOLS } = await import(pathToFileURL(resolve(dirname(coreManifest), core.exports['./workspace'].import.default)));
const memoryManifest = sdkRequire.resolve('@mastra/memory/package.json');
const memory = JSON.parse(readFileSync(memoryManifest, 'utf8'));
if (memory.version !== '1.28.3') throw new Error('Mastra Code adapter requires exactly @mastra/memory 1.28.3');
const { Memory } = await import(pathToFileURL(resolve(dirname(memoryManifest), memory.exports['.'].import.default)));
const { loadSettings, resolveModelDefaults } = await importSDK('onboarding/settings');
const { getBuiltinModePack } = await importSDK('onboarding/packs');
const nativeSettings = loadSettings();
const pack = getBuiltinModePack(nativeSettings.models.activeModelPackId);
const model = resolveModelDefaults(nativeSettings, pack ? [pack] : []).build;
if (!model) throw new Error('Select a build model in Mastra Code before running a review');

const names = ['READ_FILE', 'LIST_FILES', 'GREP', 'FILE_STAT'].map(name => WORKSPACE_TOOLS.FILESYSTEM[name]);
const workspace = new Workspace({
  id: 'pr-board-review',
  filesystem: new LocalFilesystem({ basePath: checkout, contained: true, readOnly: true }),
  skills: [], lsp: false,
  tools: { enabled: false, ...Object.fromEntries(names.map(name => [name, { enabled: true, requireApproval: false }])) },
});
let boot;
let run;
let subscription;
const abort = new AbortController();
process.on('SIGTERM', () => abort.abort());
process.on('SIGINT', () => abort.abort());
try {
  // Use the empty parent as cwd so the SDK cannot load the checkout's .env.
  // AuthStorage retains its native location; PR Board never opens credentials.
  boot = await createMastraCode({
    cwd: work, settingsPath, workspace,
    storage: { backend: 'libsql', url: 'file:' + join(work, 'review.db') },
    disableHooks: true, disableMcp: true, disablePlugins: true,
    disableGithubSignals: true, disableSettingsOmSeed: true,
    // The SDK's mandatory task processor needs thread memory. Disable its
    // model-backed memory features and keep this fresh thread in the local store.
    memory: new Memory({ options: { observationalMemory: false, semanticRecall: false, workingMemory: { enabled: false } } }),
    subagents: [], intervalHandlers: [], unixSocketPubSub: false,
    initialState: { projectPath: checkout, untrustedCheckout: true, skipGlobalInstructions: true },
    modes: [{ id: 'build', defaultModelId: model, availableTools: names }],
  });
  let prompt = '';
  for await (const chunk of process.stdin) prompt += chunk;
  const thread = await boot.session.thread.create();
  subscription = await boot.codeAgent.subscribeToThread({ threadId: thread.id, resourceId: boot.session.identity.getResourceId() });
  // AgentController's "complete" also covers unknown provider finish reasons.
  // Observe the public raw stream so only a successful model stop is accepted.
  const finish = (async () => {
    let reason;
    for await (const chunk of subscription.stream) {
      if (chunk.type === 'finish') reason = chunk.payload?.stepResult?.reason;
    }
    return reason;
  })();
  run = runMC({ controller: boot.controller, session: boot.session, prompt, policy: denyPolicy, signal: abort.signal });
  let text = '';
  for await (const event of run) {
    if (event.type === 'message_end' && event.message.role === 'assistant') {
      const parts = event.message.content.parts;
      const pending = parts.some(part => part.type.startsWith('tool-') &&
        (part.type !== 'tool-invocation' || !['result', 'output-error', 'output-denied'].includes(part.toolInvocation?.state)));
      const lastTool = parts.findLastIndex(part => part.type.startsWith('tool-'));
      text = pending || event.message.content.metadata?.stopReason !== 'complete' ? '' :
        parts.slice(lastTool + 1).filter(part => part.type === 'text').map(part => part.text).join('');
    }
  }
  const result = await run.result;
  subscription.unsubscribe();
  const providerFinishReason = await finish;
  const output = JSON.stringify({ type: 'result', status: result.status, finishReason: result.finishReason, providerFinishReason, exitCode: result.exitCode, error: result.error, text });
  await new Promise((resolve, reject) => process.stdout.write(output, error => error ? reject(error) : resolve()));
  process.exitCode = result.exitCode;
} finally {
  subscription?.unsubscribe();
  if (boot) {
    boot.stopPluginSignalProviders();
    await Promise.allSettled([boot.controller.stopIntervals(), boot.controller.getMastra()?.stopWorkers()]);
  }
}
process.exit(process.exitCode ?? 1);
