package cli

import (
	"runtime/debug"
	"testing"
)

func TestResolveBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.1.0"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}},
	}
	if got, want := resolveBuildInfo("dev", "unknown", info, true), (BuildInfo{Version: "0.1.0", Commit: "abc123"}); got != want {
		t.Fatalf("resolveBuildInfo() = %#v, want %#v", got, want)
	}
	if got := resolveBuildInfo("1.2.3", "release", info, true); got != (BuildInfo{Version: "1.2.3", Commit: "release"}) {
		t.Fatalf("explicit build changed: %#v", got)
	}
	if got, want := formatVersion(BuildInfo{Version: "0.1.0", Commit: "abc123"}), "0.1.0 (abc123)"; got != want {
		t.Fatalf("formatVersion() = %q, want %q", got, want)
	}
}
