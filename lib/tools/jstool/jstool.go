// Package jstool is an ADK tool for local JavaScript execution using
// github.com/dop251/goja.
package jstool

import (
	"fmt"

	"github.com/dop251/goja"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func Tools() ([]tool.Tool, error) {
	tools := []tool.Tool{}

	jsTool, err := functiontool.New(
		functiontool.Config{
			Name:        "executeJavaScript",
			Description: description,
		},
		execJS,
	)
	if err != nil {
		return nil, fmt.Errorf("while creating executeJavaScript tool: %w", err)
	}
	tools = append(tools, jsTool)
	return tools, nil
}

const description = `
Execute JavaScript code using a local sandboxed interpreter.

The interpreter is goja (github.com/dop251/goja).  It implements most of
ECMAScript 5.1, but it is a standalone interpreter, so many features of
browsers or NodeJS are unsupported.  In particular, there is no way to make
network requests or read/write files.

The return value of the tool is the value of the last statement in the script
you submit.
`

type jsArgs struct {
	JSCode string
}

type jsResult struct {
	ExecutionError string
	Result         any
}

func execJS(ctx tool.Context, args jsArgs) (jsResult, error) {
	vm := goja.New()

	val, err := vm.RunString(args.JSCode)
	if err != nil {
		return jsResult{
			ExecutionError: err.Error(),
		}, nil
	}

	return jsResult{
		Result: val.Export(),
	}, nil
}
