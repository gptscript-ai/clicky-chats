package cli

import (
	"fmt"
	"time"

	"github.com/gptscript-ai/clicky-chats/pkg/agents/chatcompletion"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/run"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/steprunner"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/spf13/cobra"
)

type Agent struct {
	DSN                           string `usage:"Server datastore" default:"sqlite://clicky-chats.db" env:"CLICKY_CHATS_DSN"`
	ChatCompletionPollingInterval string `usage:"Chat completion polling interval" default:"1s" env:"CLICKY_CHATS_CHAT_COMPLETION_POLLING_INTERVAL"`
	ChatCompletionCleanupTickTime string `usage:"Chat completion cleanup tick time" default:"5m" env:"CLICKY_CHATS_CHAT_COMPLETION_CLEANUP_TICK_TIME"`
	RunCompletionPollingInterval  string `usage:"Run completion polling interval" default:"1s" env:"CLICKY_CHATS_RUN_COMPLETION_POLLING_INTERVAL"`
	RunCompletionCleanupTickTime  string `usage:"Run completion cleanup tick time" default:"5m" env:"CLICKY_CHATS_RUN_COMPLETION_CLEANUP_TICK_TIME"`
	ToolRunnerPollingInterval     string `usage:"Tool runner polling interval" default:"1s" env:"CLICKY_CHATS_TOOL_RUNNER_POLLING_INTERVAL"`
	ToolRunnerBaseURL             string `usage:"Tool runner base URL" default:"http://localhost:8080/v1" env:"CLICKY_CHATS_TOOL_RUNNER_BASE_URL"`
	DefaultChatCompletionURL      string `usage:"The defaultURL for the chat completion agent to use" default:"https://api.openai.com/v1/chat/completions" env:"CLICKY_CHATS_CHAT_COMPLETION_SERVER_URL"`
	ModelsURL                     string `usage:"The url for the to get the available models" default:"https://api.openai.com/v1/models" env:"CLICKY_CHATS_CHAT_COMPLETION_SERVER_URL"`
	APIURL                        string `usage:"URL for API calls" default:"http://localhost:8080/v1/chat/completions" env:"CLICKY_CHATS_SERVER_URL"`
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
		return fmt.Errorf("failed to parse chat completion cleanup tick time: %w", err)
	}
	chatCompletionPollingInterval, err := time.ParseDuration(s.ChatCompletionPollingInterval)
	if err != nil {
		return fmt.Errorf("failed to parse chat completion polling tick time: %w", err)
	}

	ccCfg := chatcompletion.Config{
		APIKey:            s.ModelAPIKey,
		ModelsURL:         s.ModelsURL,
		ChatCompletionURL: s.DefaultChatCompletionURL,
		PollingInterval:   chatCompletionPollingInterval,
		CleanupTickTime:   chatCompletionCleanupTickTime,
		AgentID:           s.AgentID,
	}
	if err = chatcompletion.Start(cmd.Context(), gormDB, ccCfg); err != nil {
		return err
	}

	runCompletionCleanupTickTime, err := time.ParseDuration(s.ChatCompletionCleanupTickTime)
	if err != nil {
		return fmt.Errorf("failed to parse run completion cleanup tick time: %w", err)
	}
	runCompletionPollingInterval, err := time.ParseDuration(s.ChatCompletionPollingInterval)
	if err != nil {
		return fmt.Errorf("failed to parse run completion polling tick time: %w", err)
	}

	runCfg := run.Config{
		PollingInterval: runCompletionPollingInterval,
		CleanupTickTime: runCompletionCleanupTickTime,
		APIURL:          s.APIURL,
		APIKey:          s.ModelAPIKey,
		AgentID:         s.AgentID,
	}
	if err = run.Start(cmd.Context(), gormDB, runCfg); err != nil {
		return err
	}

	toolRunnerPollingInterval, err := time.ParseDuration(s.ChatCompletionPollingInterval)
	if err != nil {
		return fmt.Errorf("failed to parse run completion polling tick time: %w", err)
	}

	stepRunnerCfg := steprunner.Config{
		PollingInterval: toolRunnerPollingInterval,
		APIURL:          s.ToolRunnerBaseURL,
		APIKey:          s.ModelAPIKey,
		AgentID:         s.AgentID,
	}
	if err = steprunner.Start(cmd.Context(), gormDB, stepRunnerCfg); err != nil {
		return err
	}

	<-cmd.Context().Done()
	return nil
}
