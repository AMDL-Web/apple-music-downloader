// Package temaridiff holds the tests that keep amdl's pure-Go
// internal/fairplay identical to the upstream Temari library it was
// translated from. It is a separate module so the main module never depends
// on Temari or loads its cdylib.
//
// `make temaridiff` (go test ./... in this directory) compares both
// implementations on random samples under random templates and under
// templates that keep the public golden template's state (TEMARIDIFF_SEED
// picks the seed), and checks that internal/fairplay/rounds_gen.go matches a
// fresh translation of the pinned Temari version. TEMARIDIFF_TEMPLATE, a list
// of /key response files separated by the OS path list separator, adds real
// templates; those are key material and stay out of the repository.
package temaridiff
