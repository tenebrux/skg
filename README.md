# SKG

**Static Key Group** - a small, strict, human-readable configuration
language with native parsers in Zig and Go.

```skg
skg_version: "1.0"

import ["./theme.skg"]

name: "my-service"
port: 8080
debug: false
allowed_hosts: ["localhost", "127.0.0.1"]

database {
  host: "localhost"
  port: 5432
  ssl: true
  replicas ["db-a", "db-b"]    # colonless array shorthand
}

users [
  { name: "admin"  sudo: true  groups: ["wheel"] }
  { name: "guest"  sudo: false groups: ["users"] }
]

motd: """Welcome.
No warranty expressed or implied.
You get what you get."""

cache_ttl: null    # explicitly unset an inherited value
```

## Why SKG

- **One way to write each construct.** No shortcuts, no alternatives,
  no implicit behavior.
- **Structured data, nothing more.** No variables, templates,
  expressions, or computation. Configuration is data, not a program.
- **Your struct is the schema.** The parser hands back an AST; your
  types define what fields are valid. Extra config keys are silently
  ignored. Remove a field, it becomes a no-op. Nothing to keep in
  sync.
- **Typed scalars, nullable, hierarchical.** `int`, `float`, `bool`,
  `string`, `null`, arrays, blocks, block arrays. Triple-quoted
  multiline strings. Imports with last-wins merge.
- **Two first-party parsers, one conformance suite.** Shared fixtures
  in [testdata/](testdata/) are the contract between the Zig and Go
  implementations.

Created for [dusk](https://github.com/tenebrux/dusk) but standalone -
nothing in the parser depends on dusk.

## Quick start

### Go

```go
import skg "github.com/tenebrux/skg/go"

type Config struct {
    Name  string   `skg:"name"`
    Port  int64    `skg:"port"`
    Debug bool     `skg:"debug"`
    Tags  []string `skg:"tags"`
    DB    Database `skg:"database"` // nested struct = block
}

var cfg Config
err := skg.UnmarshalFile("config.skg", &cfg)
```

Struct tags work like `encoding/json`. Extra config keys are ignored,
missing keys keep zero values. Round-trip with `skg.Marshal`.

Full walk-through: **[examples/go/](examples/go/)**.

### Zig

```zig
const skg = @import("skg");

var result = skg.parseSource(allocator, source, "config.skg");
defer result.deinit();

if (result.file) |file| {
    // walk file.children, pattern-match on field keys and block names
} else if (result.diagnostic) |d| {
    std.debug.print("{s}:{d}:{d}: {s}\n", .{ d.path, d.line, d.col, d.message });
}
```

No reflection, no tags - you write a small walker that maps keys to
struct fields. Explicit, arena-allocated, full control over defaults
and validation.

Full walk-through: **[examples/zig/](examples/zig/)**.

## More examples

Standalone `.skg` files under [examples/](examples/) showing real-world
config patterns:

- `app.skg` - web service (kitchen sink)
- `ci-pipeline.skg` - ordered stages with nested steps (block arrays)
- `feature-flags.skg` - defaults + per-environment overrides
- `servers.skg` - backend pool with health checks
- `users.skg` - structured user accounts
- `theme.skg` + `main-with-imports.skg` - imports demo

## Build

### Zig (0.15+)

```sh
zig build       # build the module
zig build test  # run parser tests against testdata/
```

### Go (1.26+)

```sh
cd go
go build ./...
go test ./...
```

## Documentation

- **[docs/spec.md](docs/spec.md)** - full language specification
- **[docs/tree-sitter.md](docs/tree-sitter.md)** - tree-sitter grammar
  for Neovim, Helix, Zed, Emacs
- **[docs/vscode.md](docs/vscode.md)** - VS Code extension

## Repo layout

```text
skg/
  zig/        # Zig implementation (lexer, parser, ast, merge, emit)
  go/         # Go implementation (+ unmarshal, marshal)
  testdata/   # Shared conformance fixtures - the contract
  examples/   # Working Go and Zig examples + real-world .skg files
  tools/      # tree-sitter grammar + VS Code extension
  docs/       # Language spec and editor integration guides
```

Each language directory is a self-contained implementation with its
own build tooling. Both are validated against the same `testdata/`
fixtures on every test run.

## License

MIT
