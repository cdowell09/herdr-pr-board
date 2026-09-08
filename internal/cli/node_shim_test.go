package cli

import (
	"strings"
	"testing"
)

const nodeShimFixture = "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n\r\nIF EXIST \"%dp0%\\node.exe\" (\r\n  SET \"_prog=%dp0%\\node.exe\"\r\n) ELSE (\r\n  SET \"_prog=node\"\r\n)\r\n\r\nendLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & set PATHEXT=%PATHEXT:;.JS;=;% & \"%_prog%\"  \"%dp0%\\node_modules\\@openai\\codex\\bin\\codex.js\" %*\r\n"

func TestNpmNodeTarget(t *testing.T) {
	for _, content := range []string{nodeShimFixture, strings.ReplaceAll(nodeShimFixture, "\r\n", "\n")} {
		got, err := npmNodeTarget([]byte(content))
		if err != nil || got != `node_modules\@openai\codex\bin\codex.js` {
			t.Fatalf("target=%q err=%v", got, err)
		}
	}
	for name, content := range map[string]string{
		"extra command":     nodeShimFixture + "echo unsafe\r\n",
		"node options":      strings.Replace(nodeShimFixture, `"%_prog%"  `, `"%_prog%" --require injected `, 1),
		"variable":          strings.Replace(nodeShimFixture, "codex.js", "%INJECT%.js", 1),
		"traversal":         strings.Replace(nodeShimFixture, `@openai\codex`, `..\codex`, 1),
		"alternate runtime": strings.ReplaceAll(nodeShimFixture, "node.exe", "other.exe"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := npmNodeTarget([]byte(content)); err == nil {
				t.Fatal("accepted unsupported launcher")
			}
		})
	}
}
