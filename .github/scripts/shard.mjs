// Cuts a package's tests into slices for parallel CI jobs. It reads test names,
// one per line, from standard input and prints the -run pattern that selects
// the k-th of n slices: every n-th name, so slow tests that sit together in a
// file are spread. Every name lands in exactly one slice. A bad argument or an
// empty slice is an error, never a pattern that selects everything or nothing.
//
//   go test -list . ./pkg | node .github/scripts/shard.mjs 2/3
import { fileURLToPath } from 'node:url';

// parseShard reads "k/n" with 1 <= k <= n.
export function parseShard(spec) {
  const m = /^([1-9][0-9]*)\/([1-9][0-9]*)$/.exec(spec ?? '');
  if (!m || Number(m[1]) > Number(m[2])) throw new Error(`shard must be k/n with 1 <= k <= n, not ${JSON.stringify(spec)}`);
  return { k: Number(m[1]), n: Number(m[2]) };
}

// slice returns the names of slice spec among names, in order. Names that are
// not Go test names are ignored (go test -list also prints "ok pkg 0.1s").
export function slice(names, spec) {
  const { k, n } = parseShard(spec);
  const tests = names.map((s) => s.trim()).filter((s) => /^(Test|Fuzz|Example)\w*$/.test(s));
  return tests.filter((_, i) => i % n === k - 1);
}

// pattern anchors the names so that Test1 does not also select Test10.
export function pattern(names) {
  if (names.length === 0) throw new Error('this slice has no tests');
  return `^(${names.join('|')})$`;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    let input = '';
    for await (const chunk of process.stdin) input += chunk;
    process.stdout.write(pattern(slice(input.split('\n'), process.argv[2])) + '\n');
  } catch (e) {
    console.error(`shard: ${e.message}`);
    process.exit(2);
  }
}
