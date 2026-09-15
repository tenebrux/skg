#!/usr/bin/env node

import { createHash } from "node:crypto";
import { lstat, readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const contractsDir = path.join(repo, "testdata", "contracts");
const sourceRoots = [
  "testdata/error-codes.json",
  "testdata/native",
  "testdata/resolution",
  "testdata/valid",
  "testdata/invalid",
];

async function filesUnder(relative) {
  const absolute = path.join(repo, relative);
  const info = await lstat(absolute);
  if (info.isSymbolicLink()) throw new Error(`${relative}: symlinks are not contract files`);
  if (info.isFile()) return [relative];
  if (!info.isDirectory()) throw new Error(`${relative}: unsupported filesystem entry`);
  const result = [];
  for (const entry of await readdir(absolute, { withFileTypes: true })) {
    const child = path.posix.join(relative, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`${child}: symlinks are not contract files`);
    if (entry.isDirectory()) result.push(...(await filesUnder(child)));
    else if (entry.isFile()) result.push(child);
    else throw new Error(`${child}: unsupported filesystem entry`);
  }
  return result;
}

async function sourceFiles() {
  const result = (await Promise.all(sourceRoots.map(filesUnder))).flat().sort();
  if (new Set(result).size !== result.length) throw new Error("contract source roots overlap");
  return result;
}

async function digest(relative) {
  return createHash("sha256").update(await readFile(path.join(repo, relative))).digest("hex");
}

function exactKeys(value, expected, label) {
  if (value === null || Array.isArray(value) || typeof value !== "object") {
    throw new Error(`${label}: expected an object`);
  }
  const actual = Object.keys(value).sort();
  const wanted = [...expected].sort();
  if (JSON.stringify(actual) !== JSON.stringify(wanted)) {
    throw new Error(`${label}: expected keys ${wanted.join(", ")}; got ${actual.join(", ")}`);
  }
}

async function generatedManifest(languageVersion) {
  if (!/^1\.\d+$/.test(languageVersion)) throw new Error("this lock accepts V1 language versions such as 1.1");
  const assigned = new Set();
  try {
    for (const entry of await readdir(contractsDir, { withFileTypes: true })) {
      if (!entry.isFile() || !/^v1\.\d+\.json$/.test(entry.name)) continue;
      const manifest = JSON.parse(await readFile(path.join(contractsDir, entry.name), "utf8"));
      if (manifest.files && typeof manifest.files === "object") {
        for (const relative of Object.keys(manifest.files)) assigned.add(relative);
      }
    }
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  const files = {};
  for (const relative of await sourceFiles()) {
    if (!assigned.has(relative)) files[relative] = await digest(relative);
  }
  if (Object.keys(files).length === 0) throw new Error("no unassigned contract files to put in a new manifest");
  return {
    contract_version: 1,
    language_version: languageVersion,
    hash: "sha256",
    files,
  };
}

async function check() {
  const discovered = new Set(await sourceFiles());
  const assigned = new Map();
  const allEntries = await readdir(contractsDir, { withFileTypes: true });
  for (const entry of allEntries) {
    if (!entry.isFile() || !/^v1\.\d+\.json$/.test(entry.name)) {
      throw new Error(`testdata/contracts/${entry.name}: expected a V1 manifest named v1.MINOR.json`);
    }
  }
  const entries = allEntries.sort((a, b) => a.name.localeCompare(b.name));
  if (entries.length === 0) throw new Error("no versioned contract manifests found");

  for (const entry of entries) {
    const label = path.posix.join("testdata/contracts", entry.name);
    const manifest = JSON.parse(await readFile(path.join(contractsDir, entry.name), "utf8"));
    exactKeys(manifest, ["contract_version", "language_version", "hash", "files"], label);
    exactKeys(manifest.files, Object.keys(manifest.files), `${label}.files`);
    const filenameVersion = entry.name.slice(1, -5);
    if (manifest.contract_version !== 1 || manifest.language_version !== filenameVersion || manifest.hash !== "sha256") {
      throw new Error(`${label}: unsupported or inconsistent contract metadata`);
    }
    if (Object.keys(manifest.files).length === 0) throw new Error(`${label}: files must not be empty`);

    for (const [relative, expected] of Object.entries(manifest.files)) {
      if (!discovered.has(relative)) throw new Error(`${label}: locked file is missing or outside the contract roots: ${relative}`);
      if (assigned.has(relative)) throw new Error(`${relative}: assigned by both ${assigned.get(relative)} and ${label}`);
      if (typeof expected !== "string" || !/^[0-9a-f]{64}$/.test(expected)) {
        throw new Error(`${label}: invalid SHA-256 for ${relative}`);
      }
      const actual = await digest(relative);
      if (actual !== expected) throw new Error(`${relative}: V${filenameVersion} contract changed (expected ${expected}, got ${actual})`);
      assigned.set(relative, label);
    }
  }

  const unassigned = [...discovered].filter((relative) => !assigned.has(relative));
  if (unassigned.length > 0) {
    throw new Error(`contract files need a version manifest:\n${unassigned.map((name) => `  ${name}`).join("\n")}`);
  }
  console.log(`V1 contract lock: ${assigned.size} files verified across ${entries.length} manifest(s)`);
}

try {
  if (process.argv[2] === "--generate") {
    if (process.argv.length !== 4) throw new Error("usage: check-contract.mjs --generate MAJOR.MINOR");
    console.log(`${JSON.stringify(await generatedManifest(process.argv[3]), null, 2)}\n`);
  } else {
    if (process.argv.length !== 2) throw new Error("usage: check-contract.mjs");
    await check();
  }
} catch (error) {
  console.error(`contract check failed: ${error.message}`);
  process.exitCode = 1;
}
