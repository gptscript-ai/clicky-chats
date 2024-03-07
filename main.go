package main

import (
	"log/slog"

	"github.com/acorn-io/cmd"
	"github.com/gptscript-ai/clicky-chats/pkg/cli"
)

func main() {
	// For now, log at debug level
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cmd.Main(cli.New())
}
