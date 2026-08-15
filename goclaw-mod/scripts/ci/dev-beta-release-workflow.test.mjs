import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");
const workflow = readFileSync(join(repoRoot, ".github/workflows/dev-beta-release.yaml"), "utf8");

function jobBlock(name) {
  const lines = workflow.split("\n");
  const start = lines.findIndex((line) => line === `  ${name}:`);
  assert.notEqual(start, -1, `missing job ${name}`);

  let end = lines.length;
  for (let index = start + 1; index < lines.length; index += 1) {
    if (/^  [A-Za-z0-9_]+:$/.test(lines[index])) {
      end = index;
      break;
    }
  }
  return lines.slice(start + 1, end).join("\n");
}

test("dev beta workflow does not serialize fast deploy behind full artifact completion", () => {
  assert.doesNotMatch(workflow, /^concurrency:\n/m);

  const publishRelease = jobBlock("publish_release");
  assert.match(publishRelease, /needs: \[beta_version, build_zuey_binary\]/);
  assert.doesNotMatch(publishRelease, /promote_beta_aliases|docker_images|build_remaining_binaries/);

  const deployZuey = jobBlock("deploy_zuey_beta");
  assert.match(deployZuey, /needs: \[beta_version, publish_release\]/);
  assert.doesNotMatch(deployZuey, /promote_beta_aliases|docker_images|complete_release_artifacts/);
});

test("dev beta workflow publishes zuey amd64 asset before completing all release assets", () => {
  const buildZuey = jobBlock("build_zuey_binary");
  assert.match(buildZuey, /GOARCH: amd64/);
  assert.match(buildZuey, /name: binary-linux-amd64/);

  const remainingBinaries = jobBlock("build_remaining_binaries");
  assert.match(remainingBinaries, /goarch: arm64/);
  assert.match(remainingBinaries, /name: binary-\$\{\{ matrix\.goos \}\}-\$\{\{ matrix\.goarch \}\}/);

  const completion = jobBlock("complete_release_artifacts");
  assert.match(completion, /needs: \[beta_version, publish_release, build_zuey_binary, build_remaining_binaries, deploy_zuey_beta\]/);
  assert.match(completion, /pattern: binary-\*/);
  assert.match(completion, /sha256sum goclaw-\*\.tar\.gz > CHECKSUMS\.sha256/);
});

test("dev beta workflow skips stale deploy and alias mutations", () => {
  for (const name of ["deploy_zuey_beta", "promote_beta_aliases"]) {
    const block = jobBlock(name);
    assert.match(block, /id: beta_freshness/);
    assert.match(block, /latest_beta/);
    assert.match(block, /Skipping stale/);
    assert.match(block, /steps\.beta_freshness\.outputs\.current == 'true'/);
  }
});
