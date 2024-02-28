package cli

import (
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/thedadams/clicky-chats/pkg/agents/chatcompletion"
	"github.com/thedadams/clicky-chats/pkg/db"
)

type Agent struct {
	DSN                           string `usage:"Server datastore" default:"sqlite://clicky-chats.db" env:"CLICKY_CHATS_DSN"`
	ChatCompletionPollingInterval string `usage:"Chat completion polling interval" default:"5s" env:"CLICKY_CHATS_CHAT_COMPLETION_POLLING_INTERVAL"`
	ChatCompletionCleanupTickTime string `usage:"Chat completion cleanup tick time" default:"5m" env:"CLICKY_CHATS_CHAT_COMPLETION_CLEANUP_TICK_TIME"`
	APIURL                        string `usage:"URL for API calls" default:"https://api.openai.com/v1/chat/completions" env:"CLICKY_CHATS_SERVER_URL"`
	ModelAPIKey                   string `usage:"API key for API calls" default:"" env:"CLICKY_CHATS_MODEL_API_KEY"`
	AgentID                       string `usage:"Agent ID to identify this agent" default:"" env:"CLICKY_CHATS_AGENT_ID"`
}

func (s *Agent) Run(cmd *cobra.Command, _ []string) error {
	gormDB, err := db.New(s.DSN, false)
	if err != nil {
		return err
	}

	chatCompletionCleanupTickTime, err := time.ParseDuration(s.ChatCompletionCleanupTickTime)
	if err != nil {
		slog.Warn("Failed to parse chat completion cleanup tick time, using 15m", "err", err)
		chatCompletionCleanupTickTime = 5 * time.Minute
	}
	chatCompletionPollingInterval, err := time.ParseDuration(s.ChatCompletionPollingInterval)
	if err != nil {
		slog.Warn("Failed to parse chat completion polling tick time, using 5s", "err", err)
		chatCompletionPollingInterval = 5 * time.Second
	}

	cfg := chatcompletion.Config{
		APIKey:          s.ModelAPIKey,
		APIURL:          s.APIURL,
		PollingInterval: chatCompletionPollingInterval,
		CleanupTickTime: chatCompletionCleanupTickTime,
		AgentID:         s.AgentID,
	}

	if err = chatcompletion.Start(cmd.Context(), gormDB.DB, cfg); err != nil {
		return err
	}

	<-cmd.Context().Done()
	return nil
}
