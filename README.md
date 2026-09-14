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

cache_ttl: null    # override an inherited value with explicit null
```

## Why SKG

- **Small grammar and one canonical output.** The few accepted conveniences,
  such as colonless arrays, normalize to the same formatter output.
- **Structured data, nothing more.** No variables, templates,
  expressions, or computation. Configuration is data, not a program.
- **Your struct is the schema.** The parser hands back an AST; your
  types define what fields are valid. Extra config keys are silently
  ignored. Remove a field, it becomes a no-op. Nothing to keep in
  sync.
- **Typed scalars, nullable, hierarchical.** `int`, `float`, `bool`,
  `string`, `null`, arrays, blocks, block arrays. Triple-quoted
  multiline strings. Imports with last-wins merge.
- **Two first-party parsers, one frozen contract.** Versioned shared fixtures
  in [testdata/](testdata/) bind Zig and Go to the same syntax, resolution,
  native decoding, diagnostics, and canonical output.

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

cfg, err := skg.DecodeFile[Config]("config.skg", skg.DecodeOptions{})
```

Tagged fields define the schema. Extra keys are ignored; missing nonnullable
fields and inexact numeric conversions are errors. Supply native defaults with
`DecodeFileInto` and `AllowMissingFields`. Existing `Unmarshal` APIs retain their
permissive behavior. See [native types](docs/native-types.md) for mappings,
custom hooks and encoding details.

Full walk-through: **[examples/go/](examples/go/)**.

### Zig

```zig
const skg = @import("skg");

const Config = struct {
    name: []const u8,
    port: u16,
    debug: bool = false,
};

var result = skg.decodeFile(Config, allocator, "config.skg", .{});
defer result.deinit();

if (result.value) |config| {
    // use native fields while result is alive
    _ = config;
} else if (result.diagnostic) |d| {
    std.debug.print("{s}: {s}\n", .{ d.field_path, d.message });
}
```

Native structs, maps, lists, optional values and enums decode in process.
Defaults live on the struct; optional name mappings and custom hooks handle
application-specific types. The result owns all decoded storage.
See [native types](docs/native-types.md) for exact conversion and ownership rules.

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

### Zig (0.15.2)

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
- **[docs/compatibility.md](docs/compatibility.md)** - the V1 compatibility,
  toolchain, platform, and evolution policy
- **[docs/conformance.md](docs/conformance.md)** - shared parser, resolver, and
  porting contract
- **[docs/native-types.md](docs/native-types.md)** - native struct mappings,
  ownership, conversion, and validation rules
- **[docs/tree-sitter.md](docs/tree-sitter.md)** - tree-sitter grammar
  for Neovim, Helix, Zed, Emacs
- **[docs/vscode.md](docs/vscode.md)** - VS Code extension
- **[docs/formatter.md](docs/formatter.md)** - formatter behavior and in-place
  write guarantees

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

The package is currently a V1 release candidate while package metadata remains
`0.x`. Starting at package `v1.0.0`, the [V1 compatibility
policy](docs/compatibility.md) reserves breaking changes for V2.

## License

MIT
