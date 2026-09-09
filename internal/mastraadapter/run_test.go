package mastraadapter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalText(t *testing.T) {
	valid := `{"type":"result","status":"completed","finishReason":"complete","providerFinishReason":"stop","exitCode":0,"text":"{}"}`
	if got, err := finalText([]byte(valid)); err != nil || string(got) != "{}" {
		t.Fatalf("text=%s err=%v", got, err)
	}
	for name, data := range map[string]string{
		"partial":          strings.Replace(valid, "completed", "aborted", 1),
		"provider length":  strings.Replace(valid, `"providerFinishReason":"stop"`, `"providerFinishReason":"length"`, 1),
		"provider tools":   strings.Replace(valid, `"providerFinishReason":"stop"`, `"providerFinishReason":"tool-calls"`, 1),
		"missing provider": strings.Replace(valid, `"providerFinishReason":"stop",`, "", 1),
		"unknown finish":   strings.Replace(valid, `"complete"`, `"stop"`, 1),
		"missing exit":     strings.Replace(valid, `"exitCode":0,`, "", 1),
		"failed exit":      strings.Replace(valid, `"exitCode":0`, `"exitCode":1`, 1),
		"error":            strings.Replace(valid, `"text":`, `"error":{},"text":`, 1),
		"no text":          strings.Replace(valid, `"{}"`, `""`, 1),
		"truncated":        valid[:len(valid)-1],
		"multiple":         valid + valid,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalText([]byte(data)); err == nil {
				t.Fatal("accepted failed or incomplete result")
			}
		})
	}
}

func TestBridgeIsolationAndTerminalResult(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is not installed")
	}
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"name":"mastracode","version":"0.39.0"}`)
	write("dist/cli.js", "throw new Error('CLI must not run')")
	write("node_modules/@mastra/code-sdk/package.json", `{"name":"@mastra/code-sdk","version":"1.7.0","type":"module","exports":{".":{"import":{"default":"./index.js"}},"./*":{"import":{"default":"./dist/*.js"}},"./package.json":"./package.json"}}`)
	write("node_modules/@mastra/core/package.json", `{"name":"@mastra/core","version":"1.65.0","type":"module","exports":{"./workspace":{"import":{"default":"./workspace.js"}},"./package.json":"./package.json"}}`)
	write("node_modules/@mastra/memory/package.json", `{"name":"@mastra/memory","version":"1.28.3","type":"module","exports":{".":{"import":{"default":"./index.js"}},"./package.json":"./package.json"}}`)
	write("node_modules/@mastra/memory/index.js", `export class Memory { constructor(config) { this.config=config; } }`)
	write("node_modules/@mastra/core/workspace.js", `
export const WORKSPACE_TOOLS={FILESYSTEM:{READ_FILE:'read',LIST_FILES:'list',GREP:'grep',FILE_STAT:'stat'}};
export class Workspace { constructor(options) { this.options=options; } }
export class LocalFilesystem { constructor(options) { this.options=options; } }
`)
	write("node_modules/@mastra/code-sdk/dist/onboarding/settings.js", `export function loadSettings(){return {models:{activeModelPackId:'native'}}} export function resolveModelDefaults(s,p){return {build:p[0].models.build}}`)
	write("node_modules/@mastra/code-sdk/dist/onboarding/packs.js", `export function getBuiltinModePack(id){return {models:{build:'native/model'}}}`)
	write("node_modules/@mastra/code-sdk/index.js", `
import assert from 'node:assert/strict';
import {readFileSync,realpathSync} from 'node:fs';
export const denyPolicy={deny:true};
export async function createMastraCode(c) {
  assert.equal(c.disableHooks,true); assert.equal(c.disableMcp,true); assert.equal(c.disablePlugins,true);
  assert.equal(c.disableGithubSignals,true); assert.equal(c.disableSettingsOmSeed,true);
  assert.deepEqual(c.memory.config.options,{observationalMemory:false,semanticRecall:false,workingMemory:{enabled:false}});
  assert.deepEqual(c.subagents,[]); assert.deepEqual(c.intervalHandlers,[]);
  assert.equal(c.unixSocketPubSub,false); assert.deepEqual(JSON.parse(readFileSync(c.settingsPath)),{});
  assert.equal(process.cwd(),realpathSync(c.cwd)); assert.equal(c.modes[0].defaultModelId,'native/model');
  assert.notEqual(c.cwd,c.initialState.projectPath);
  assert.equal(c.initialState.untrustedCheckout,true); assert.equal(c.initialState.skipGlobalInstructions,true);
  assert.equal(c.initialState.baseRef,undefined);
  const w=c.workspace.options;
  assert.deepEqual(w.skills,[]); assert.equal(w.lsp,false); assert.equal(w.sandbox,undefined);
  assert.equal(w.filesystem.options.basePath,c.initialState.projectPath);
  assert.equal(w.filesystem.options.readOnly,true); assert.equal(w.filesystem.options.contained,true);
  assert.equal(w.tools.enabled,false); assert.deepEqual(c.modes[0].availableTools,['read','list','grep','stat']);
  assert.equal(process.env.MASTRA_APP_DATA_DIR,'native-auth-sentinel');
  console.log('SDK diagnostic');
  return {controller:{stopIntervals:async()=>{},getMastra:()=>({stopWorkers:async()=>{}})}, session:{thread:{async create(){return {id:"thread"}}},identity:{getResourceId(){return "resource"}}},codeAgent:{async subscribeToThread(){return {async *stream(){},stream:(async function*(){yield {type:"finish",payload:{stepResult:{reason:"stop"}}}})(),unsubscribe(){}}}},stopPluginSignalProviders(){}};
}
export function runMC(options) {
  assert.equal(options.prompt,'selected prompt and skill'); assert.equal(options.policy,denyPolicy);
  assert.ok(options.signal instanceof AbortSignal);
  const text=process.env.MASTRA_TEST_LARGE ? 'x'.repeat(2*1024*1024) : '{}';
  const parts=process.env.MASTRA_TEST_TOOL ? [{type:'text',text},{type:'tool-invocation'}] :
    process.env.MASTRA_TEST_COMPLETED ? [{type:'text',text:'progress'},{type:'tool-invocation',toolInvocation:{state:'result'}},{type:'text',text}] : [{type:'text',text}];
  return {
    async *[Symbol.asyncIterator]() {
      yield {type:'message_end',message:{role:'assistant',content:{parts:[{type:'text',text:'intermediate'}]}}};
      yield {type:'message_end',message:{role:'assistant',content:{parts,metadata:{stopReason:'complete'}}}};
    },
    result:Promise.resolve({status:'completed',finishReason:'complete',exitCode:0,text:'intermediate{}'})
  };
}
`)
	for _, scenario := range []string{"final", "pending", "completed"} {
		cmd, err := command(filepath.Join(root, "dist", "cli.js"), "", root, filepath.Join(root, "checkout"))
		if err != nil {
			t.Fatal(err)
		}
		cmd.Env = append(os.Environ(), "MASTRA_APP_DATA_DIR=native-auth-sentinel")
		if scenario == "pending" {
			cmd.Env = append(cmd.Env, "MASTRA_TEST_TOOL=1")
		}
		if scenario == "completed" {
			cmd.Env = append(cmd.Env, "MASTRA_TEST_COMPLETED=1")
		}
		cmd.Stdin = strings.NewReader("selected prompt and skill")
		data, err := cmd.Output()
		if err != nil {
			t.Fatalf("bridge failed: %v: %s", err, err.(*exec.ExitError).Stderr)
		}
		got, err := finalText(data)
		if scenario == "pending" {
			if err == nil {
				t.Fatal("accepted final tool invocation")
			}
			continue
		}
		if err != nil || string(got) != "{}" {
			t.Fatalf("text=%s err=%v data=%s", got, err, data)
		}
	}
	cmd, err := command(filepath.Join(root, "dist", "cli.js"), "", root, filepath.Join(root, "checkout"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "MASTRA_APP_DATA_DIR=native-auth-sentinel", "MASTRA_TEST_LARGE=1")
	cmd.Stdin = strings.NewReader("selected prompt and skill")
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	text, err := finalText(data)
	if err != nil || len(text) != 2*1024*1024 {
		t.Fatalf("large result length=%d err=%v", len(text), err)
	}
	write("package.json", `{"name":"mastracode","version":"0.40.0"}`)
	cmd, err = command(filepath.Join(root, "dist", "cli.js"), "", root, filepath.Join(root, "checkout"))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(data), "requires exactly mastracode 0.39.0") {
		t.Fatalf("unsupported version result=%s err=%v", data, err)
	}
}

func TestNativeTerminalFixtures(t *testing.T) {
	for _, name := range []string{"stop", "length", "missing"} {
		data, err := os.ReadFile(filepath.Join("testdata", "native-"+name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		text, err := finalText(data)
		if name == "stop" {
			if err != nil || string(text) != "{}" {
				t.Fatalf("native success text=%s err=%v", text, err)
			}
		} else if err == nil {
			t.Fatalf("accepted native %s termination", name)
		}
	}
}
