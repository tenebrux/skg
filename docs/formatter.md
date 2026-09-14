# Formatter contract

`skg fmt` is the canonical Zig formatter. It parses one source file without
resolving imports and emits the V1 canonical spelling. `skg fmt --check` is
read-only; `skg fmt --stdin` reads standard input and writes standard output.

## Source and trivia

Formatting preserves SKG values, key order, import paths, overlay operations and
every comment's complete text. It is intentionally not a byte-for-byte or
layout-preserving formatter. Whitespace, line endings and comment placement are
normalized from the AST:

- output uses LF and two-space indentation; nonempty output has a final newline;
- headers have no blank line between them and one blank line before a body;
- a non-first top-level block or block array has one preceding blank line;
- other blank lines are removed;
- comments attached to fields and named objects retain that attachment;
- comments between header directives become file-leading comments;
- comments in positions without a dedicated trivia slot are reattached to the
  nearest represented owner; comments between scalar array values become the
  array's trailing comments.

The last two rules can move a comment while retaining its text. Applications
that need source-exact layout should retain the original bytes. Comment trivia
is an optional port capability; a language package does not need a formatter to
provide native parsing and typed loading.

## In-place writes

For a changed regular file, the formatter writes a random temporary in the same
directory, synchronizes its contents, and atomically renames it over the target.
A crash before the rename leaves the original bytes in place. The containing
directory is not synchronized because that operation is not portable across the
supported filesystems; a machine losing power immediately after a successful
return can still lose the rename on a filesystem that had not committed its
directory entry.

The input path is resolved once. If it is a symbolic link, the link remains and
its referent is replaced. The formatter rejects a regular file with more than
one hard link because atomic replacement cannot update every name for the old
inode. `--check` remains usable for such files.

The formatter preserves these metadata classes before replacement:

| Platform | Preserved |
| --- | --- |
| POSIX | complete mode, owner and group |
| Linux | POSIX metadata plus all extended attributes, with up to 1 MiB of names and 1 MiB of value data; this includes the usual POSIX ACL xattrs |
| Windows | basic file attributes exposed by `FILE_BASIC_INFORMATION` |

Any failure while reading or applying promised metadata removes the temporary
and leaves the original path untouched. On macOS and other non-Linux POSIX
systems, extended attributes and ACLs are outside the V1 guarantee. On Windows,
the security descriptor and alternate data streams are outside the guarantee.
Use `--check` or `--stdin` when those unpromised classes are significant.

Immediately before rename, the formatter reopens the resolved path, compares
its identity, type, size, mode, modification/change timestamps, hard-link count
and exact bytes with the source it parsed, then checks identity and link count
again after reading. A detected edit fails with `FileChanged` and leaves the new
content untouched. Portable path APIs do not provide an atomic compare-and-swap
rename, so an adversarial writer can still race the final check and rename.
