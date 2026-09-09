package cursoradapter

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFinalTextRequiresTerminalSuccess(t *testing.T) {
	valid := `{"type":"result","status":"finished","turnEnded":1,"pending":0,"cancelled":false,"text":"{}"}`
	if text, err := finalText([]byte(valid)); err != nil || string(text) != "{}" {
		t.Fatalf("text=%s err=%v", text, err)
	}
	for name, data := range map[string]string{
		"running":      strings.Replace(valid, "finished", "running", 1),
		"error status": strings.Replace(valid, "finished", "error", 1),
		"cancelled":    strings.Replace(valid, "false", "true", 1),
		"no turn end":  strings.Replace(valid, `"turnEnded":1`, `"turnEnded":0`, 1),
		"duplicate":    strings.Replace(valid, `"turnEnded":1`, `"turnEnded":2`, 1),
		"missing end":  strings.Replace(valid, `"turnEnded":1,`, "", 1),
		"pending tool": strings.Replace(valid, `"pending":0`, `"pending":1`, 1),
		"missing tool": strings.Replace(valid, `"pending":0,`, "", 1),
		"error":        strings.Replace(valid, `"text":`, `"error":{},"text":`, 1),
		"empty":        strings.Replace(valid, `"{}"`, `""`, 1),
		"truncated":    valid[:len(valid)-1],
		"multiple":     valid + valid,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalText([]byte(data)); err == nil {
				t.Fatal("accepted incomplete or failed result")
			}
		})
	}
}

func TestNativeTerminalFixtures(t *testing.T) {
	for _, name := range []string{"tools", "pending", "missing", "error", "error_after", "cancel", "stored"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "native-"+name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			text, err := finalText(data)
			if name == "tools" || name == "stored" {
				if err != nil || string(text) != "{}" {
					t.Fatalf("native success text=%s err=%v", text, err)
				}
			} else if err == nil {
				t.Fatalf("accepted native %s failure", name)
			}
		})
	}
}

func TestBridgePreservesNativeAuthAndIsolatesReview(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is not installed")
	}
	root := t.TempDir()
	packageDir := filepath.Join(root, "node_modules", "@cursor", "sdk")
	entry := filepath.Join(packageDir, "dist", "esm", "index.js")
	if err := os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(packageDir, "package.json")
	if err := os.WriteFile(manifest, []byte(`{"name":"@cursor/sdk","version":"1.0.31","type":"module"}`), 0600); err != nil {
		t.Fatal(err)
	}
	const sdk = `
import assert from 'node:assert/strict';
import {readFileSync, realpathSync, writeFileSync} from 'node:fs';
import {join} from 'node:path';
export class JsonlLocalAgentStore { constructor(path) { this.path=path; } }
export const Agent={async create(options) {
  const [entry,work,checkout]=process.argv.slice(2);
  assert.equal(process.cwd(),realpathSync(work));
  assert.equal(options.local.cwd,checkout);
  assert.deepEqual(options.local.settingSources,[]);
  assert.deepEqual(options.local.sandboxOptions,{enabled:false});
  assert.equal(options.local.enableAgentRetries,false);
  assert.equal(options.local.store.path,join(work,'store'));
  assert.deepEqual(options.tools,['read','ls','grep','glob']);
  assert.deepEqual(options.mcpServers,{}); assert.deepEqual(options.agents,{});
  assert.equal(options.apiKey,undefined);
  assert.equal(options.model.id,process.env.CURSOR_MODEL || 'composer-2.5');
  assert.equal(process.env.CURSOR_API_KEY,'generated-auth-sentinel');
  assert.equal(process.env.NODE_OPTIONS,undefined); assert.equal(process.env.NODE_PATH,undefined);
  console.log('native SDK diagnostic');
  return {
    async send(prompt,{onDelta}) {
      assert.equal(prompt,'selected prompt and skill');
      const scenario=process.env.CURSOR_TEST_SCENARIO;
      if (scenario==='tools' || scenario==='pending') onDelta({update:{type:'tool-call-started',callId:'read'}});
      if (scenario==='tools') onDelta({update:{type:'tool-call-completed',callId:'read'}});
      if (scenario!=='missing') onDelta({update:{type:'turn-ended'}});
      return {async wait(){return {status:'finished',result:scenario==='large'?'x'.repeat(2*1024*1024):'{}'}}};
    },
    async [Symbol.asyncDispose]() {writeFileSync(join(work,'disposed'),'yes');}
  };
}};
`
	if err := os.WriteFile(entry, []byte(sdk), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURSOR_API_KEY", "generated-auth-sentinel")
	t.Setenv("NODE_OPTIONS", "--require=/unselected/module.js")
	t.Setenv("NODE_PATH", "/unselected/modules")
	for _, scenario := range []string{"stop", "tools", "pending", "missing", "large", "global"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("CURSOR_TEST_SCENARIO", scenario)
			if scenario == "tools" {
				t.Setenv("CURSOR_MODEL", "selected-model")
			}
			work := t.TempDir()
			sdkEntry := entry
			if scenario == "global" {
				bin := t.TempDir()
				script := "#!/usr/bin/env node\nconst assert=require('node:assert/strict'); assert.deepEqual(process.argv.slice(2),['root','-g']); console.log(process.env.CURSOR_TEST_GLOBAL_ROOT);\n"
				if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS == "windows" {
					if err := os.WriteFile(filepath.Join(bin, "npm.cmd"), []byte("@echo off\r\nnode \"%~dp0npm\" %*\r\n"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("CURSOR_TEST_GLOBAL_ROOT", filepath.Join(root, "node_modules"))
				sdkEntry = "cursor"
			}
			cmd, err := command(sdkEntry, "", work, filepath.Join(work, "checkout"))
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stdin = strings.NewReader("selected prompt and skill")
			data, err := cmd.Output()
			if err != nil {
				t.Fatalf("bridge failed: %v: %s", err, err.(*exec.ExitError).Stderr)
			}
			if data, err := os.ReadFile(filepath.Join(work, "disposed")); err != nil || string(data) != "yes" {
				t.Fatalf("SDK not disposed: %s %v", data, err)
			}
			text, err := finalText(data)
			if scenario == "pending" || scenario == "missing" {
				if err == nil {
					t.Fatal("accepted incomplete SDK turn")
				}
				return
			}
			if scenario == "large" {
				if err != nil || len(text) != 2*1024*1024 {
					t.Fatalf("large result bytes=%d err=%v", len(text), err)
				}
			} else if err != nil || string(text) != "{}" {
				t.Fatalf("text=%s err=%v", text, err)
			}
		})
	}
	if err := os.WriteFile(manifest, []byte(`{"name":"@cursor/sdk","version":"1.0.32"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd, err := command(entry, "", t.TempDir(), "checkout")
	if err != nil {
		t.Fatal(err)
	}
	data, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(data), "requires @cursor/sdk 1.0.31") {
		t.Fatalf("unsupported version: %s %v", data, err)
	}
}
