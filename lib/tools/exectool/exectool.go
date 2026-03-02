// Package exectool is a bad idea.
package exectool

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func Tools() ([]tool.Tool, error) {
	tools := []tool.Tool{}

	execTool, err := functiontool.New(
		functiontool.Config{
			Name:                "execLocalSynchronous",
			Description:         execLocalSynchronousDescription,
			RequireConfirmation: true,
			// TODO: Tool Confirmation with ADK Web UI is busted.  The Web UI
			// sends the JSON key "function_response", but the Golang type JSON
			// tag says it should be functionResponse.
		},
		execLocalSynchronous,
	)
	if err != nil {
		return nil, fmt.Errorf("while creating execLocalSynchronous tool: %w", err)
	}
	tools = append(tools, execTool)

	return tools, nil
}

const execLocalSynchronousDescription = `
Run a command locally in the same environment as the main agent process.

The interface is a direct invocation of a binary, similar to the exec()
system call.  If you want to run bash (or other shell), or python code,
you will have to explicitly invoke the correct executable.
`

type execLocalSynchronousArgs struct {
	Name  string
	Args  []string
	Stdin string
}

type execLocalSynchronousResult struct {
	StdOut   string
	StdErr   string
	ExitCode int
}

func execLocalSynchronous(ctx tool.Context, args execLocalSynchronousArgs) (execLocalSynchronousResult, error) {
	slog.InfoContext(ctx, "execLocalSynchronous", slog.Any("args", args))

	cmd := exec.Command(args.Name, args.Args...)

	stdin := strings.NewReader(args.Stdin)
	cmd.Stdin = stdin

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if _, ok := errors.AsType[*exec.ExitError](err); ok {
		// This is OK, fall through to the main return.
	} else if err != nil {
		return execLocalSynchronousResult{}, fmt.Errorf("while executing command: %w", err)
	}

	return execLocalSynchronousResult{
		StdOut:   stdout.String(),
		StdErr:   stderr.String(),
		ExitCode: cmd.ProcessState.ExitCode(),
	}, nil
}
