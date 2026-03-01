package sessionstate

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func Tools() (tool.Tool, tool.Tool, error) {
	setTool, err := functiontool.New(
		functiontool.Config{
			Name:        "setSessionState",
			Description: "Sets a fact in the session's state map.  Keys should be strings with no special characters",
		},
		setSessionState,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("while creating getSessionState function tool: %w", err)
	}

	getTool, err := functiontool.New(
		functiontool.Config{
			Name:        "getSessionState",
			Description: "Gets a fact from the session's state map",
		},
		getSessionState,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("while creating getSessionState tool: %w", err)
	}

	return setTool, getTool, nil
}

type setSessionStateArgs struct {
	Key   string
	Value any
}

type setSessionStateResult struct {
}

func setSessionState(ctx tool.Context, args setSessionStateArgs) (setSessionStateResult, error) {
	if strings.HasPrefix(args.Key, "app:") {
		return setSessionStateResult{}, fmt.Errorf("setSessionState may not set app state")
	}
	if strings.HasPrefix(args.Key, "user:") {
		return setSessionStateResult{}, fmt.Errorf("setSessionState may not set user state")
	}
	if strings.HasPrefix(args.Key, "temp:") {
		return setSessionStateResult{}, fmt.Errorf("setSessionState may not set temp state")
	}

	ctx.State().Set(args.Key, args.Value)
	return setSessionStateResult{}, nil
}

type getSessionStateArgs struct {
	Key string
}

type getSessionStateResult struct {
	Value any
}

func getSessionState(ctx tool.Context, args getSessionStateArgs) (getSessionStateResult, error) {
	val, err := ctx.State().Get(args.Key)
	if err != nil {
		return getSessionStateResult{}, fmt.Errorf("while getting state: %w", err)
	}
	return getSessionStateResult{
		Value: val,
	}, nil
}
