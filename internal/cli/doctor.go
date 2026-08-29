package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/aduverger/madeleine/internal/madeleine"
	"github.com/aduverger/madeleine/internal/rpc"
)

type DoctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type DoctorResult struct {
	Checks []DoctorCheck `json:"checks"`
}

func runDoctor(
	ctx context.Context,
	args []string,
	output, diagnostics io.Writer,
	home string,
	build BuildInfo,
) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	jsonOutput := flags.Bool("json", false, "emit structured JSON")
	repositoryPath := flags.String("repo", ".", "repository path")
	flags.Usage = func() {
		fmt.Fprintln(diagnostics, "usage: madeleine doctor [--json] [--repo <path>]")
	}
	if err := flags.Parse(args); err != nil {
		return exitInvalidInvocation
	}
	if flags.NArg() != 0 {
		return invalidInvocation(diagnostics, "doctor accepts no positional arguments")
	}
	return executeDoctor(ctx, *repositoryPath, *jsonOutput, output, diagnostics, home, build)
}

func executeDoctor(
	ctx context.Context,
	repositoryPath string,
	jsonOutput bool,
	output, diagnostics io.Writer,
	home string,
	build BuildInfo,
) int {
	checks, service := runDoctorChecks(ctx, repositoryPath, home, build)
	status := doctorStatus(checks)

	var outputError error
	if jsonOutput {
		outputError = rpc.WriteSuccess(output, DoctorResult{Checks: checks})
	} else {
		outputError = writeHumanChecks(output, checks)
	}
	if outputError != nil {
		fmt.Fprintf(diagnostics, "write doctor response: %v\n", outputError)
		status = exitOperationFailure
	}
	if service != nil {
		if err := service.Close(); err != nil {
			fmt.Fprintf(diagnostics, "close Madeleine service: %v\n", err)
			status = exitOperationFailure
		}
	}
	return status
}

func runDoctorChecks(
	ctx context.Context,
	repositoryPath string,
	home string,
	build BuildInfo,
) ([]DoctorCheck, *madeleine.Service) {
	checks := []DoctorCheck{{Name: "binary_version", OK: true, Detail: formatVersion(build)}}
	options := madeleine.Options{Home: home}
	if err := madeleine.CheckDataDirectory(options); err != nil {
		checks = append(checks, DoctorCheck{Name: "data_directory", Detail: "not accessible"})
	} else {
		checks = append(checks, DoctorCheck{Name: "data_directory", OK: true, Detail: "accessible"})
	}

	service, openErr := madeleine.Open(ctx, options)
	if openErr != nil {
		checks = append(checks,
			DoctorCheck{Name: "application", Detail: "initialization failed"},
			DoctorCheck{Name: "schema_version", Detail: "unavailable"},
		)
	} else {
		checks = append(checks, DoctorCheck{Name: "application", OK: true, Detail: "initialized"})
		version, err := service.SchemaVersion(ctx)
		if err != nil {
			checks = append(checks, DoctorCheck{Name: "schema_version", Detail: "unavailable"})
		} else {
			checks = append(checks, DoctorCheck{
				Name: "schema_version", OK: true, Detail: fmt.Sprintf("version %d", version),
			})
		}
	}

	checks = append(checks, checkGitExecutable(ctx))
	checks = append(checks, checkRepository(ctx, service, repositoryPath))
	return checks, service
}

func checkGitExecutable(ctx context.Context) DoctorCheck {
	output, err := exec.CommandContext(ctx, "git", "--version").Output()
	if err != nil {
		return DoctorCheck{Name: "git_executable", Detail: "unavailable"}
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = "available"
	}
	return DoctorCheck{Name: "git_executable", OK: true, Detail: detail}
}

func checkRepository(ctx context.Context, service *madeleine.Service, repositoryPath string) DoctorCheck {
	check := DoctorCheck{Name: "repository"}
	if service == nil {
		check.Detail = "application unavailable"
		return check
	}
	if _, err := service.ResolveRepository(ctx, repositoryPath); err != nil {
		check.Detail = rpc.SafeMessage(err)
		return check
	}
	check.OK = true
	check.Detail = "resolved"
	return check
}

func doctorStatus(checks []DoctorCheck) int {
	for _, check := range checks {
		if !check.OK {
			return exitOperationFailure
		}
	}
	return exitSuccess
}

func writeHumanChecks(output io.Writer, checks []DoctorCheck) error {
	for _, check := range checks {
		status := "ok"
		if !check.OK {
			status = "failed"
		}
		if _, err := fmt.Fprintf(output, "%s: %s - %s\n", check.Name, status, check.Detail); err != nil {
			return err
		}
	}
	return nil
}
