package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/aduverger/madeleine/internal/rpc"
)

const (
	exitSuccess           = 0
	exitOperationFailure  = 1
	exitInvalidInvocation = 2
)

func Main(build BuildInfo) int {
	return run(
		context.Background(),
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
		os.Getenv,
		currentBuildInfo(build.Version, build.Commit),
	)
}

func run(
	ctx context.Context,
	args []string,
	input io.Reader,
	output, diagnostics io.Writer,
	getenv func(string) string,
	build BuildInfo,
) int {
	if len(args) == 0 {
		return invalidInvocation(diagnostics, "command is required")
	}

	home := getenv("MADELEINE_HOME")
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return invalidInvocation(diagnostics, "version accepts no arguments")
		}
		return runVersion(output, diagnostics, build)
	case "rpc":
		if len(args) != 2 {
			return invalidInvocation(diagnostics, "rpc requires exactly one method")
		}
		outcome := rpc.Run(ctx, args[1], input, output, diagnostics, rpc.Config{Home: home})
		return rpcExitCode(outcome)
	case "doctor":
		return runDoctor(ctx, args[1:], output, diagnostics, home, build)
	default:
		return invalidInvocation(diagnostics, fmt.Sprintf("unknown command %q", args[0]))
	}
}

func rpcExitCode(outcome rpc.Outcome) int {
	switch outcome {
	case rpc.OutcomeSuccess:
		return exitSuccess
	case rpc.OutcomeInvalidRequest:
		return exitInvalidInvocation
	default:
		return exitOperationFailure
	}
}

func invalidInvocation(diagnostics io.Writer, message string) int {
	fmt.Fprintf(diagnostics, "madeleine: %s\n", message)
	return exitInvalidInvocation
}
