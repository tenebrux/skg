#!/usr/bin/env node

import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const semver = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.([1-9]\d*))?$/;

async function json(relative) {
  return JSON.parse(await readFile(path.join(repo, relative), "utf8"));
}

function assertVersion(version, label) {
  const match = semver.exec(version);
  if (!match) throw new Error(`${label}: expected MAJOR.MINOR.PATCH or MAJOR.MINOR.PATCH-rc.N, got ${JSON.stringify(version)}`);
  return { major: Number(match[1]), prerelease: match[4] !== undefined };
}

function checkModuleMajor(version, modulePath) {
  const { major } = assertVersion(version, "release version");
  const suffix = /\/v(\d+)$/.exec(modulePath);
  if (major >= 2 && Number(suffix?.[1]) !== major) {
    throw new Error(`go/go.mod module ${JSON.stringify(modulePath)} must end in /v${major} for ${version}`);
  }
  if (major < 2 && suffix) throw new Error(`go/go.mod must not have a major suffix for ${version}`);
}

async function checkWorkflowPins() {
  const workflowDir = path.join(repo, ".github", "workflows");
  for (const name of await readdir(workflowDir)) {
    if (!name.endsWith(".yml") && !name.endsWith(".yaml")) continue;
    const source = await readFile(path.join(workflowDir, name), "utf8");
    for (const match of source.matchAll(/\buses:\s*([^\s@]+)@([^\s#]+)/g)) {
      const [, action, reference] = match;
      if (!/^[0-9a-f]{40}$/.test(reference)) {
        throw new Error(`${name}: ${action}@${reference} is not pinned to a full commit SHA`);
      }
    }
  }
}

async function main() {
  const zon = await readFile(path.join(repo, "build.zig.zon"), "utf8");
  const zonVersion = /\.version\s*=\s*"([^"]+)"/.exec(zon)?.[1];
  if (!zonVersion) throw new Error("build.zig.zon has no version");
  const zonSemver = assertVersion(zonVersion, "build.zig.zon");

  const vscode = await json("tools/vscode-skg/package.json");
  const vscodeLock = await json("tools/vscode-skg/package-lock.json");
  const tree = await json("tools/tree-sitter-skg/package.json");
  const treeLock = await json("tools/tree-sitter-skg/package-lock.json");
  for (const [label, pkg, lock] of [
    ["tools/vscode-skg", vscode, vscodeLock],
    ["tools/tree-sitter-skg", tree, treeLock],
  ]) {
    assertVersion(pkg.version, `${label}/package.json`);
    if (lock.version !== pkg.version || lock.packages?.[""]?.version !== pkg.version) {
      throw new Error(`${label}: package.json and package-lock.json versions differ`);
    }
  }
  if (vscode.version !== tree.version) throw new Error("editor package versions differ");
  if (!zonSemver.prerelease && vscode.version !== zonVersion) {
    throw new Error(`stable build.zig.zon version ${zonVersion} differs from editor packages ${vscode.version}`);
  }

  const goMod = await readFile(path.join(repo, "go", "go.mod"), "utf8");
  const modulePath = /^module\s+(\S+)/m.exec(goMod)?.[1];
  if (!modulePath) throw new Error("go/go.mod has no module declaration");
  checkModuleMajor(zonVersion, modulePath);
  await checkWorkflowPins();

  const args = process.argv.slice(2);
  if (args.length === 2 && args[0] === "--version") {
    checkModuleMajor(args[1], modulePath);
  } else if (args.length === 2 && args[0] === "--tag") {
    if (!args[1].startsWith("v")) throw new Error("release tag must start with v");
    const tagged = args[1].slice(1);
    assertVersion(tagged, "release tag");
    checkModuleMajor(tagged, modulePath);
    if (tagged !== zonVersion) throw new Error(`tag ${args[1]} differs from build.zig.zon version ${zonVersion}`);
  } else if (args.length !== 0) {
    throw new Error("usage: check-release.mjs [--version VERSION | --tag vVERSION]");
  }

  console.log(`release metadata: ${zonVersion}; Go module ${modulePath}; editor packages ${vscode.version}`);
}

try {
  await main();
} catch (error) {
  console.error(`release metadata check failed: ${error.message}`);
  process.exitCode = 1;
}
