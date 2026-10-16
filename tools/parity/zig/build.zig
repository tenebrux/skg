const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const skg_dep = b.dependency("skg", .{ .target = target, .optimize = optimize });

    const exe = b.addExecutable(.{
        .name = "skg-parity",
        .root_module = b.createModule(.{
            .root_source_file = b.path("main.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{.{ .name = "skg", .module = skg_dep.module("skg") }},
        }),
    });
    b.installArtifact(exe);
}
