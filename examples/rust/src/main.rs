//! Runnable typed-loading example for SKG's native Rust package.
//!
//! Run from this directory with `cargo run --locked`.

use std::collections::BTreeMap;
use std::error::Error;
use std::path::Path;

use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize, Serialize)]
struct Config {
    name: String,
    port: u16,
    debug: bool,
    allowed_hosts: Vec<String>,
    cache_ttl: Option<u64>,
    motd: String,
    database: Database,
    logging: Logging,
    packages: BTreeMap<String, Vec<String>>,
    extra: Extra,
}

#[derive(Debug, Deserialize, Serialize)]
struct Database {
    host: String,
    port: u16,
    name: String,
    max_connections: u16,
    ssl: bool,
}

#[derive(Debug, Deserialize, Serialize)]
struct Logging {
    level: String,
    max_size_mb: u16,
    keep_rotations: u16,
    streams: Streams,
}

#[derive(Debug, Deserialize, Serialize)]
struct Streams {
    stdout: bool,
    file: bool,
    syslog: bool,
}

#[derive(Debug, Deserialize, Serialize)]
struct Extra {
    retries: u16,
    region: String,
    verbose: bool,
}

fn main() -> Result<(), Box<dyn Error>> {
    // Resolve from a confined root before decoding. The shared app.skg has no
    // imports, but this same call follows and merges them when they are added.
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("..");
    let document = skg::resolve(root.join("app.skg"), &skg::ResolveOptions::rooted(root))?;
    let config: Config = skg::from_document(&document)?;

    println!("name:         {}", config.name);
    println!("port:         {}", config.port);
    println!("debug:        {}", config.debug);
    println!("hosts:        {:?}", config.allowed_hosts);
    println!(
        "db:           {}:{}/{} (ssl={}, pool={})",
        config.database.host,
        config.database.port,
        config.database.name,
        config.database.ssl,
        config.database.max_connections
    );
    println!("log level:    {}", config.logging.level);
    println!(
        "log streams:  stdout={} file={} syslog={}",
        config.logging.streams.stdout, config.logging.streams.file, config.logging.streams.syslog
    );
    match config.cache_ttl {
        Some(ttl) => println!("cache_ttl:    {ttl}"),
        None => println!("cache_ttl:    <null> (not set)"),
    }
    println!("motd:\n{}", config.motd);

    // Encoding writes the typed application value as canonical SKG. It does
    // not restore comments or imports because those are not part of the type.
    let encoded = skg::to_string(&config)?;
    println!("\n--- encoded back to SKG ---");
    print!("{encoded}");

    Ok(())
}
