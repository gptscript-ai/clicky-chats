package cli

import (
	"github.com/spf13/cobra"
	"github.com/thedadams/clicky-chats/pkg/controller"
	"github.com/thedadams/clicky-chats/pkg/db"
)

type Agent struct {
	DSN string `usage:"Server datastore" default:"sqlite://clicky-chats.db" env:"CLICKY_CHATS_DSN"`
}

func (s *Agent) Run(cmd *cobra.Command, _ []string) error {
	gormDB, err := db.New(s.DSN, false)
	if err != nil {
		return err
	}

	if err = controller.Start(cmd.Context(), gormDB.DB); err != nil {
		return err
	}

	<-cmd.Context().Done()
	return nil
}
