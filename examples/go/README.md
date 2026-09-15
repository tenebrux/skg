# SKG in Go

A working example of loading an SKG config into Go structs.

Source: [main.go](main.go) - reads [../app.skg](../app.skg) and
populates a `Config` struct.

## Run it

```sh
cd examples/go
go run main.go
```

You'll see every field from `app.skg` printed, then the struct
marshaled back to SKG text.

## How it works

### Your struct IS the schema

No separate schema file. The `skg:"name"` tag maps config keys to
struct fields. Extra keys in the config are ignored. With the typed loader,
missing nonnullable fields fail; nullable fields may be absent. Supply native
defaults through `DecodeFileInto` with `AllowMissingFields`. No second schema
file is needed.

```go
type Config struct {
    Name         string                 `skg:"name"`
    Port         int64                  `skg:"port"`
    Debug        bool                   `skg:"debug"`
    AllowedHosts []string               `skg:"allowed_hosts"`
    CacheTTL     *int64                 `skg:"cache_ttl"` // pointer = nullable
    Motd         string                 `skg:"motd"`
    Database     Database               `skg:"database"` // nested = block
    Packages     map[string][]string    `skg:"packages"` // map = dynamic keys
    Extra        map[string]interface{} `skg:"extra"`    // any-value bag
}
```

### Parse and decode in one call

```go
cfg, err := skg.DecodeFile[Config]("app.skg", skg.DecodeOptions{})
if err != nil {
    log.Fatalf("config error: %v", err)
}
```

### Marshal back to SKG

Encode the resulting struct for `--print-effective-config`, migration tools,
or tests. `Marshal` writes native values, not the original comments or imports.
It does not reverse custom decode hooks, and nil maps/slices encode as empty
collections. See [native types](../../docs/native-types.md) for the exact rules.

```go
out, err := skg.Marshal(cfg)
```

## Type mapping

| SKG type          | Go type                             |
| ----------------- | ----------------------------------- |
| `int`             | `int`, `int8`..`int64`, `uint`, ... |
| `float`           | `float32`, `float64`                |
| `bool`            | `bool`                              |
| `string`          | `string`                            |
| `"""multi"""`     | `string` (newlines preserved)       |
| `null`            | nil pointer / nil map / nil slice   |
| `array`           | `[]T` where T matches element type  |
| fixed array       | `[N]T`, with exactly N elements     |
| block             | nested struct                       |
| block array       | `[]T`                               |
| block w/ dyn keys | `map[string]T`                      |
| untyped bag       | `map[string]interface{}`            |

## Importing

```go
import skg "github.com/tenebrux/skg/go"
```

Module path is `github.com/tenebrux/skg/go` (not the repo root). The
`go/` subdirectory is a self-contained Go module so you can depend on
it without pulling in the Zig sources or test fixtures.

## Public interface

The main entry points:

- `skg.DecodeFile[T](path, options) (T, error)` - resolve a file and
  decode with strict native type checks; no partial value on failure
- `skg.DecodeFileWithOptions[T](path, decodeOptions, resolveOptions)` - strict
  decode with explicit aggregate budgets and optional rooted resolution
- `skg.DecodeSource[T](bytes, path, options) (T, error)` - decode bytes
  without reading imports
- `skg.DecodeFileInto(path, &target, options) error` - stage native defaults
  and replace the target only on success
- `skg.UnmarshalFile(path string, v interface{}) error` - parse a file
  and populate `v` with the existing permissive policy
- `skg.Unmarshal(data []byte, v interface{}) error` - parse an
  in-memory buffer
- `skg.ParseFile(path string) (*skg.File, error)` - parse only, get
  the AST
- `skg.ParseFileWithOptions(path, resolveOptions)` - parse a file graph under
  the same explicit resolution policy
- `skg.Marshal(v interface{}) ([]byte, error)` - struct to SKG text

See [../../go/](../../go/) for the full implementation.
