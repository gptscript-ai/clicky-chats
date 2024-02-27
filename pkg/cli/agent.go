package cli

import (
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/thedadams/clicky-chats/pkg/controllers/chatcompletion"
	"github.com/thedadams/clicky-chats/pkg/db"
)

type Agent struct {
	DSN                           string `usage:"Server datastore" default:"sqlite://clicky-chats.db" env:"CLICKY_CHATS_DSN"`
	ChatCompletionCleanupTickTime string `usage:"Chat completion cleanup tick time" default:"5m" env:"CLICKY_CHATS_CHAT_COMPLETION_CLEANUP_TICK_TIME"`
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

	if err = chatcompletion.Start(cmd.Context(), gormDB.DB, chatCompletionCleanupTickTime); err != nil {
		return err
	}

	<-cmd.Context().Done()
	return nil
}
