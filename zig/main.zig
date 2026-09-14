const std = @import("std");
const skg = @import("skg");

const usage =
    \\Usage: skg <command> [options] [files...]
    \\
    \\Commands:
    \\  fmt    Format SKG files
    \\
    \\Options:
    \\  --check    Check if files are formatted (exit 1 if not)
    \\  --stdin    Read from stdin, write to stdout
    \\  -h, --help Show this help
    \\
;

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);

    const stderr: std.fs.File = .stderr();
    const stdout: std.fs.File = .stdout();

    if (args.len < 2) {
        try stderr.writeAll(usage);
        std.process.exit(1);
    }

    if (std.mem.eql(u8, args[1], "fmt")) {
        std.process.exit(try fmtCommand(allocator, args[2..]));
    } else if (std.mem.eql(u8, args[1], "-h") or std.mem.eql(u8, args[1], "--help")) {
        try stdout.writeAll(usage);
    } else {
        var buf: [256]u8 = undefined;
        const msg = std.fmt.bufPrint(&buf, "unknown command: {s}\n", .{args[1]}) catch "unknown command\n";
        try stderr.writeAll(msg);
        try stderr.writeAll(usage);
        std.process.exit(1);
    }
}

fn fmtCommand(allocator: std.mem.Allocator, args: []const []const u8) !u8 {
    var check = false;
    var use_stdin = false;
    var files: std.ArrayListUnmanaged([]const u8) = .empty;

    for (args) |arg| {
        if (std.mem.eql(u8, arg, "--check")) {
            check = true;
        } else if (std.mem.eql(u8, arg, "--stdin")) {
            use_stdin = true;
        } else if (std.mem.eql(u8, arg, "-h") or std.mem.eql(u8, arg, "--help")) {
            const stdout: std.fs.File = .stdout();
            try stdout.writeAll(usage);
            return 0;
        } else if (std.mem.startsWith(u8, arg, "-")) {
            const stderr: std.fs.File = .stderr();
            var buf: [256]u8 = undefined;
            const msg = std.fmt.bufPrint(&buf, "unknown flag: {s}\n", .{arg}) catch "unknown flag\n";
            try stderr.writeAll(msg);
            return 1;
        } else {
            try files.append(allocator, arg);
        }
    }

    if (use_stdin) {
        return try fmtStdin(allocator, check);
    }

    if (files.items.len == 0) {
        const stderr: std.fs.File = .stderr();
        try stderr.writeAll("skg fmt: no files specified\n");
        return 1;
    }

    var any_changed: bool = false;
    for (files.items) |path| {
        const changed = fmtFile(allocator, path, check) catch |err| {
            const stderr: std.fs.File = .stderr();
            var buf: [512]u8 = undefined;
            const msg = std.fmt.bufPrint(&buf, "{s}: {s}\n", .{ path, @errorName(err) }) catch "format error\n";
            try stderr.writeAll(msg);
            return 1;
        };
        if (changed) any_changed = true;
    }

    if (check and any_changed) return 1;
    return 0;
}

fn fmtStdin(allocator: std.mem.Allocator, check: bool) !u8 {
    const stdin: std.fs.File = .stdin();
    const src = try stdin.readToEndAlloc(allocator, 10 * 1024 * 1024);
    defer allocator.free(src);

    var result = skg.parseSource(allocator, src, "<stdin>");
    defer result.deinit();

    if (result.file == null) {
        if (result.diagnostic) |d| {
            const stderr: std.fs.File = .stderr();
            var buf: [512]u8 = undefined;
            const msg = std.fmt.bufPrint(&buf, "<stdin>:{d}:{d}: {s}\n", .{ d.line, d.col, d.message }) catch "parse error\n";
            try stderr.writeAll(msg);
        }
        return 1;
    }

    const formatted = try skg.emit.emitFile(allocator, result.file.?);
    defer allocator.free(formatted);

    if (check) {
        if (!std.mem.eql(u8, src, formatted)) {
            const stderr: std.fs.File = .stderr();
            try stderr.writeAll("<stdin>: not formatted\n");
            return 1;
        }
        return 0;
    }

    const stdout: std.fs.File = .stdout();
    try stdout.writeAll(formatted);
    return 0;
}

fn fmtFile(allocator: std.mem.Allocator, path: []const u8, check: bool) !bool {
    const file = try std.fs.cwd().openFile(path, .{});
    defer file.close();
    const src = try file.readToEndAlloc(allocator, 10 * 1024 * 1024);
    defer allocator.free(src);

    var result = skg.parseSource(allocator, src, path);
    defer result.deinit();

    if (result.file == null) {
        if (result.diagnostic) |d| {
            const stderr: std.fs.File = .stderr();
            var buf: [512]u8 = undefined;
            const msg = std.fmt.bufPrint(&buf, "{s}:{d}:{d}: {s}\n", .{ d.path, d.line, d.col, d.message }) catch "parse error\n";
            try stderr.writeAll(msg);
        }
        return error.ParseError;
    }

    const formatted = try skg.emit.emitFile(allocator, result.file.?);
    defer allocator.free(formatted);

    if (std.mem.eql(u8, src, formatted)) return false;

    if (check) {
        const stderr: std.fs.File = .stderr();
        try stderr.writeAll(path);
        try stderr.writeAll("\n");
        return true;
    }

    try writeAtomically(allocator, path, formatted);
    return true;
}

/// Replace `path` with `contents` via a temporary file in the same directory
/// and a rename.
///
/// Truncating the target and writing into it leaves a half-written config
/// behind if the process dies mid-write - and `skg fmt` runs over files people
/// have no other copy of. A rename within the same directory is atomic, so the
/// file is either the old text or the new text and never something in between.
fn writeAtomically(allocator: std.mem.Allocator, path: []const u8, contents: []const u8) !void {
    // Format the referent of a symlink, rather than replacing the link itself.
    const resolved = try std.fs.cwd().realpathAlloc(allocator, path);
    defer allocator.free(resolved);
    const stat = try std.fs.cwd().statFile(resolved);
    var atomic = try std.fs.cwd().atomicFile(resolved, .{ .mode = 0o600, .write_buffer = &.{} });
    defer atomic.deinit();
    try atomic.file_writer.file.writeAll(contents);
    // Set the exact mode after writing (umask and writes can strip mode bits).
    // A failure must leave the original untouched.
    if (std.fs.has_executable_bit) try atomic.file_writer.file.chmod(stat.mode);
    try atomic.file_writer.file.sync();
    try atomic.finish();
}

test "formatter ignores predictable temporary symlinks and preserves target mode" {
    if (@import("builtin").os.tag == .windows) return error.SkipZigTest;
    const t = std.testing;
    var tmp = t.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(.{ .sub_path = "config.skg", .data = "value:1\n" });
    try tmp.dir.writeFile(.{ .sub_path = "unrelated", .data = "keep me" });
    try tmp.dir.symLink("unrelated", ".config.skg.skg-fmt.tmp", .{});
    const original = try tmp.dir.openFile("config.skg", .{});
    try original.chmod(0o640);
    original.close();
    const path = try tmp.dir.realpathAlloc(t.allocator, "config.skg");
    defer t.allocator.free(path);
    try t.expect(try fmtFile(t.allocator, path, false));
    const untouched = try tmp.dir.readFileAlloc(t.allocator, "unrelated", 100);
    defer t.allocator.free(untouched);
    try t.expectEqualStrings("keep me", untouched);
    try t.expectEqual(@as(std.fs.File.Mode, 0o640), (try tmp.dir.statFile("config.skg")).mode & 0o777);
    try t.expect(!try fmtFile(t.allocator, path, false));
}

test "formatter preserves a config symlink and formats its referent" {
    if (@import("builtin").os.tag == .windows) return error.SkipZigTest;
    const t = std.testing;
    var tmp = t.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(.{ .sub_path = "target.skg", .data = "value:1\n" });
    try tmp.dir.symLink("target.skg", "link.skg", .{});
    const dir = try tmp.dir.realpathAlloc(t.allocator, ".");
    defer t.allocator.free(dir);
    const path = try std.fs.path.join(t.allocator, &.{ dir, "link.skg" });
    defer t.allocator.free(path);
    try t.expect(try fmtFile(t.allocator, path, false));
    var buf: [128]u8 = undefined;
    try t.expectEqualStrings("target.skg", try tmp.dir.readLink("link.skg", &buf));
    const data = try tmp.dir.readFileAlloc(t.allocator, "target.skg", 100);
    defer t.allocator.free(data);
    try t.expectEqualStrings("value: 1\n", data);
}
