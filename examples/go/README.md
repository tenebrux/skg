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
struct fields. Extra keys in the config are silently ignored; missing
keys keep the zero value. Add a field to your struct, it gets picked
up. Remove it, the config key becomes a silent no-op.

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

### Parse and unmarshal in one call

```go
var cfg Config
if err := skg.UnmarshalFile("app.skg", &cfg); err != nil {
    log.Fatalf("config error: %v", err)
}
```

### Marshal back to SKG

Round-trip is supported. Useful for config migration tools,
`--print-effective-config` flags, and tests.

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

- `skg.UnmarshalFile(path string, v interface{}) error` - parse a file
  and populate `v`
- `skg.Unmarshal(data []byte, v interface{}) error` - parse an
  in-memory buffer
- `skg.ParseFile(path string) (*ast.File, error)` - parse only, get
  the AST
- `skg.Marshal(v interface{}) ([]byte, error)` - struct to SKG text

See [../../go/](../../go/) for the full implementation.
