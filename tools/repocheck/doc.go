// Package repocheck holds repository-wide checks that run as ordinary Go
// tests under "go test ./...": the Markdown docs must point at things that
// exist, and CI's path filters must not skip a Go test that reads a light
// file. The package has no production code; see docs_test.go for the rules.
package repocheck
