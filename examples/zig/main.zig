// Example: load SKG directly into native Zig structs.
// Run from examples/zig with: zig build run
const std = @import("std");
const skg = @import("skg");

// Native fields and defaults supply the application schema.
const Streams = struct {
    stdout: bool = false,
    file: bool = false,
    syslog: bool = false,
};

const Logging = struct {
    level: []const u8 = "info",
    max_size_mb: i64 = 5,
    keep_rotations: i64 = 3,
    streams: Streams = .{},
};

const Database = struct {
    host: []const u8 = "localhost",
    port: i64 = 5432,
    name: []const u8 = "",
    max_connections: i64 = 10,
    ssl: bool = false,
};

const Config = struct {
    name: []const u8 = "",
    port: i64 = 0,
    debug: bool = false,
    motd: []const u8 = "",
    database: Database = .{},
    logging: Logging = .{},
};

pub fn main() void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    var result = skg.decodeFile(Config, allocator, "../app.skg", .{});
    defer result.deinit();
    const cfg = result.value orelse {
        if (result.diagnostic) |d| {
            std.debug.print("{s}:{d}:{d} {s}: {s}\n", .{
                d.source.path, d.source.line, d.source.col, d.field_path, d.message,
            });
        }
        return;
    };

    // ── Use it ─────────────────────────────────────────────────────────
    const p = std.debug.print;
    p("name:         {s}\n", .{cfg.name});
    p("port:         {d}\n", .{cfg.port});
    p("debug:        {}\n", .{cfg.debug});
    p("db:           {s}:{d}/{s} (ssl={}, pool={d})\n", .{
        cfg.database.host,
        cfg.database.port,
        cfg.database.name,
        cfg.database.ssl,
        cfg.database.max_connections,
    });
    p("log level:    {s}\n", .{cfg.logging.level});
    p("log streams:  stdout={} file={} syslog={}\n", .{
        cfg.logging.streams.stdout,
        cfg.logging.streams.file,
        cfg.logging.streams.syslog,
    });
    p("motd:\n{s}\n", .{cfg.motd});
}
