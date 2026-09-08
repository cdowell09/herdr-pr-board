package cli

import (
	"fmt"
	"strings"
)

// npmNodeTarget recognizes npm's argument-free Node launcher. Other batch
// programs cannot be translated without executing a command shell.
func npmNodeTarget(data []byte) (string, error) {
	const prefix = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0

IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
)

endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & set PATHEXT=%PATHEXT:;.JS;=;% & "%_prog%"  "%dp0%\`
	const suffix = "\" %*\n"
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(content, prefix) || !strings.HasSuffix(content, suffix) {
		return "", fmt.Errorf("unsupported batch launcher; configure a native executable or reinstall the CLI with npm")
	}
	target := strings.TrimSuffix(strings.TrimPrefix(content, prefix), suffix)
	if !strings.HasPrefix(target, `node_modules\`) || strings.ContainsAny(target, "\"%\r\n:/*?<>|") {
		return "", fmt.Errorf("unsupported npm Node target")
	}
	for _, part := range strings.Split(target, `\`) {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("unsupported npm Node target")
		}
	}
	return target, nil
}
