#!/usr/bin/env node

import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import oniguruma from "vscode-oniguruma";
import textmate from "vscode-textmate";

const { loadWASM, OnigScanner, OnigString } = oniguruma;
const { INITIAL, Registry, parseRawGrammar } = textmate;

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const grammarPath = path.join(here, "syntaxes", "skg.tmLanguage.json");
const wasm = await readFile(require.resolve("vscode-oniguruma/release/onig.wasm"));
await loadWASM(wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength));

const registry = new Registry({
  onigLib: Promise.resolve({
    createOnigScanner: (patterns) => new OnigScanner(patterns),
    createOnigString: (text) => new OnigString(text),
  }),
  loadGrammar: async (scope) => {
    if (scope !== "source.skg") return null;
    return parseRawGrammar(await readFile(grammarPath, "utf8"), grammarPath);
  },
});
const grammar = await registry.loadGrammar("source.skg");
if (!grammar) throw new Error("source.skg grammar did not load");

async function collect(dir) {
  const files = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const child = path.join(dir, entry.name);
    if (entry.isDirectory()) files.push(...(await collect(child)));
    else if (entry.name.endsWith(".skg")) files.push(child);
  }
  return files.sort();
}

function tokenize(source, label) {
  let stack = INITIAL;
  const tokens = [];
  for (const [lineNumber, line] of source.split("\n").entries()) {
    const result = grammar.tokenizeLine(line, stack);
    stack = result.ruleStack;
    let end = 0;
    for (const token of result.tokens) {
      const start = Math.min(token.startIndex, line.length);
      const finish = Math.min(token.endIndex, line.length);
      if (start !== end || finish < start) {
        throw new Error(`${label}:${lineNumber + 1}: grammar left an uncovered span`);
      }
      end = finish;
      if (token.scopes.some((scope) => scope.startsWith("invalid."))) {
        throw new Error(`${label}:${lineNumber + 1}: valid source received ${token.scopes.join(" ")}`);
      }
      tokens.push({ text: line.slice(start, finish), scopes: token.scopes });
    }
    if (end !== line.length) {
      throw new Error(`${label}:${lineNumber + 1}: grammar covered ${end}/${line.length} bytes in ${JSON.stringify(line)}`);
    }
  }
  if (stack.depth > 1) throw new Error(`${label}: grammar has an unterminated state at end of valid input (depth ${stack.depth})`);
  return tokens;
}

function expectScope(source, needle, scope) {
  const tokens = tokenize(source, "representative syntax");
  if (!tokens.some((token) => token.text.includes(needle) && token.scopes.includes(scope))) {
    throw new Error(`expected ${JSON.stringify(needle)} to receive ${scope}`);
  }
}

const validDir = path.join(here, "..", "..", "testdata", "valid");
const fixtures = await collect(validDir);
if (fixtures.length === 0) throw new Error("no valid SKG fixtures found");
for (const file of fixtures) tokenize(await readFile(file, "utf8"), path.relative(here, file));

expectScope('@delete "quoted key"', "@delete", "keyword.control.skg");
expectScope('@delete "quoted key"', '"quoted key"', "variable.other.property.skg");
expectScope('@replace "quoted block" { fresh: true }', "@replace", "keyword.control.skg");
expectScope('import ["base.skg"]', "import", "keyword.control.import.skg");
expectScope('"quoted key": { nested: [1, null] }', '"quoted key"', "variable.other.property.skg");
expectScope('items: [{ id: 1 }, null]', "null", "constant.language.null.skg");
expectScope("ratio: -12.50", "-12.50", "constant.numeric.float.skg");
expectScope('message: "line\\nnext"', "\\n", "constant.character.escape.skg");
expectScope('message: """line one\nline two"""', "line two", "string.quoted.triple.skg");

console.log(`VS Code TextMate grammar tokenized ${fixtures.length} valid fixtures and all V1 syntax probes`);
