// Package celtool is a marginally better idea than
package celtool

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func Tools() ([]tool.Tool, error) {
	tools := []tool.Tool{}

	celTool, err := functiontool.New(
		functiontool.Config{
			Name:        "executeCEL",
			Description: description,
		},
		execCEL,
	)
	if err != nil {
		return nil, fmt.Errorf("while creating executeCEL tool: %w", err)
	}
	tools = append(tools, celTool)

	return tools, nil
}

const description = `
Execute CEL (Common Expression Language) code locally.

CEL is fast, but limited.  It has no loops, or recursion, so to do things
like calculate the N-th Fibonnacci number, you may need to manually unroll
your code N times.
`

type celArgs struct {
	CELCode string
}

type celResult struct {
	CompilationErrors []*cel.Error
	Result            any
}

func execCEL(ctx tool.Context, args celArgs) (celResult, error) {
	env, err := cel.NewEnv()
	if err != nil {
		return celResult{}, fmt.Errorf("while creating CEL env: %w", err)
	}

	ast, issues := env.Compile(args.CELCode)
	if issues.Err() != nil {
		return celResult{
			CompilationErrors: issues.Errors(),
		}, nil
	}

	prg, err := env.Program(ast)
	if err != nil {
		return celResult{}, fmt.Errorf("while creating CEL program: %w", err)
	}

	out, _, err := prg.Eval(map[string]any{})
	if err != nil {
		return celResult{}, fmt.Errorf("while executing CEL program: %w", err)
	}

	return celResult{
		CompilationErrors: []*cel.Error{},
		Result:            out,
	}, nil
}
