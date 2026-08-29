package cli

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

type BuildInfo struct {
	Version string
	Commit  string
}

func currentBuildInfo(version, commit string) BuildInfo {
	info, ok := debug.ReadBuildInfo()
	return resolveBuildInfo(version, commit, info, ok)
}

func resolveBuildInfo(version, commit string, info *debug.BuildInfo, ok bool) BuildInfo {
	if version != "dev" || !ok || info == nil {
		return BuildInfo{Version: version, Commit: commit}
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = strings.TrimPrefix(info.Main.Version, "v")
	}
	if commit == "unknown" {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				commit = setting.Value
				break
			}
		}
	}
	return BuildInfo{Version: version, Commit: commit}
}

func formatVersion(build BuildInfo) string {
	if build.Commit == "" || build.Commit == "unknown" {
		return build.Version
	}
	return fmt.Sprintf("%s (%s)", build.Version, build.Commit)
}

func runVersion(output, diagnostics io.Writer, build BuildInfo) int {
	if _, err := fmt.Fprintln(output, formatVersion(build)); err != nil {
		fmt.Fprintf(diagnostics, "write version: %v\n", err)
		return exitOperationFailure
	}
	return exitSuccess
}
