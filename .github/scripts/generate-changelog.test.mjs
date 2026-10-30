import assert from 'node:assert/strict';
import test from 'node:test';

import {
  classifyCommits,
  formatAuthors,
  generateMd,
  parseCoAuthors,
  parseCategory,
  parseGitLog,
} from './generate-changelog.js';

test('parseCategory keeps a breaking build commit and drops a plain one', () => {
  assert.match(parseCategory('build!: ship one amdl binary'), /Breaking/i);
  assert.match(parseCategory('build(docker)!: rename the image'), /Breaking/i);
  assert.equal(parseCategory('build: bump the Go toolchain'), null);
  assert.equal(parseCategory('style: gofmt'), null);
});

test('parseCoAuthors extracts every Co-authored-by trailer', () => {
  const trailers = `Co-authored-by: Bob Example <123+Bob@users.noreply.github.com>
co-authored-by: Codex <noreply@openai.com>
`;

  assert.deepEqual(parseCoAuthors(trailers), [
    { author: 'Bob Example', email: '123+Bob@users.noreply.github.com' },
    { author: 'Codex', email: 'noreply@openai.com' },
  ]);
});

test('parseGitLog keeps the primary author and parsed co-authors', () => {
  const output = [
    '0123456789abcdef',
    'Alice',
    'Alice@users.noreply.github.com',
    'feat: add release notes',
    'Co-authored-by: Bob <Bob@users.noreply.github.com>\n',
    '',
  ].join('\0');

  assert.deepEqual(parseGitLog(output), [{
    hash: '0123456789abcdef',
    author: 'Alice',
    email: 'Alice@users.noreply.github.com',
    message: 'feat: add release notes',
    coAuthors: [{ author: 'Bob', email: 'Bob@users.noreply.github.com' }],
  }]);
});

test('formatAuthors maps known GitHub users and removes duplicate identities', () => {
  assert.equal(formatAuthors([
    { author: 'Alice', email: 'Alice@users.noreply.github.com' },
    { author: 'Alice Duplicate', email: '123+Alice@users.noreply.github.com' },
    { author: 'Bob Example', email: 'bob@example.com' },
    { author: 'Codex', email: 'noreply@openai.com' },
  ]), '@Alice, Bob Example, @codex');
});

test('generated changelog appends the author and all co-authors to each entry', () => {
  const classified = classifyCommits([{
    hash: '0123456789abcdef',
    author: 'Alice',
    email: 'Alice@users.noreply.github.com',
    message: 'feat: add release notes',
    coAuthors: [
      { author: 'Bob', email: 'Bob@users.noreply.github.com' },
      { author: 'Codex', email: 'noreply@openai.com' },
    ],
  }]);

  const markdown = generateMd(classified, 'v1.4.0', null);
  assert.match(markdown, /\* add release notes @Alice, @Bob, @codex/);
  assert.doesNotMatch(markdown, /### 协作者 \| Contributors/);
});

test('breaking commits get their own first section and lose the prefix', () => {
  const classified = classifyCommits([
    { hash: 'a'.repeat(16), author: 'Alice', email: 'Alice@users.noreply.github.com', message: 'feat(api)!: switch errors to an envelope', coAuthors: [] },
    { hash: 'b'.repeat(16), author: 'Alice', email: 'Alice@users.noreply.github.com', message: 'fix(db): keep rows', coAuthors: [] },
  ]);
  const markdown = generateMd(classified, 'v2.0.0', null);
  const breaking = markdown.indexOf('### 破坏性变更 | Breaking');
  assert.ok(breaking >= 0 && breaking < markdown.indexOf('### 修复 | Fix'));
  assert.match(markdown, /\* switch errors to an envelope @Alice/);
});

test('Claude co-authors are credited as @claude whatever the model name', () => {
  assert.equal(formatAuthors([
    { author: 'Claude Opus 5.5', email: 'noreply@anthropic.com' },
    { author: 'Claude', email: 'noreply@anthropic.com' },
  ]), '@claude');
});
