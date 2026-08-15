import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");
const scriptPath = join(repoRoot, "scripts/ci/semantic-beta-version.mjs");

function run(cwd, args, env = {}) {
  return execFileSync(args[0], args.slice(1), {
    cwd,
    encoding: "utf8",
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function git(cwd, ...args) {
  return run(cwd, ["git", ...args]);
}

function writeAndCommit(cwd, name, message) {
  writeFileSync(join(cwd, name), `${message}\n`);
  git(cwd, "add", name);
  git(cwd, "commit", "-m", message);
}

function tag(cwd, name) {
  git(cwd, "tag", "-a", name, "-m", `Release ${name}`);
}

function createRepo() {
  const cwd = mkdtempSync(join(tmpdir(), "goclaw-beta-version-"));
  mkdirSync(join(cwd, "scripts/ci"), { recursive: true });
  copyFileSync(scriptPath, join(cwd, "scripts/ci/semantic-beta-version.mjs"));
  git(cwd, "init", "-b", "main");
  git(cwd, "config", "user.name", "Version Test");
  git(cwd, "config", "user.email", "version-test@example.com");

  writeAndCommit(cwd, "base.txt", "fix(base): initial stable");
  tag(cwd, "v3.11.3");

  git(cwd, "checkout", "-b", "dev");
  writeAndCommit(cwd, "old-beta.txt", "fix(runtime): old beta fix");
  tag(cwd, "v3.12.0-beta.41");

  git(cwd, "checkout", "main");
  writeAndCommit(cwd, "stable.txt", "feat(release): stable milestone");
  tag(cwd, "v3.12.0");

  git(cwd, "checkout", "dev");
  writeAndCommit(cwd, "current.txt", "feat(api): new work after stable");
  return cwd;
}

function runVersionScript(cwd) {
  return run(cwd, ["node", "scripts/ci/semantic-beta-version.mjs"], {
    GITHUB_OUTPUT: "",
    INITIAL_VERSION: "3.11.3",
    PRERELEASE_ID: "beta",
  });
}

test("bumps beta above latest stable even when stable tag is not merged into dev", () => {
  const cwd = createRepo();
  try {
    const output = runVersionScript(cwd);
    assert.match(output, /version=3\.13\.0-beta\.1/);
    assert.match(output, /tag=v3\.13\.0-beta\.1/);
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});

test("continues prerelease numbering when upstream already has the next beta base", () => {
  const cwd = createRepo();
  try {
    git(cwd, "checkout", "-b", "upstream-dev");
    writeAndCommit(cwd, "upstream.txt", "fix(vault): upstream beta work");
    tag(cwd, "v3.13.0-beta.2");
    git(cwd, "checkout", "dev");

    const output = runVersionScript(cwd);
    assert.match(output, /version=3\.13\.0-beta\.3/);
    assert.match(output, /tag=v3\.13\.0-beta\.3/);
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});

test("dev beta workflow pushes missing origin tags even if a fetched tag exists locally", () => {
  const workflow = readFileSync(join(repoRoot, ".github/workflows/dev-beta-release.yaml"), "utf8");
  assert.match(workflow, /git rev-parse "\$TAG"/);
  assert.match(workflow, /git ls-remote --exit-code --tags origin "refs\/tags\/\$TAG"/);
  assert.match(workflow, /git push origin "\$TAG"/);
});
