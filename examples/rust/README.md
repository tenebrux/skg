# SKG in Rust

This runnable example resolves [../app.skg](../app.skg), decodes it directly
into Serde structs, uses the resulting native values, and encodes the structs
back to canonical SKG. The structs in [src/main.rs](src/main.rs) are the
application schema; there is no separate schema file, code generator, CLI, or
language bridge.

## Run it

```sh
cd examples/rust
cargo run --locked
```

You will see the service, database, logging, nullable, list, and map values
from `app.skg`, followed by the typed value encoded back to SKG.

## Typed loading

Derive Serde's native traits on the types your application already owns:

```rust
use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize, Serialize)]
struct Config {
    name: String,
    port: u16,
    debug: bool,
    database: Database,
}

#[derive(Debug, Deserialize, Serialize)]
struct Database {
    host: String,
    port: u16,
    ssl: bool,
}
```

Resolve the file graph, then decode it:

```rust
let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("..");
let document = skg::resolve(
    root.join("app.skg"),
    &skg::ResolveOptions::rooted(root),
)?;
let config: Config = skg::from_document(&document)?;
```

`resolve` applies imports, overlays, `@replace`, and `@delete` before typed
decoding. `ResolveOptions::rooted` confines the entry and every import to the
chosen configuration tree. For trusted in-memory text with no import loading,
use `skg::from_str` instead.

## Type behavior

- Structs and string-keyed maps decode from SKG blocks.
- `Vec<T>` decodes from arrays or block arrays.
- `Option<T>` accepts explicit `null`; absent fields use Serde defaults when
  declared with `#[serde(default)]`.
- Unknown fields are ignored by default. `DecodeOptions` can reject them.
- Numeric conversions are range checked and exact by default.
- Decode errors include a stable code, JSON Pointer field path, and source
  location.

`skg::to_string(&config)` encodes the native value as deterministic canonical
SKG. It writes the application model rather than the original syntax, so
comments and imports are not reconstructed.

## Using the crate

The example uses the repository checkout through a path dependency. Published
applications use:

```toml
[dependencies]
skg = "0.1"
serde = { version = "1", features = ["derive"] }
```

See [../../rust/README.md](../../rust/README.md) for the full mapping,
diagnostic, loader, and compatibility contract.
