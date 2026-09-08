package board

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Parenthesized literal chunks preserve paths across display line breaks.
// PowerShell uses doubled apostrophes inside a single-quoted string.
func powershellInvocation(args []string, width int) []string {
	var lines []string
	for i, arg := range args {
		opening := "("
		if i == 0 {
			opening = "&("
		}
		lines = append(lines, opening)
		runes := []rune(arg)
		for len(runes) > 0 {
			n := 0
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
			for n < len(runes) && ansi.StringWidth(quote(string(runes[:n+1]))) <= width-2 {
				n++
			}
			line := quote(string(runes[:n]))
			runes = runes[n:]
			if len(runes) > 0 {
				line += " +"
			}
			lines = append(lines, line)
		}
		closing := ")"
		if i < len(args)-1 {
			closing += " `"
		}
		lines = append(lines, closing)
	}
	return lines
}

func (command monitorInvocation) powershellLines(width int) []string {
	lines := powershellInvocation([]string{"Set-Item", "Env:HERDR_PLUGIN_STATE_DIR", command.state}, width)
	return append(lines, powershellInvocation([]string{command.binary, "--monitor", "--config", command.path}, width)...)
}
