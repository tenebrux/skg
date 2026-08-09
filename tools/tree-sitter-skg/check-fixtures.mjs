#!/usr/bin/env node
// Cheap gate for grammar.js.
//
// The tree-sitter grammar is a *highlighting* grammar, not a conformance peer:
// it is deliberately more permissive than go/ and zig/ and has no AST contract
// with them. So this checks exactly one thing - every .skg file the real
// parsers accept must also parse here without an ERROR or MISSING node.
//
// That is enough to catch a grammar edit that breaks highlighting on valid
// input, and it stays cheap. Do not grow this into an AST-equivalence check.

import { execFileSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const validDir = join(here, "..", "..", "testdata", "valid");

/** Every .skg file under testdata/valid, including directory fixtures. */
function collect(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...collect(path));
    else if (entry.name.endsWith(".skg")) out.push(path);
  }
  return out.sort();
}

const files = collect(validDir);
if (files.length === 0) {
  console.error("no .skg fixtures found under testdata/valid - refusing to pass vacuously");
  process.exit(1);
}

// `tree-sitter parse -q` prints a line per file that contains an ERROR or
// MISSING node and exits non-zero; clean files print nothing.
try {
  execFileSync(
    "npx",
    ["tree-sitter", "parse", "-q", ...files.map((f) => relative(here, f))],
    { cwd: here, stdio: ["ignore", "inherit", "inherit"] },
  );
} catch {
  console.error(`\ntree-sitter grammar failed on one or more of ${files.length} valid fixtures (see above)`);
  process.exit(1);
}

console.log(`tree-sitter grammar parsed ${files.length} valid fixtures with no ERROR nodes`);
