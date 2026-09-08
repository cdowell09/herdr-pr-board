//go:build !windows

package codexadapter

import "fmt"

func runCancellationAgent(string) error {
	return fmt.Errorf("native Windows cancellation fixture called on Unix")
}
