// Package repocheck holds repository-wide checks that run as ordinary Go
// tests under "go test ./...": tests that police the repository's files
// rather than how the product behaves. The package has no production code;
// the checks are grouped by subject:
//
//   - docs_test.go: the Markdown docs point at things that exist and keep
//     their types and index.
//   - console_test.go: the web console's static files (the vendored bundle's
//     checksum, references and imports that resolve, the design guard, the
//     CSP-safe index, the stylesheet order and skin tokens) and the values
//     they share with the Go code (log components, progress weights).
//   - config_test.go: configs/config.example.yaml documents every key with
//     config.Default's values.
//   - api_test.go: docs/api.md lists every event type, and the
//     SubmitErrorCode enum of openapi.yaml lists the codes the source produces.
//   - deploy_test.go: deploy/docker-compose.yml gives the backend time to
//     shut down.
//   - ci_paths_test.go: CI's path filters run every Go test that can see a
//     change, this package's included.
package repocheck
