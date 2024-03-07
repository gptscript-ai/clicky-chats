package main

import (
	"log/slog"
	"os"

	"github.com/acorn-io/cmd"
	"github.com/gptscript-ai/clicky-chats/pkg/cli"
)

func main() {
	// For now, log at debug level
	if os.Getenv("CLICKY_CHATS_DEBUG") != "" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	cmd.Main(cli.New())
}
