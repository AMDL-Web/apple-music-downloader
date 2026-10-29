// Package fairplay decrypts Apple Music FairPlay SAMPLE-AES samples with the
// white-box template that wrapper-lite's /key endpoint returns.
//
// It is a pure-Go translation of Temari v0.5.1
// (https://github.com/WorldObservationLog/Temari, tag bindings/go/v0.5.1,
// commit 85c6762). rounds_gen.go is translated mechanically from
// src/rounds_gen.rs by tools/temaridiff/cmd/transpile; template.go,
// decrypt.go and rounds.go are hand ports of src/template.rs, src/rounds.rs
// and the unsafe helpers in src/rounds_gen.rs. tools/temaridiff checks the
// result byte for byte against the upstream library.
//
// Temari is distributed under the MIT License:
//
//	MIT License
//
//	Copyright (c) 2025 Temari contributors
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE.
package fairplay
