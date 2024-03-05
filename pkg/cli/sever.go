package cli

import (
	"github.com/spf13/cobra"
	"github.com/thedadams/clicky-chats/pkg/db"
	"github.com/thedadams/clicky-chats/pkg/server"
)

type Server struct {
	DSN         string `usage:"Server datastore" default:"sqlite://clicky-chats.db" env:"CLICKY_CHATS_DSN"`
	AutoMigrate string `usage:"Auto migrate" default:"true" env:"CLICKY_CHATS_AUTO_MIGRATE"`

	ServerURL     string `usage:"Server URL" default:"http://localhost" env:"CLICKY_CHATS_SERVER_URL"`
	ServerPort    string `usage:"Server port" default:"8080" env:"CLICKY_CHATS_SERVER_PORT"`
	ServerAPIBase string `usage:"Server API base" default:"/v1" env:"CLICKY_CHATS_SERVER_API_BASE"`
}

func (s *Server) Run(cmd *cobra.Command, _ []string) error {
	gormDB, err := db.New(s.DSN, s.AutoMigrate == "true")
	if err != nil {
		return err
	}

	return server.NewServer(gormDB).Run(cmd.Context(), server.Config{
		ServerURL: s.ServerURL,
		Port:      s.ServerPort,
		APIBase:   s.ServerAPIBase,
	})
}
