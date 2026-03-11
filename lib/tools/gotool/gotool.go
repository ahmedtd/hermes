// Package gotool provides an ADK tool enabling local execution of Go code by
// compiling to the wasip1 target, then executing under WASM using wazero.
package gotool

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func Tools() ([]tool.Tool, error) {
	tools := []tool.Tool{}

	goTool, err := functiontool.New(
		functiontool.Config{
			Name:        "executeGo",
			Description: description,
		},
		execGo,
	)
	if err != nil {
		return nil, fmt.Errorf("while creating executeGo tool: %w", err)
	}
	tools = append(tools, goTool)
	return tools, nil
}

const description = `
Execute Go code by transpiling to WASM, targeting wasi1p.

You provide a single file's worth of Go code.  It is cross-compiled using
GOOS=wasip1 GOARCH=wasm, then loaded and executed in-process with wazero
(github.com/tetratelabs/wazero).
`

type execArgs struct {
	GoCode  string
	GoStdin string
}

type execResult struct {
	Status         string
	CompilerStdout string
	CompilerStderr string
	GoStdout       string
	GoStderr       string
}

func execGo(ctx tool.Context, args execArgs) (execResult, error) {
	slog.InfoContext(ctx, "execGo", slog.Any("args", args))

	tmpDir, err := os.MkdirTemp("", "hermes-gotool-*")
	if err != nil {
		return execResult{}, fmt.Errorf("while creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte(args.GoCode), 0o640); err != nil {
		return execResult{}, fmt.Errorf("while creating go file: %w", err)
	}

	cmd := exec.Command("go", "build", "-o", "main.wasm", "main.go")
	cmd.Dir = tmpDir
	cmd.Env = append(cmd.Env, "GOARCH=wasm")
	cmd.Env = append(cmd.Env, "GOOS=wasip1")
	cmd.Env = append(cmd.Env, "GOCACHE="+filepath.Join(tmpDir, "go-cache"))

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	if _, ok := errors.AsType[*exec.ExitError](err); ok {
		return execResult{
			Status:         "CompilationFailed",
			CompilerStdout: stdout.String(),
			CompilerStderr: stderr.String(),
		}, nil
	} else if err != nil {
		return execResult{}, fmt.Errorf("while executing compiler: %w", err)
	}

	mainWasm, err := os.ReadFile(filepath.Join(tmpDir, "main.wasm"))
	if err != nil {
		return execResult{}, fmt.Errorf("while reading wasm file: %w", err)
	}

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	wasiStdin := strings.NewReader(args.GoStdin)
	wasiStdout := &strings.Builder{}
	wasiStderr := &strings.Builder{}

	wasiCfg := wazero.NewModuleConfig()
	wasiCfg = wasiCfg.WithStdin(wasiStdin)
	wasiCfg = wasiCfg.WithStdout(wasiStdout)
	wasiCfg = wasiCfg.WithStderr(wasiStderr)

	_, err = r.InstantiateWithConfig(ctx, mainWasm, wasiCfg)
	if err != nil {
		return execResult{}, fmt.Errorf("while instantiating WASM module: %w", err)
	}

	return execResult{
		Status:   "Success",
		GoStdout: wasiStdout.String(),
		GoStderr: wasiStderr.String(),
	}, nil

}
