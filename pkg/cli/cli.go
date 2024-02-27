package cli

import (
	"github.com/acorn-io/cmd"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	return cmd.Command(&ClickyChats{}, new(Server), new(Agent))
}

type ClickyChats struct{}

func (a *ClickyChats) Run(cmd *cobra.Command, _ []string) error {
	return cmd.Help()
}
