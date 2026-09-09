//go:build !windows

package copilotadapter

func checkPolicy() error { return checkPolicyDirectory("/etc/github-copilot/policy.d") }
