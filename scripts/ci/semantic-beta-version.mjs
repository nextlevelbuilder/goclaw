#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { writeFileSync, appendFileSync } from "node:fs";

const prerelease = process.env.PRERELEASE_ID || "beta";
const initialVersion = process.env.INITIAL_VERSION || "0.1.0";

function git(args) {
  return execFileSync("git", args, { encoding: "utf8" }).trim();
}

function setOutput(name, value) {
  const output = process.env.GITHUB_OUTPUT;
  if (output) {
    appendFileSync(output, `${name}=${value}\n`);
    return;
  }
  console.log(`${name}=${value}`);
}

function parseVersion(value) {
  const match = /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z-]+)\.(\d+))?$/.exec(value);
  if (!match) return null;
  return {
    raw: value,
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    preid: match[4] || "",
    prenumber: match[5] ? Number(match[5]) : 0,
  };
}

function tagCommit(tag) {
  return git(["rev-list", "-n", "1", tag]);
}

function compareBase(a, b) {
  return a.major - b.major || a.minor - b.minor || a.patch - b.patch;
}

function compareVersion(a, b) {
  const base = compareBase(a, b);
  if (base !== 0) return base;
  if (!a.preid && b.preid) return 1;
  if (a.preid && !b.preid) return -1;
  if (a.preid !== b.preid) return a.preid.localeCompare(b.preid);
  return a.prenumber - b.prenumber;
}

function bump(base, level) {
  if (level === "major") return { major: base.major + 1, minor: 0, patch: 0 };
  if (level === "minor") return { major: base.major, minor: base.minor + 1, patch: 0 };
  return { major: base.major, minor: base.minor, patch: base.patch + 1 };
}

function versionText(version) {
  return `${version.major}.${version.minor}.${version.patch}`;
}

function commitLevel(message) {
  const header = message.split(/\r?\n/, 1)[0] || "";
  if (/^[a-zA-Z]+(?:\([^)]+\))?!:/.test(header) || /\nBREAKING[ -]CHANGE:/.test(message)) {
    return "major";
  }
  if (/^feat(?:\([^)]+\))?:/.test(header)) return "minor";
  if (/^(fix|perf)(?:\([^)]+\))?:/.test(header) || /^revert:/.test(header)) return "patch";
  return "";
}

function maxBase(a, b) {
  return compareBase(a, b) >= 0 ? a : b;
}

function writeNoRelease(reason) {
  setOutput("released", "false");
  setOutput("version", "");
  setOutput("tag", "");
  console.log(reason);
}

const tags = git(["tag", "--merged", "HEAD", "--list", "v[0-9]*"])
  .split(/\r?\n/)
  .filter(Boolean)
  .map(parseVersion)
  .filter(Boolean)
  .map((tag) => ({ ...tag, commit: tagCommit(tag.raw) }))
  .sort(compareVersion)
  .reverse();

const latest = tags[0];
const latestStable = tags.find((tag) => !tag.preid);
const latestPrerelease = tags.find((tag) => tag.preid === prerelease);
const head = git(["rev-parse", "HEAD"]);
const previousTag = tags.find((tag) => tag.raw !== latest?.raw);
const repairTag = latest?.preid === prerelease && latest.commit === head ? latest : null;
const range = latest ? [`${latest.raw}..HEAD`] : [];
const logRange = repairTag && previousTag ? [`${previousTag.raw}..HEAD`] : range;
const log = git(["log", "--format=%B%x1e", ...logRange]);
const messages = log.split("\x1e").map((message) => message.trim()).filter(Boolean);

if (latest?.preid && latest.preid !== prerelease) {
  writeNoRelease(`Latest prerelease tag ${latest.raw} is not a ${prerelease} release; skipping ${prerelease} automation.`);
  process.exit(0);
}

if (repairTag) {
  const version = repairTag.raw.replace(/^v/, "");
  const releaseNotes = [
    `## ${repairTag.raw}`,
    "",
    `Automated ${prerelease} release from ${head}.`,
    "",
    "### Changes",
    "",
    ...(messages.length ? messages.map((message) => `- ${message.split(/\r?\n/, 1)[0]}`) : ["- Repair release assets for this tag."]),
    "",
  ].join("\n");

  writeFileSync("release-notes.md", releaseNotes);
  setOutput("released", "true");
  setOutput("version", version);
  setOutput("tag", repairTag.raw);
  setOutput("notes_path", "release-notes.md");
  console.log(`Repairing ${prerelease} release: ${repairTag.raw}`);
  process.exit(0);
}

let level = "";
for (const message of messages) {
  const next = commitLevel(message);
  if (next === "major") {
    level = "major";
    break;
  }
  if (next === "minor" && level !== "major") level = "minor";
  if (next === "patch" && !level) level = "patch";
}

if (!level) {
  writeNoRelease("No release-worthy conventional commits found since the last release tag.");
  process.exit(0);
}

const zero = { major: 0, minor: 0, patch: 0 };
const initial = parseVersion(initialVersion) || { major: 0, minor: 1, patch: 0 };
const stableBase = latestStable || initial || zero;
const bumpedBase = bump(stableBase, level);
const prereleaseBase = latestPrerelease || zero;
let targetBase = maxBase(bumpedBase, prereleaseBase);
if (!latest && compareBase(targetBase, initial) < 0) targetBase = initial;

const nextNumber = compareBase(targetBase, prereleaseBase) === 0
  ? latestPrerelease.prenumber + 1
  : 1;
const version = `${versionText(targetBase)}-${prerelease}.${nextNumber}`;
const tag = `v${version}`;

if (tags.some((existing) => existing.raw === tag)) {
  writeNoRelease(`Computed tag ${tag} already exists.`);
  process.exit(0);
}

const releaseNotes = [
  `## ${tag}`,
  "",
  `Automated ${prerelease} release from ${process.env.GITHUB_SHA || git(["rev-parse", "HEAD"])}.`,
  "",
  "### Changes",
  "",
  ...messages.map((message) => `- ${message.split(/\r?\n/, 1)[0]}`),
  "",
].join("\n");

writeFileSync("release-notes.md", releaseNotes);
setOutput("released", "true");
setOutput("version", version);
setOutput("tag", tag);
setOutput("notes_path", "release-notes.md");
console.log(`Next ${prerelease} release: ${tag}`);
