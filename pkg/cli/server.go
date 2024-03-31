package cli

import (
	"log/slog"
	"os/signal"
	"sync"
	"syscall"

	"github.com/gptscript-ai/clicky-chats/pkg/db"
	kb "github.com/gptscript-ai/clicky-chats/pkg/knowledgebases"
	"github.com/gptscript-ai/clicky-chats/pkg/server"
	"github.com/gptscript-ai/clicky-chats/pkg/trigger"
	"github.com/spf13/cobra"
)

type Server struct {
	Agent

	AutoMigrate string `usage:"Auto migrate" default:"true" env:"CLICKY_CHATS_AUTO_MIGRATE"`

	ServerURL     string `usage:"Server URL" default:"http://localhost" env:"CLICKY_CHATS_SERVER_URL"`
	ServerPort    string `usage:"Server port" default:"8080" env:"CLICKY_CHATS_SERVER_PORT"`
	ServerAPIBase string `usage:"Server API base" default:"/v1" env:"CLICKY_CHATS_SERVER_API_BASE"`

	WithAgents bool `usage:"Run the server and agents" default:"false" env:"CLICKY_CHATS_WITH_AGENTS"`
}

func (s *Server) Run(cmd *cobra.Command, _ []string) error {
	wg := new(sync.WaitGroup)
	gormDB, err := db.New(s.DSN, s.AutoMigrate == "true")
	if err != nil {
		return err
	}

	var kbManager *kb.KnowledgeBaseManager
	if s.Config.KnowledgeRetrievalAPIURL != "" {
		kbManager = kb.NewKnowledgeBaseManager(s.Config, gormDB)
	} else {
		slog.Warn("No knowledge retrieval API URL provided, knowledge base manager will not be started - assistants cannot be created with the `retrieval` tool")
	}

	triggers := new(server.Triggers)
	if s.WithAgents {
		triggers.ChatCompletion = trigger.New()
		triggers.Run = trigger.New()
		triggers.RunStep = trigger.New()
		triggers.RunTool = trigger.New()
		triggers.Image = trigger.New()
		triggers.Embeddings = trigger.New()
		triggers.Audio = trigger.New()
	}
	triggers.Complete()

	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL)
	defer cancel()
	if err = server.NewServer(gormDB, kbManager).Start(ctx, wg, server.Config{
		ServerURL: s.ServerURL,
		Port:      s.ServerPort,
		APIBase:   s.ServerAPIBase,
		Triggers:  triggers,
	}); err != nil {
		return err
	}

	if s.WithAgents {
		if err = runAgents(cmd.Context(), wg, gormDB, kbManager, &s.Agent, triggers); err != nil {
			return err
		}
	}

	wg.Wait()
	return nil
}
