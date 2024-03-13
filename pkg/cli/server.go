package cli

import (
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	kb "github.com/gptscript-ai/clicky-chats/pkg/knowledgebases"
	"github.com/gptscript-ai/clicky-chats/pkg/server"
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
	gormDB, err := db.New(s.DSN, s.AutoMigrate == "true")
	if err != nil {
		return err
	}

	kbManager := kb.NewKnowledgeBaseManager(s.Config, gormDB)

	if err = server.NewServer(gormDB, kbManager).Start(cmd.Context(), server.Config{
		ServerURL: s.ServerURL,
		Port:      s.ServerPort,
		APIBase:   s.ServerAPIBase,
	}); err != nil {
		return err
	}

	if s.WithAgents {
		if err = runAgents(cmd.Context(), gormDB, &s.Agent); err != nil {
			return err
		}
	}

	<-cmd.Context().Done()
	return nil
}
