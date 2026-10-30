// Package skill holds the agent skill for amdl: when to use the command line
// or the MCP server, the download flow, the output and exit codes, and the
// rules an agent keeps. amdl cli setup prints it; the MCP server's
// instructions repeat its rules word for word (a test in mcpserver checks).
package skill

import _ "embed"

// Text is SKILL.md.
//
//go:embed SKILL.md
var Text string
