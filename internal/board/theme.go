package board

import "github.com/charmbracelet/lipgloss"

// The board palette. Every color the board draws comes from one of these
// roles, so styles name what a color means instead of which color it is.
// The values are ANSI 256 indexes. The board paints no background of its
// own, so the terminal background shows through.
var (
	textColor       = lipgloss.Color("252") // body text, column headers, keys
	brightColor     = lipgloss.Color("255") // text on the selection and accent fills
	subtleColor     = lipgloss.Color("246") // secondary text: inactive tabs, review details
	mutedColor      = lipgloss.Color("242") // hints and absent values
	borderColor     = lipgloss.Color("238") // rules around the table
	selectionColor  = lipgloss.Color("237") // selected row fill
	accentColor     = lipgloss.Color("105") // selected row edge
	accentFillColor = lipgloss.Color("62")  // active tab fill
	highlightColor  = lipgloss.Color("212") // board title
	linkColor       = lipgloss.Color("81")  // the selected PR URL
	okColor         = lipgloss.Color("42")
	warnColor       = lipgloss.Color("214")
	errorColor      = lipgloss.Color("210")
	// cautionColor marks P2 findings, between the P1 warning and the P3
	// subtle text.
	cautionColor = lipgloss.Color("179")
)
