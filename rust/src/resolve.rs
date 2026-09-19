//! File resolution: imports, overlays, cycles, and resource budgets.
//!
//! Import semantics (frozen V1 contract):
//!
//! - Import paths are relative to the canonical directory of the file that
//!   contains the import statement, never the process working directory.
//! - Imports merge in declaration order, then the importing file's own
//!   children overlay everything it imported: the main file always wins.
//! - Canonical identity is the host's real absolute path, so `./a.skg` and
//!   `a.skg` are one file and symlink aliases share identity. Hard-link
//!   aliases stay distinct.
//! - A diamond (two files importing one file) is legal and linear, via a
//!   cache of finished files. A cycle is [`ErrorCode::CircularImport`].
//! - Chains deeper than 32 levels below the entry file are
//!   [`ErrorCode::ImportChainTooDeep`].
//! - One resolution call has V1 default budgets for aggregate source bytes,
//!   unique canonical files, parsed nodes and values, and merge work.
//!
//! All filesystem access goes through the [`Loader`] trait, so applications
//! can supply in-memory, sandboxed, or otherwise custom stores. The byte API
//! never touches the filesystem; only this module does.

use std::collections::HashMap;
use std::io::{self, Read};
use std::path::{Component, Path, PathBuf};

use crate::error::{Diagnostic, ErrorCode, Position, ResolveError};
use crate::merge::{materialize_nodes, merge_nodes_budget};
use crate::model::Document;
use crate::parser::{parse_bytes, MAX_FILE_SIZE};

/// How many levels of imports the resolver follows below the entry file.
pub const MAX_IMPORT_DEPTH: usize = 32;

// V1 file-resolution defaults. These are compatibility limits: lowering one in
// a 1.x release would reject a graph accepted by V1.0.
pub const DEFAULT_MAX_RESOLVE_BYTES: u64 = 64 * 1024 * 1024;
pub const DEFAULT_MAX_RESOLVE_FILES: usize = 1024;
pub const DEFAULT_MAX_RESOLVE_NODES: usize = 8_000_000;
pub const DEFAULT_MAX_RESOLVE_MERGE_WORK: u64 = 64_000_000;

/// Where an import was written: the file that contains it and the position of
/// its path token. A resolution failure is reported there rather than at 0:0,
/// so the diagnostic points at a line the author can go and fix.
struct Origin {
    path: String,
    position: Position,
}

/// A resolved file and the deepest import chain beneath it, so cache hits
/// enforce the same graph-depth limit regardless of import order.
struct Resolved {
    document: Document,
    depth: usize,
}

/// File source for resolution. Implement this to load SKG from anything: the
/// real filesystem, an in-memory map, an archive, or a sandboxed store.
///
/// `canonicalize` must return a stable identity for each distinct file:
/// the same value for every alias of one file (including symlinks and `./`
/// spellings) and different values for different files. `read` returns the
/// file's bytes; only the first `MAX_FILE_SIZE + 1` are significant.
pub trait Loader {
    /// Canonical identity of `path`. An error means the file cannot be found
    /// or resolved.
    fn canonicalize(&self, path: &Path) -> io::Result<PathBuf>;

    /// The bytes of the file at `path`. `path` is a path previously returned
    /// by [`Loader::canonicalize`], or the entry path itself.
    fn read(&self, path: &Path) -> io::Result<Vec<u8>>;
}

/// The default [`Loader`]: the real filesystem.
///
/// Canonical identity is `std::fs::canonicalize` (realpath), which follows
/// symlinks. A file that cannot be canonicalized - including through an
/// operating-system symlink loop - is [`ErrorCode::ImportNotFound`].
#[derive(Debug, Default, Clone, Copy)]
pub struct FsLoader;

impl Loader for FsLoader {
    fn canonicalize(&self, path: &Path) -> io::Result<PathBuf> {
        std::fs::canonicalize(path)
    }

    fn read(&self, path: &Path) -> io::Result<Vec<u8>> {
        // Read one byte past the cap rather than trusting the file's stated
        // size, which lies for pipes and procfs entries.
        let mut file = std::fs::File::open(path)?;
        let mut buffer = Vec::new();
        (&mut file)
            .take(MAX_FILE_SIZE as u64 + 1)
            .read_to_end(&mut buffer)?;
        Ok(buffer)
    }
}

/// An in-memory [`Loader`] for tests, examples, and sandboxed embedding.
///
/// Canonical identity is lexical: paths are made absolute against a virtual
/// base, then `.` and `..` components are collapsed. There are no symlinks,
/// so every alias of one normalized path shares identity.
#[derive(Debug, Clone, Default)]
pub struct MemoryLoader {
    base: PathBuf,
    files: HashMap<PathBuf, Vec<u8>>,
}

impl MemoryLoader {
    /// A loader whose relative paths resolve against the process working
    /// directory, mirroring [`FsLoader`] for in-process files.
    #[must_use]
    pub fn new() -> Self {
        Self {
            base: std::env::current_dir().unwrap_or_else(|_| PathBuf::from("/")),
            files: HashMap::new(),
        }
    }

    /// Insert one file's bytes at `path` (absolute or relative to the base).
    #[must_use]
    pub fn with_file(mut self, path: impl AsRef<Path>, contents: impl Into<Vec<u8>>) -> Self {
        self.insert(path, contents);
        self
    }

    /// Insert one file's bytes at `path`.
    pub fn insert(&mut self, path: impl AsRef<Path>, contents: impl Into<Vec<u8>>) {
        self.files
            .insert(normalize_path(&self.base, path.as_ref()), contents.into());
    }
}

impl Loader for MemoryLoader {
    fn canonicalize(&self, path: &Path) -> io::Result<PathBuf> {
        let canonical = normalize_path(&self.base, path);
        if self.files.contains_key(&canonical) {
            Ok(canonical)
        } else {
            Err(io::Error::new(io::ErrorKind::NotFound, "no such file"))
        }
    }

    fn read(&self, path: &Path) -> io::Result<Vec<u8>> {
        let canonical = normalize_path(&self.base, path);
        self.files
            .get(&canonical)
            .cloned()
            .ok_or_else(|| io::Error::new(io::ErrorKind::NotFound, "no such file"))
    }
}

/// Collapse `.` and `..` after making `path` absolute against `base`.
fn normalize_path(base: &Path, path: &Path) -> PathBuf {
    let joined = if path.is_absolute() {
        path.to_path_buf()
    } else {
        base.join(path)
    };
    let mut out = PathBuf::new();
    for component in joined.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                // Never pop past the filesystem root.
                out.pop();
            }
            other => out.push(other.as_os_str()),
        }
    }
    if out.as_os_str().is_empty() {
        out.push(Component::RootDir.as_os_str());
    }
    out
}

/// File-graph policy. `Default` selects the frozen V1 budgets. `root` enables
/// opt-in containment after symlink resolution: the entry and every import
/// target must remain inside it or resolution fails with
/// [`ErrorCode::PathOutsideRoot`]. No root keeps the unrestricted,
/// relative-import behavior.
#[derive(Debug, Clone, Default)]
pub struct ResolveOptions {
    /// Optional containment root. Both `PathBuf` and `&Path` convert.
    pub root: Option<PathBuf>,
    /// Aggregate source bytes across unique canonical files.
    pub max_bytes: u64,
    /// Unique canonical files parsed by one resolution call.
    pub max_files: usize,
    /// Named nodes plus every recursively nested value, across unique files.
    pub max_nodes: usize,
    /// Node slots scanned across every recursive overlay merge.
    pub max_merge_work: u64,
}

impl ResolveOptions {
    /// V1 defaults with no containment root.
    #[must_use]
    pub fn new() -> Self {
        Self {
            root: None,
            max_bytes: DEFAULT_MAX_RESOLVE_BYTES,
            max_files: DEFAULT_MAX_RESOLVE_FILES,
            max_nodes: DEFAULT_MAX_RESOLVE_NODES,
            max_merge_work: DEFAULT_MAX_RESOLVE_MERGE_WORK,
        }
    }

    /// Contain the graph inside `root` after resolving symlinks.
    #[must_use]
    pub fn rooted(root: impl Into<PathBuf>) -> Self {
        Self {
            root: Some(root.into()),
            ..Self::new()
        }
    }
}

/// Load `entry`, resolve its import graph, compose overlays, and finalize.
///
/// This is the default file API: it reads through [`FsLoader`] with the given
/// [`ResolveOptions`]. The returned [`Document`] has `imports_resolved` set,
/// delete markers removed, and replacement flags cleared; emitting it writes
/// standalone final data without import statements.
///
/// # Errors
///
/// Parse failures in any file of the graph return [`ResolveError::Parse`];
/// resolution failures return [`ResolveError::Resolution`]. A failure on an
/// imported file is reported at the import statement that named it.
pub fn resolve(
    entry: impl AsRef<Path>,
    options: &ResolveOptions,
) -> Result<Document, ResolveError> {
    resolve_with(entry, &FsLoader, options)
}

/// Load `entry` through a custom [`Loader`] with explicit options.
///
/// This is the seam for in-memory, sandboxed, or otherwise custom stores:
/// resolution logic, budgets, and error semantics are identical to [`resolve`].
pub fn resolve_with(
    entry: impl AsRef<Path>,
    loader: &dyn Loader,
    options: &ResolveOptions,
) -> Result<Document, ResolveError> {
    let root = match &options.root {
        Some(root) => match loader.canonicalize(root) {
            Ok(canonical) => Some(canonical),
            Err(_) => {
                return Err(ResolveError::Resolution(Diagnostic::without_position(
                    ErrorCode::ImportNotFound,
                    root.display().to_string(),
                    "resolution root not found",
                )));
            }
        },
        None => None,
    };

    let mut resolver = Resolver {
        loader,
        root,
        max_bytes: non_zero_or(options.max_bytes, DEFAULT_MAX_RESOLVE_BYTES),
        max_files: if options.max_files == 0 {
            DEFAULT_MAX_RESOLVE_FILES
        } else {
            options.max_files
        },
        max_nodes: if options.max_nodes == 0 {
            DEFAULT_MAX_RESOLVE_NODES
        } else {
            options.max_nodes
        },
        remaining_work: if options.max_merge_work == 0 {
            DEFAULT_MAX_RESOLVE_MERGE_WORK
        } else {
            options.max_merge_work
        },
        bytes: 0,
        files: 0,
        nodes: 0,
        chain: Vec::new(),
        done: HashMap::new(),
    };
    let resolved = resolver.load(entry.as_ref(), None)?;
    let mut document = resolved.document;
    document.children = materialize_nodes(&document.children);
    document.imports_resolved = true;
    Ok(document)
}

fn non_zero_or(value: u64, default: u64) -> u64 {
    if value == 0 {
        default
    } else {
        value
    }
}

struct Resolver<'a> {
    loader: &'a dyn Loader,
    root: Option<PathBuf>,
    max_bytes: u64,
    max_files: usize,
    max_nodes: usize,
    remaining_work: u64,
    bytes: u64,
    files: usize,
    nodes: usize,
    /// Canonical paths of files on the chain currently being resolved - not
    /// every file ever loaded. Entries are removed on the way out, so a
    /// diamond is legal and its shared file is reused.
    chain: Vec<PathBuf>,
    /// Files fully resolved during this call, keyed by canonical path.
    ///
    /// Without it, resolution is exponential in the depth of a diamond-shaped
    /// import graph. The cache holds only completed files, and a completed
    /// file has already been popped off the chain, so a hit can never be a
    /// file still being resolved: memoization cannot mask a cycle.
    done: HashMap<PathBuf, Resolved>,
}

impl<'a> Resolver<'a> {
    fn diagnostic(
        &self,
        origin: Option<&Origin>,
        path: &Path,
        code: ErrorCode,
        message: impl Into<String>,
    ) -> ResolveError {
        let message = message.into();
        match origin {
            Some(origin) => ResolveError::Resolution(Diagnostic::new(
                code,
                origin.path.clone(),
                origin.position.line,
                origin.position.col,
                message,
            )),
            // Only a failure on the entry file itself, which no import
            // statement named, has no position to report.
            None => ResolveError::Resolution(Diagnostic::without_position(
                code,
                path.display().to_string(),
                message,
            )),
        }
    }

    fn chain_string(&self, target: &Path) -> String {
        let mut out = String::new();
        for path in &self.chain {
            out.push_str(&path.display().to_string());
            out.push_str(" -> ");
        }
        out.push_str(&target.display().to_string());
        out
    }

    fn load(&mut self, path: &Path, origin: Option<&Origin>) -> Result<Resolved, ResolveError> {
        let canonical = self.loader.canonicalize(path).map_err(|_| {
            let message = format!("cannot resolve imported file: {}", self.chain_string(path));
            self.diagnostic(origin, path, ErrorCode::ImportNotFound, message)
        })?;
        if let Some(root) = &self.root {
            if !path_within_root(root, &canonical) {
                let message = format!("resolved path is outside root: {}", canonical.display());
                return Err(self.diagnostic(
                    origin,
                    &canonical,
                    ErrorCode::PathOutsideRoot,
                    message,
                ));
            }
        }
        if let Some(cached) = self.done.get(&canonical) {
            if self.chain.len() + cached.depth > MAX_IMPORT_DEPTH {
                let message = format!(
                    "import chain too deep through cached file (max {MAX_IMPORT_DEPTH}): {}",
                    self.chain_string(path),
                );
                return Err(self.diagnostic(origin, path, ErrorCode::ImportChainTooDeep, message));
            }
            return Ok(Resolved {
                document: cached.document.clone(),
                depth: cached.depth,
            });
        }
        if self.chain.contains(&canonical) {
            let message = format!("circular import: {}", self.chain_string(path));
            return Err(self.diagnostic(origin, &canonical, ErrorCode::CircularImport, message));
        }
        if self.chain.len() > MAX_IMPORT_DEPTH {
            let message = format!(
                "import chain too deep (max {MAX_IMPORT_DEPTH}): {}",
                self.chain_string(path),
            );
            return Err(self.diagnostic(origin, path, ErrorCode::ImportChainTooDeep, message));
        }
        if self.files >= self.max_files {
            let message = format!("resolution file limit exceeded (max {})", self.max_files);
            return Err(self.diagnostic(
                origin,
                &canonical,
                ErrorCode::ResolutionFileLimit,
                message,
            ));
        }
        self.files += 1;
        self.chain.push(canonical.clone());

        let source = self.loader.read(&canonical).map_err(|_| {
            let message = format!("cannot read imported file: {}", self.chain_string(path));
            self.diagnostic(origin, &canonical, ErrorCode::ImportNotFound, message)
        })?;
        if source.len() as u64 > self.max_bytes.saturating_sub(self.bytes) {
            let message = format!("resolution byte limit exceeded (max {})", self.max_bytes);
            return Err(self.diagnostic(
                origin,
                &canonical,
                ErrorCode::ResolutionByteLimit,
                message,
            ));
        }
        self.bytes += source.len() as u64;

        let canonical_string = canonical.display().to_string();
        let mut document = parse_bytes(&source, canonical_string.clone()).map_err(|error| {
            if origin.is_none() {
                ResolveError::Parse(error.diagnostic)
            } else {
                // The diagnostic already names the failing file and position;
                // only the human message gains the route that reached it.
                let mut diagnostic = error.diagnostic;
                diagnostic
                    .message
                    .push_str(&format!(" (import chain: {})", self.chain_string(path)));
                ResolveError::Parse(diagnostic)
            }
        })?;

        let count = count_nodes(&document.children);
        if count > self.max_nodes.saturating_sub(self.nodes) {
            let message = format!("resolution node limit exceeded (max {})", self.max_nodes);
            return Err(self.diagnostic(
                origin,
                &canonical,
                ErrorCode::ResolutionNodeLimit,
                message,
            ));
        }
        self.nodes += count;

        let mut depth = 0;
        if !document.import_paths.is_empty() {
            let directory = canonical
                .parent()
                .unwrap_or_else(|| Path::new("."))
                .to_path_buf();
            let mut merged: Vec<crate::model::Node> = Vec::new();
            for (i, import_path) in document.import_paths.iter().enumerate() {
                let child_path = directory.join(import_path);
                let import_origin = Origin {
                    path: canonical_string.clone(),
                    position: document.import_positions[i],
                };
                let imported = self.load(&child_path, Some(&import_origin))?;
                depth = depth.max(1 + imported.depth);
                let mut remaining = Some(self.remaining_work);
                match merge_nodes_budget(
                    std::mem::take(&mut merged),
                    imported.document.children,
                    &mut remaining,
                ) {
                    Ok(merged_nodes) => {
                        merged = merged_nodes;
                        self.remaining_work = remaining.expect("set before merge");
                    }
                    Err(()) => {
                        let message =
                            "resolution merge work limit exceeded (max work units)".to_string();
                        return Err(self.diagnostic(
                            Some(&import_origin),
                            &canonical,
                            ErrorCode::ResolutionWorkLimit,
                            message,
                        ));
                    }
                }
            }
            let mut remaining = Some(self.remaining_work);
            match merge_nodes_budget(
                merged,
                std::mem::take(&mut document.children),
                &mut remaining,
            ) {
                Ok(children) => {
                    document.children = children;
                    self.remaining_work = remaining.expect("set before merge");
                }
                Err(()) => {
                    let message = "resolution merge work limit exceeded".to_string();
                    return Err(self.diagnostic(
                        origin,
                        &canonical,
                        ErrorCode::ResolutionWorkLimit,
                        message,
                    ));
                }
            }
        }

        self.done.insert(
            canonical,
            Resolved {
                document: document.clone(),
                depth,
            },
        );
        // Pop the file off the chain only after it is fully resolved, so a
        // diamond is legal and a cycle is never masked by the cache.
        self.chain.pop();
        Ok(Resolved { document, depth })
    }
}

/// Whether `target` is `root` itself or lies underneath it, comparing path
/// components so `/root2/x` never matches root `/root`.
fn path_within_root(root: &Path, target: &Path) -> bool {
    let mut root_components = root.components();
    let mut target_components = target.components();
    loop {
        match (root_components.next(), target_components.next()) {
            // The whole root matched: the target is inside or equal.
            (None, _) => return true,
            (Some(r), Some(t)) if r == t => continue,
            _ => return false,
        }
    }
}

/// Named nodes plus every recursively nested value, matching the Go and Zig
/// counters so identical budgets accept identical graphs.
fn count_nodes(nodes: &[crate::model::Node]) -> usize {
    use crate::model::Node;
    let mut total = nodes.len();
    for node in nodes {
        match node {
            Node::Field(field) => total += count_value_nodes(&field.value),
            Node::Block(block) => total += count_nodes(&block.children),
            Node::BlockArray(block_array) => {
                for value in &block_array.items {
                    total += count_value_nodes(value);
                }
            }
            Node::Delete(_) | Node::None => {}
        }
    }
    total
}

fn count_value_nodes(value: &crate::model::Value) -> usize {
    use crate::model::Value;
    let mut total = 1;
    match value {
        Value::Object(object) => total += count_nodes(&object.children),
        Value::Array(array) => {
            for item in &array.items {
                total += count_value_nodes(item);
            }
        }
        _ => {}
    }
    total
}
