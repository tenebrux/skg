const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const skg_dep = b.dependency("skg", .{ .target = target, .optimize = optimize });

    const tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("consumer_test.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{.{ .name = "skg", .module = skg_dep.module("skg") }},
        }),
    });
    const run = b.addRunArtifact(tests);
    const test_step = b.step("test", "Run the standalone SKG consumer tests");
    test_step.dependOn(&run.step);
}
