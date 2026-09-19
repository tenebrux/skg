#!/usr/bin/env node

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

function expectedAssets(tag) {
  const assets = [
    "checksums.txt",
    "skg_aarch64_linux.tar.gz",
    "skg_riscv64_linux.tar.gz",
    "skg_x86_64_linux.tar.gz",
  ];

  for (const arch of ["amd64", "arm64"]) {
    for (const extension of ["apk", "deb", "pkg.tar.zst", "rpm"]) {
      assets.push(`skg_${tag}_linux_${arch}.${extension}`);
    }
  }
  for (const extension of ["apk", "deb", "rpm"]) {
    assets.push(`skg_${tag}_linux_riscv64.${extension}`);
  }

  return assets.sort();
}

function isComplete(release, tag) {
  if (
    release?.isDraft !== false ||
    release?.isPrerelease !== true ||
    typeof release?.publishedAt !== "string" ||
    release.publishedAt.length === 0 ||
    !Array.isArray(release?.assets)
  ) {
    return false;
  }

  if (
    release.assets.some(
      (asset) =>
        typeof asset?.name !== "string" ||
        !Number.isSafeInteger(asset?.size) ||
        asset.size <= 0 ||
        asset?.state !== "uploaded",
    )
  ) {
    return false;
  }

  const actual = release.assets.map((asset) => asset.name).sort();
  return JSON.stringify(actual) === JSON.stringify(expectedAssets(tag));
}

function unexpectedAssetIds(release, tag) {
  if (release?.isImmutable === true || !Array.isArray(release?.assets)) {
    return [];
  }

  const expected = new Set(expectedAssets(tag));
  return release.assets
    .filter((asset) => !expected.has(asset?.name))
    .map((asset) => {
      const match = /^https:\/\/api\.github\.com\/repos\/[^/]+\/[^/]+\/releases\/assets\/([1-9]\d*)$/.exec(
        asset?.apiUrl,
      );
      if (!match) {
        throw new Error("unexpected release asset has no valid GitHub API URL");
      }
      return match[1];
    });
}

function selfTest(tag) {
  const complete = {
    isDraft: false,
    isPrerelease: true,
    publishedAt: "1970-01-01T00:00:00Z",
    assets: expectedAssets(tag).map((name) => ({
      name,
      size: 1,
      state: "uploaded",
    })),
  };

  assert.equal(isComplete(complete, tag), true);
  assert.equal(isComplete({ ...complete, isDraft: true }, tag), false);
  assert.equal(isComplete({ ...complete, publishedAt: null }, tag), false);
  assert.equal(
    isComplete({ ...complete, assets: complete.assets.slice(1) }, tag),
    false,
  );
  assert.equal(
    isComplete({
      ...complete,
      assets: complete.assets.map((asset, index) =>
        index === 0 ? { ...asset, size: 0 } : asset,
      ),
    }, tag),
    false,
  );

  const stale = {
    ...complete,
    assets: [
      ...complete.assets,
      {
        apiUrl:
          "https://api.github.com/repos/tenebrux/skg/releases/assets/123",
        name: "obsolete-package.zip",
        size: 1,
        state: "uploaded",
      },
    ],
  };
  assert.equal(isComplete(stale, tag), false);
  assert.deepEqual(unexpectedAssetIds(stale, tag), ["123"]);
  assert.deepEqual(unexpectedAssetIds({ ...stale, isImmutable: true }, tag), []);
  assert.throws(
    () =>
      unexpectedAssetIds(
        {
          ...complete,
          assets: [{ apiUrl: "https://example.com/123", name: "stale" }],
        },
        tag,
      ),
    /valid GitHub API URL/,
  );

  assert.equal(isComplete(JSON.parse(JSON.stringify(complete)), tag), true);
}

const args = process.argv.slice(2);
if (args[0] === "--self-test" && args.length === 2) {
  selfTest(args[1]);
  process.exit(0);
}
const listUnexpected = args[0] === "--unexpected-asset-ids";
if ((listUnexpected && args.length !== 2) || (!listUnexpected && args.length !== 1)) {
  console.error(
    `usage: ${process.argv[1]} [--self-test | --unexpected-asset-ids] TAG`,
  );
  process.exit(2);
}

let release;
try {
  release = JSON.parse(readFileSync(0, "utf8"));
} catch {
  process.exit(1);
}
if (listUnexpected) {
  try {
    console.log(unexpectedAssetIds(release, args[1]).join("\n"));
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }
} else {
  process.exit(isComplete(release, args[0]) ? 0 : 1);
}
