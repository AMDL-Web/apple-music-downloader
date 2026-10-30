// Package repocheck holds repository-wide checks that run as ordinary Go
// tests under "go test ./...": the Markdown docs must point at things that
// exist, and the agent instruction files must keep their agreed shape. The
// package has no production code; see docs_test.go for the rules.
package repocheck
