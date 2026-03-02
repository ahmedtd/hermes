package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"slices"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/ahmedtd/hermes/lib/gcssessionservice"
	"github.com/ahmedtd/hermes/lib/tools/exectool"
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
)

func main() {
	ctx := context.Background()

	flag.Parse()
	slog.SetLogLoggerLevel(slog.LevelDebug)

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

	execTools, err := exectool.Tools()
	if err != nil {
		slog.ErrorContext(ctx, "Error creating execution tools", slog.Any("err", err))
		os.Exit(1)
	}
	tools = append(tools, execTools...)

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

	// ADK's built-in launchers explode if they are handed arg lists with flags
	// they don't understand.  This is a problem if you want to have additional
	// flags in your own main.
	filteredArgs := os.Args[1:]
	filteredArgs = slices.DeleteFunc(filteredArgs, func(x string) bool {
		return strings.HasPrefix(x, "-state-bucket") || strings.HasPrefix(x, "--state-bucket")
	})

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, filteredArgs); err != nil {
		slog.ErrorContext(ctx, "Error executing ADK launcher", slog.Any("err", err), slog.String("syntax", l.CommandLineSyntax()))
		os.Exit(1)
	}
}
