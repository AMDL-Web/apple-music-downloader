#!/usr/bin/env node
import { execFileSync, execSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const changelogPath = path.join(__dirname, '..', '..', 'CHANGELOG.md');

const BREAKING = '破坏性变更 | Breaking';

const typeMap = {
  feat: '新增 | New',
  fix: '修复 | Fix',
  refactor: '改进 | Improved',
  perf: '改进 | Improved',
  rft: '改进 | Improved',
  docs: '文档 | Docs',
  doc: '文档 | Docs',
  style: '其他 | Other',
  build: '其他 | Other',
  ci: '自动化 | CI',
  test: '其他 | Other',
  chore: '其他 | Other',
};

// Commits without a Conventional Commits prefix are classified by keyword.
const chineseKeywords = {
  '新增': '新增 | New',
  '修复': '修复 | Fix',
  '更新': '改进 | Improved',
  '改进': '改进 | Improved',
  '优化': '改进 | Improved',
  '重构': '改进 | Improved',
  '文档': '文档 | Docs',
};

const IGNORE_PREFIXES = /^(?:build|style|debug)\s*(?:\([^)]*\))*!?:\s*/;

// type, optional (scope), optional "!" marking a breaking change.
const CONVENTIONAL_PREFIX = /^(?<prefix>\w+)(?:\([\w\-]+\))?(?<breaking>!)?:\s*/;

function parseArgs() {
  const args = process.argv.slice(2);
  let tagName = null;
  let latest = null;
  let withHash = false;
  let withCommitizen = false;
  let outputOnly = false;

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--tag' || arg === '-t') {
      tagName = args[++i];
    } else if (arg === '--base' || arg === '--latest' || arg === '-b') {
      latest = args[++i];
    } else if (arg === '-wh' || arg === '--with-hash') {
      withHash = true;
    } else if (arg === '-wc' || arg === '--with-commitizen') {
      withCommitizen = true;
    } else if (arg === '--output-only') {
      outputOnly = true;
    } else if (arg === '--help' || arg === '-h') {
      console.log(`
Usage: node generate-changelog.js [options]

Options:
  --tag, -t <tag>                release tag name
  --base, --latest, -b <tag>     base tag to compare from
  -wh, --with-hash               show commit hashes
  -wc, --with-commitizen         keep the Conventional Commits prefix
  --output-only                  print the changelog only (for CI)
  -h, --help                     show this help
      `);
      process.exit(0);
    }
  }

  return { tagName, latest, withHash, withCommitizen, outputOnly };
}

function callCommand(command) {
  try {
    return execSync(command, { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
  } catch (e) {
    try {
      return execSync(command, { encoding: 'gbk', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
    } catch {
      return '';
    }
  }
}

function callGit(args, trim = true) {
  try {
    const output = execFileSync('git', args, {
      encoding: 'utf-8',
      stdio: ['pipe', 'pipe', 'pipe'],
      maxBuffer: 10 * 1024 * 1024,
    });
    return trim ? output.trim() : output;
  } catch {
    return '';
  }
}

function getLatestTag() {
  return callGit(['describe', '--tags', '--match', 'v*', '--abbrev=0']) || null;
}

function getCurrentTag() {
  return callGit(['describe', '--tags', '--match', 'v*']) || null;
}

function parseCategory(message) {
  if (IGNORE_PREFIXES.test(message)) {
    return null;
  }

  const m = message.match(CONVENTIONAL_PREFIX);
  if (m) {
    if (m.groups.breaking) return BREAKING;
    const prefix = m.groups.prefix.toLowerCase();
    return typeMap[prefix] || '其他 | Other';
  }

  for (const [keyword, category] of Object.entries(chineseKeywords)) {
    if (message.includes(keyword)) {
      return category;
    }
  }

  return '其他 | Other';
}

function parseCoAuthors(trailers = '') {
  const coAuthors = [];
  const trailerPattern = /^Co-authored-by:\s*(.+?)\s*<([^<>]+)>\s*$/gim;

  for (const match of trailers.matchAll(trailerPattern)) {
    coAuthors.push({
      author: match[1].trim(),
      email: match[2].trim(),
    });
  }

  return coAuthors;
}

function parseGitLog(output) {
  if (!output) return [];

  const commits = [];
  const fields = output.split('\0');
  if (fields.at(-1) === '') fields.pop();

  for (let i = 0; i + 4 < fields.length; i += 5) {
    commits.push({
      hash: fields[i],
      author: fields[i + 1],
      email: fields[i + 2],
      message: fields[i + 3],
      coAuthors: parseCoAuthors(fields[i + 4]),
    });
  }

  return commits;
}

function getCommits(latest = null) {
  const args = [
    'log',
    '--no-show-signature',
    '-z',
    '--format=%H%x00%aN%x00%aE%x00%s%x00%(trailers:key=Co-authored-by)',
  ];

  if (latest) {
    args.push(`${latest}..HEAD`);
  } else {
    args.push('-n', '50', 'HEAD');
  }

  return parseGitLog(callGit(args, false));
}

// Turns a git identity into a changelog credit. "@" is added only when the
// identity maps reliably to a GitHub account: prefixing a display name such as
// "Yangjunwei Liang" would make GitHub mention the unrelated account
// @Yangjunwei. The agent mappings follow AGENTS.md "Commits and releases".
function formatAuthor(author, email) {
  const mail = (email || '').toLowerCase();
  if (author.toLowerCase() === 'codex' && mail === 'noreply@openai.com') {
    return '@codex';
  }
  // Any Claude model name ("Claude Opus 5.5", "Claude") credits @claude.
  if (/^claude\b/i.test(author) && mail === 'noreply@anthropic.com') {
    return '@claude';
  }

  // GitHub noreply addresses carry the login: "123+LOGIN@users.noreply.github.com"
  // for web-flow commits, or the older "LOGIN@users.noreply.github.com".
  const noreply = /^(?:\d+\+)?([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)@users\.noreply\.github\.com$/.exec(email || '');
  if (noreply) {
    return `@${noreply[1]}`;
  }
  // A name shaped like a GitHub login, from a non-bot address, is assumed to
  // be that login.
  if (/^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/.test(author) && !/^no-?reply@/i.test(email || '')) {
    return `@${author}`;
  }
  return author;
}

function formatAuthors(authors = []) {
  const formattedAuthors = [];
  const seen = new Set();

  for (const identity of authors) {
    if (!identity?.author || identity.author === 'web-flow') continue;

    const formatted = formatAuthor(identity.author, identity.email);
    const key = formatted.toLowerCase();
    if (seen.has(key)) continue;

    seen.add(key);
    formattedAuthors.push(formatted);
  }

  return formattedAuthors.join(', ');
}

function classifyCommits(commits, withCommitizen = false) {
  const result = {
    [BREAKING]: [],
    '新增 | New': [],
    '修复 | Fix': [],
    '改进 | Improved': [],
    '文档 | Docs': [],
    '其他 | Other': [],
    '自动化 | CI': [],
  };

  const contributors = new Set();

  for (const commit of commits) {
    if (commit.message.includes('[skip changelog]')) continue;
    
    // Version-bump commits made by CI are not release content.
    if (commit.message.match(/^chore:\s*update\s+version\s+to\s+.+/i)) continue;

    const category = parseCategory(commit.message);
    if (!category) continue;

    let message = commit.message;

    if (!withCommitizen) {
      message = message.replace(CONVENTIONAL_PREFIX, '');
    }

    const authors = [
      { author: commit.author, email: commit.email },
      ...(commit.coAuthors || []),
    ];

    for (const author of authors) {
      if (author.author && author.author !== 'web-flow') {
        contributors.add(formatAuthor(author.author, author.email));
      }
    }

    result[category].push({
      message,
      authors,
      hash: commit.hash.slice(0, 8),
    });
  }

  return { categories: result, contributors: Array.from(contributors) };
}

function getGitHubRepoUrl() {
  try {
    const remoteUrl = callCommand('git remote get-url origin');
    if (!remoteUrl) return null;
    
    // git@github.com:owner/repo.git or https://github.com/owner/repo.git
    let repoPath = remoteUrl;
    if (repoPath.startsWith('git@')) {
      repoPath = repoPath.replace('git@github.com:', 'https://github.com/');
    }
    repoPath = repoPath.replace('.git', '');
    return repoPath;
  } catch {
    return null;
  }
}

function generateMd(classifiedData, tagName, latest, withHash = false) {
  const { categories } = classifiedData;
  const now = new Date().toLocaleDateString('zh-CN');
  const lines = [];
  const repoUrl = getGitHubRepoUrl();

  if (tagName) {
    lines.push(`## ${tagName}`);
  } else {
    lines.push(`## 📝 更新日志 (${now})`);
  }

  if (latest && repoUrl) {
    const endTag = tagName || 'HEAD';
    lines.push(`> [${latest}...${endTag}](${repoUrl}/compare/${latest}...${endTag})`);
  } else if (latest) {
    lines.push(`> ${latest} ... HEAD`);
  }
  lines.push('');

  const order = [BREAKING, '新增 | New', '修复 | Fix', '改进 | Improved', '文档 | Docs', '自动化 | CI', '其他 | Other'];
  for (const category of order) {
    if (categories[category].length === 0) continue;

    lines.push(`### ${category}`);
    lines.push('');

    for (const item of categories[category]) {
      let line = `* ${item.message}`;
      /*if (withHash && repoUrl) {
        line += ` ([${item.hash}](${repoUrl}/commit/${item.hash}))`;
      } else if (withHash) {
        line += ` (${item.hash})`;
      }*/
      const authors = formatAuthors(item.authors);
      if (authors) {
        line += ` ${authors}`;
      }
      lines.push(line);
    }

    lines.push('');
  }

  return lines.join('\n');
}

function writeToFile(content, append = false) {
  if (append) {
    fs.appendFileSync(changelogPath, '\n' + content, 'utf8');
  } else {
    fs.writeFileSync(changelogPath, content, 'utf8');
  }
}

function main() {
  const { tagName, latest, withHash, withCommitizen, outputOnly } = parseArgs();

  const resolvedLatest = latest || getLatestTag();
  const resolvedTagName = tagName || getCurrentTag();

  if (!outputOnly) {
    console.log('📊 Generating changelog...');
    if (resolvedLatest) {
      console.log(`📌 From: ${resolvedLatest}`);
    }
    if (resolvedTagName) {
      console.log(`🏷️  To: ${resolvedTagName}`);
    }
    console.log('');
  }

  const commits = getCommits(resolvedLatest);
  if (commits.length === 0) {
    if (!outputOnly) {
      console.log('⚠️  No commits found');
    }
    return;
  }

  const grouped = classifyCommits(commits, withCommitizen);
  const markdown = generateMd(grouped, resolvedTagName, resolvedLatest, withHash);

  writeToFile(markdown, !!resolvedLatest);

  if (outputOnly) {
    console.log(markdown);
  } else {
    console.log('✅ Changelog updated: CHANGELOG.md');
    console.log('\n' + markdown);
  }
}

const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) main();

export {
  classifyCommits,
  formatAuthor,
  formatAuthors,
  generateMd,
  parseCoAuthors,
  parseGitLog,
};
