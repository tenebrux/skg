const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const skg_mod = b.addModule("skg", .{
        .root_source_file = b.path("zig/root.zig"),
        .target = target,
        .optimize = optimize,
    });

    const exe = b.addExecutable(.{
        .name = "skg",
        .root_module = b.createModule(.{
            .root_source_file = b.path("zig/main.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "skg", .module = skg_mod },
            },
        }),
    });
    b.installArtifact(exe);

    const tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("zig/tests.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    const run_tests = b.addRunArtifact(tests);

    const malformed_tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("zig/malformed_test.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    const run_malformed_tests = b.addRunArtifact(malformed_tests);

    const conformance_tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("zig/conformance_test.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    const run_conformance_tests = b.addRunArtifact(conformance_tests);

    const formatter_tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("zig/main.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{.{ .name = "skg", .module = skg_mod }},
        }),
    });
    const run_formatter_tests = b.addRunArtifact(formatter_tests);

    const test_step = b.step("test", "Run SKG parser tests");
    test_step.dependOn(&run_formatter_tests.step);
    test_step.dependOn(&run_tests.step);
    test_step.dependOn(&run_malformed_tests.step);
    test_step.dependOn(&run_conformance_tests.step);
}
