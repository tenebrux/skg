const std = @import("std");
const skg = @import("skg");
const builtin = @import("builtin");

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
    // Resolve once, then keep using that referent. A symlink changed while the
    // formatter runs cannot redirect the eventual replacement elsewhere.
    const resolved = try std.fs.cwd().realpathAlloc(allocator, path);
    defer allocator.free(resolved);
    // realpath uses a nonblocking metadata handle on POSIX. Reject FIFOs and
    // devices before openFile performs a potentially blocking read.
    const path_snapshot = try std.fs.cwd().statFile(resolved);
    if (path_snapshot.kind != .file) return error.NotRegularFile;
    const file = try std.fs.cwd().openFile(resolved, .{});
    defer file.close();
    const snapshot = try file.stat();
    if (snapshot.kind != .file) return error.NotRegularFile;
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

    try ensureSingleLink(file);
    try writeAtomically(allocator, resolved, formatted, src, file, snapshot);
    return true;
}

/// Replace `path` with `contents` via a temporary file in the same directory
/// and a rename.
///
/// Truncating the target and writing into it leaves a half-written config
/// behind if the process dies mid-write - and `skg fmt` runs over files people
/// have no other copy of. A rename within the same directory is atomic, so the
/// file is either the old text or the new text and never something in between.
fn writeAtomically(
    allocator: std.mem.Allocator,
    resolved: []const u8,
    contents: []const u8,
    expected_contents: []const u8,
    original: std.fs.File,
    snapshot: std.fs.File.Stat,
) !void {
    var atomic = try std.fs.cwd().atomicFile(resolved, .{ .mode = 0o600, .write_buffer = &.{} });
    defer atomic.deinit();
    try atomic.file_writer.file.writeAll(contents);
    try copyMetadata(allocator, original, atomic.file_writer.file, snapshot);
    try atomic.file_writer.file.sync();
    // Reject an edit or metadata change observed after the bytes were read.
    // This narrows the unavoidable portable check/rename race to the final
    // rename operation instead of silently overwriting ordinary editor saves.
    try ensureUnchanged(allocator, resolved, expected_contents, snapshot);
    try atomic.finish();
}

fn sameSnapshot(a: std.fs.File.Stat, b: std.fs.File.Stat) bool {
    return a.inode == b.inode and a.kind == b.kind and a.size == b.size and
        a.mode == b.mode and a.mtime == b.mtime and a.ctime == b.ctime;
}

fn ensureUnchanged(allocator: std.mem.Allocator, path: []const u8, expected: []const u8, snapshot: std.fs.File.Stat) !void {
    const current = try std.fs.cwd().openFile(path, .{});
    defer current.close();
    const current_stat = try current.stat();
    if (!sameSnapshot(snapshot, current_stat)) return error.FileChanged;
    try ensureSingleLink(current);
    const data = current.readToEndAlloc(allocator, 10 * 1024 * 1024) catch return error.FileChanged;
    defer allocator.free(data);
    if (!std.mem.eql(u8, expected, data)) return error.FileChanged;
    if (!sameSnapshot(snapshot, try current.stat())) return error.FileChanged;
    try ensureSingleLink(current);
}

fn ensureSingleLink(file: std.fs.File) !void {
    if (builtin.os.tag == .windows) {
        const info = try windowsStandardInfo(file);
        if (info.NumberOfLinks != 1) return error.HardLinkedFile;
    } else if (builtin.os.tag != .wasi) {
        const info = try std.posix.fstat(file.handle);
        if (info.nlink != 1) return error.HardLinkedFile;
    }
}

fn copyMetadata(allocator: std.mem.Allocator, source: std.fs.File, destination: std.fs.File, snapshot: std.fs.File.Stat) !void {
    if (builtin.os.tag == .windows) {
        const info = try windowsBasicInfo(source);
        var io_status: std.os.windows.IO_STATUS_BLOCK = undefined;
        var basic = std.os.windows.FILE_BASIC_INFORMATION{
            .CreationTime = 0,
            .LastAccessTime = 0,
            .LastWriteTime = 0,
            .ChangeTime = 0,
            .FileAttributes = info.FileAttributes,
        };
        const rc = std.os.windows.ntdll.NtSetInformationFile(destination.handle, &io_status, &basic, @sizeOf(@TypeOf(basic)), .FileBasicInformation);
        if (rc != .SUCCESS) return error.MetadataCopyFailed;
        const copied = try windowsBasicInfo(destination);
        if (copied.FileAttributes != info.FileAttributes) return error.MetadataCopyFailed;
        return;
    }
    if (builtin.os.tag == .wasi) return;

    const source_posix = try std.posix.fstat(source.handle);
    const destination_posix = try std.posix.fstat(destination.handle);
    if (source_posix.uid != destination_posix.uid or source_posix.gid != destination_posix.gid) {
        try destination.chown(source_posix.uid, source_posix.gid);
    }
    // chown can clear set-id bits, so restore the complete mode afterwards.
    try destination.chmod(snapshot.mode);
    if (builtin.os.tag == .linux) try copyLinuxXattrs(allocator, source, destination);
    const copied = try destination.stat();
    const copied_posix = try std.posix.fstat(destination.handle);
    if (copied.mode != snapshot.mode or copied_posix.uid != source_posix.uid or copied_posix.gid != source_posix.gid) {
        return error.MetadataCopyFailed;
    }
}

fn windowsBasicInfo(file: std.fs.File) !std.os.windows.FILE_BASIC_INFORMATION {
    if (builtin.os.tag != .windows) unreachable;
    var io_status: std.os.windows.IO_STATUS_BLOCK = undefined;
    var info: std.os.windows.FILE_BASIC_INFORMATION = undefined;
    const rc = std.os.windows.ntdll.NtQueryInformationFile(file.handle, &io_status, &info, @sizeOf(@TypeOf(info)), .FileBasicInformation);
    return switch (rc) {
        .SUCCESS => info,
        else => error.MetadataReadFailed,
    };
}

fn windowsStandardInfo(file: std.fs.File) !std.os.windows.FILE_STANDARD_INFORMATION {
    if (builtin.os.tag != .windows) unreachable;
    var io_status: std.os.windows.IO_STATUS_BLOCK = undefined;
    var info: std.os.windows.FILE_STANDARD_INFORMATION = undefined;
    const rc = std.os.windows.ntdll.NtQueryInformationFile(file.handle, &io_status, &info, @sizeOf(@TypeOf(info)), .FileStandardInformation);
    return switch (rc) {
        .SUCCESS => info,
        else => error.MetadataReadFailed,
    };
}

const max_extended_attribute_bytes = 1024 * 1024;

fn copyLinuxXattrs(allocator: std.mem.Allocator, source: std.fs.File, destination: std.fs.File) !void {
    if (builtin.os.tag != .linux) unreachable;
    const linux = std.os.linux;
    var placeholder: [1]u8 = .{0};
    const names_size_raw = linux.flistxattr(source.handle, &placeholder, 0);
    if (std.posix.errno(names_size_raw) != .SUCCESS) return error.MetadataReadFailed;
    const names_size: usize = @intCast(names_size_raw);
    if (names_size > max_extended_attribute_bytes) return error.MetadataTooLarge;
    if (names_size == 0) return;
    const names = try allocator.alloc(u8, names_size);
    defer allocator.free(names);
    const names_read = linux.flistxattr(source.handle, names.ptr, names.len);
    if (std.posix.errno(names_read) != .SUCCESS or names_read != names.len) return error.MetadataChanged;

    var offset: usize = 0;
    var total_value_bytes: usize = 0;
    while (offset < names.len) {
        const end = std.mem.indexOfScalarPos(u8, names, offset, 0) orelse return error.MetadataReadFailed;
        const name = try allocator.dupeZ(u8, names[offset..end]);
        defer allocator.free(name);
        const value_size_raw = linux.fgetxattr(source.handle, name.ptr, &placeholder, 0);
        if (std.posix.errno(value_size_raw) != .SUCCESS) return error.MetadataChanged;
        const value_size: usize = @intCast(value_size_raw);
        if (value_size > max_extended_attribute_bytes -| total_value_bytes) return error.MetadataTooLarge;
        total_value_bytes += value_size;
        const value = try allocator.alloc(u8, value_size);
        defer allocator.free(value);
        const value_ptr: [*]u8 = if (value.len == 0) &placeholder else value.ptr;
        const value_read = linux.fgetxattr(source.handle, name.ptr, value_ptr, value.len);
        if (std.posix.errno(value_read) != .SUCCESS or value_read != value.len) return error.MetadataChanged;
        const set_result = linux.fsetxattr(destination.handle, name.ptr, value_ptr, value.len, 0);
        if (std.posix.errno(set_result) != .SUCCESS) return error.MetadataCopyFailed;
        offset = end + 1;
    }
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
    const original_identity = try std.posix.fstat(original.handle);
    original.close();
    const path = try tmp.dir.realpathAlloc(t.allocator, "config.skg");
    defer t.allocator.free(path);
    try t.expect(try fmtFile(t.allocator, path, false));
    const untouched = try tmp.dir.readFileAlloc(t.allocator, "unrelated", 100);
    defer t.allocator.free(untouched);
    try t.expectEqualStrings("keep me", untouched);
    try t.expectEqual(@as(std.fs.File.Mode, 0o640), (try tmp.dir.statFile("config.skg")).mode & 0o777);
    const rewritten = try tmp.dir.openFile("config.skg", .{});
    const rewritten_identity = try std.posix.fstat(rewritten.handle);
    rewritten.close();
    try t.expectEqual(original_identity.uid, rewritten_identity.uid);
    try t.expectEqual(original_identity.gid, rewritten_identity.gid);
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

test "formatter rejects a FIFO before opening it" {
    if (builtin.os.tag != .linux) return error.SkipZigTest;
    const t = std.testing;
    var tmp = t.tmpDir(.{});
    defer tmp.cleanup();
    const path = try tmp.dir.realpathAlloc(t.allocator, ".");
    defer t.allocator.free(path);
    const fifo_path = try std.fs.path.join(t.allocator, &.{ path, "config.skg" });
    defer t.allocator.free(fifo_path);
    const fifo_path_z = try t.allocator.dupeZ(u8, fifo_path);
    defer t.allocator.free(fifo_path_z);
    const rc = std.os.linux.mknod(fifo_path_z.ptr, std.os.linux.S.IFIFO | 0o600, 0);
    try t.expectEqual(std.posix.E.SUCCESS, std.posix.errno(rc));
    try t.expectError(error.NotRegularFile, fmtFile(t.allocator, fifo_path, false));
}

test "formatter refuses hard links before replacement" {
    if (builtin.os.tag == .windows or builtin.os.tag == .wasi) return error.SkipZigTest;
    const t = std.testing;
    var tmp = t.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(.{ .sub_path = "config.skg", .data = "value:1\n" });
    const source = try tmp.dir.realpathAlloc(t.allocator, "config.skg");
    defer t.allocator.free(source);
    const alias = try std.fs.path.join(t.allocator, &.{ std.fs.path.dirname(source).?, "alias.skg" });
    defer t.allocator.free(alias);
    try std.posix.link(source, alias);
    try t.expectError(error.HardLinkedFile, fmtFile(t.allocator, source, false));
    const source_data = try tmp.dir.readFileAlloc(t.allocator, "config.skg", 100);
    defer t.allocator.free(source_data);
    const alias_data = try tmp.dir.readFileAlloc(t.allocator, "alias.skg", 100);
    defer t.allocator.free(alias_data);
    try t.expectEqualStrings("value:1\n", source_data);
    try t.expectEqualStrings(source_data, alias_data);
}

test "formatter rejects a changed file and removes its temporary" {
    const t = std.testing;
    var tmp = t.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(.{ .sub_path = "config.skg", .data = "value:1\n" });
    const path = try tmp.dir.realpathAlloc(t.allocator, "config.skg");
    defer t.allocator.free(path);
    const original = try std.fs.cwd().openFile(path, .{});
    defer original.close();
    const snapshot = try original.stat();
    try tmp.dir.writeFile(.{ .sub_path = "config.skg", .data = "value:3\n" });
    try t.expectError(error.FileChanged, writeAtomically(t.allocator, path, "value: 1\n", "value:1\n", original, snapshot));
    const data = try tmp.dir.readFileAlloc(t.allocator, "config.skg", 100);
    defer t.allocator.free(data);
    try t.expectEqualStrings("value:3\n", data);
    var iterable = try tmp.dir.openDir(".", .{ .iterate = true });
    defer iterable.close();
    var iterator = iterable.iterate();
    var count: usize = 0;
    while (try iterator.next()) |_| count += 1;
    try t.expectEqual(@as(usize, 1), count);
}

test "formatter preserves Linux extended attributes" {
    if (builtin.os.tag != .linux) return error.SkipZigTest;
    const t = std.testing;
    var tmp = t.tmpDir(.{});
    defer tmp.cleanup();
    try tmp.dir.writeFile(.{ .sub_path = "config.skg", .data = "value:1\n" });
    const path = try tmp.dir.realpathAlloc(t.allocator, "config.skg");
    defer t.allocator.free(path);
    const file = try std.fs.cwd().openFile(path, .{});
    const name: [:0]const u8 = "user.skg-test";
    const value = "kept";
    const set_result = std.os.linux.fsetxattr(file.handle, name.ptr, value.ptr, value.len, 0);
    if (std.posix.errno(set_result) == .OPNOTSUPP) {
        file.close();
        return error.SkipZigTest;
    }
    try t.expectEqual(std.posix.E.SUCCESS, std.posix.errno(set_result));
    file.close();

    try t.expect(try fmtFile(t.allocator, path, false));
    const rewritten = try std.fs.cwd().openFile(path, .{});
    defer rewritten.close();
    var buffer: [32]u8 = undefined;
    const read_result = std.os.linux.fgetxattr(rewritten.handle, name.ptr, &buffer, buffer.len);
    try t.expectEqual(std.posix.E.SUCCESS, std.posix.errno(read_result));
    try t.expectEqualStrings(value, buffer[0..@intCast(read_result)]);
}
