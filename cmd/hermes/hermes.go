package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"cloud.google.com/go/storage"
	"github.com/ahmedtd/hermes/lib/gcssessionservice"
	"github.com/ahmedtd/hermes/lib/tools/celtool"
	"github.com/ahmedtd/hermes/lib/tools/jstool"
	"github.com/ahmedtd/hermes/lib/tools/sessionstate"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

var (
	stateBucket = flag.String("state-bucket", "", "The GCS bucket to store the agent's persistent state")
	adkFlags    = full.DefineFlags()
)

func main() {
	ctx := context.Background()

	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	model, err := gemini.NewModel(ctx, "gemini-2.5-pro", &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  "row-major-hermes",
		Location: "us-west1",
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error creating model", slog.Any("err", err))
		os.Exit(1)
	}

	tools := []tool.Tool{}

	setStateTool, getStateTool, err := sessionstate.Tools()
	if err != nil {
		slog.ErrorContext(ctx, "Error creating get/set state tools", slog.Any("err", err))
		os.Exit(1)
	}
	tools = append(tools, setStateTool, getStateTool)

	// execTools, err := exectool.Tools()
	// if err != nil {
	// 	slog.ErrorContext(ctx, "Error creating execution tools", slog.Any("err", err))
	// 	os.Exit(1)
	// }
	// tools = append(tools, execTools...)

	celTools, err := celtool.Tools()
	if err != nil {
		slog.ErrorContext(ctx, "Error creating CEL tools", slog.Any("err", err))
		os.Exit(1)
	}
	tools = append(tools, celTools...)

	jsTools, err := jstool.Tools()
	if err != nil {
		slog.ErrorContext(ctx, "Error creating JS tools", slog.Any("err", err))
		os.Exit(1)
	}
	tools = append(tools, jsTools...)

	hermesAgent, err := llmagent.New(llmagent.Config{
		Name:        "hermes",
		Model:       model,
		Description: "A general purpose agent extended with tools.",
		Instruction: "You are a helpful assistant, codenamed Hermes, used for evaluating the construction of agents.",
		Tools:       tools,
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error creating root agent", slog.Any("err", err))
		os.Exit(1)
	}

	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Error creating storage client", slog.Any("err", err))
		os.Exit(1)
	}

	sessionService := gcssessionservice.New(gcsClient, *stateBucket)

	config := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(hermesAgent),
		SessionService: sessionService,
	}

	if err = full.Run(ctx, adkFlags, config); err != nil {
		slog.ErrorContext(ctx, "Error executing ADK launcher", slog.Any("err", err))
		os.Exit(1)
	}
}
