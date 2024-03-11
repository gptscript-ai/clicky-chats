package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gptscript-ai/clicky-chats/pkg/agents/chatcompletion"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/embeddings"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/image"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/run"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/steprunner"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/spf13/cobra"
)

type Agent struct {
	DSN                           string `usage:"Server datastore" default:"sqlite://clicky-chats.db" env:"CLICKY_CHATS_DSN"`
	ChatCompletionPollingInterval string `usage:"Chat completion polling interval" default:"1s" env:"CLICKY_CHATS_CHAT_COMPLETION_POLLING_INTERVAL"`
	ChatCompletionRetentionPeriod string `usage:"Chat completion retention period" default:"5m" env:"CLICKY_CHATS_CHAT_COMPLETION_RETENTION_PERIOD"`
	RunCompletionPollingInterval  string `usage:"Run completion polling interval" default:"1s" env:"CLICKY_CHATS_RUN_COMPLETION_POLLING_INTERVAL"`
	RunCompletionRetentionPeriod  string `usage:"Run completion retention period" default:"5m" env:"CLICKY_CHATS_RUN_COMPLETION_RETENTION_PERIOD"`
	ToolRunnerPollingInterval     string `usage:"Tool runner polling interval" default:"1s" env:"CLICKY_CHATS_TOOL_RUNNER_POLLING_INTERVAL"`
	ImagePollingInterval          string `usage:"Image job polling interval" default:"1s" env:"CLICKY_CHATS_IMAGE_POLLING_INTERVAL"`
	ImageRetentionPeriod          string `usage:"Image retention period" default:"5m" env:"CLICKY_CHATS_IMAGE_RETENTION_PERIOD"`
	ToolRunnerBaseURL             string `usage:"Tool runner base URL" default:"http://localhost:8080/v1" env:"CLICKY_CHATS_TOOL_RUNNER_BASE_URL"`
	DefaultChatCompletionURL      string `usage:"The default URL for the chat completion agent to use" default:"https://api.openai.com/v1/chat/completions" env:"CLICKY_CHATS_CHAT_COMPLETION_SERVER_URL"`
	DefaultImagesURL              string `usage:"The default URL for the image agent to use" default:"https://api.openai.com/v1/images/generations" env:"CLICKY_CHATS_IMAGES_SERVER_URL"`
	DefaultEmbeddingsURL          string `usage:"The defaultURL for the embedding agent to use" default:"https://api.openai.com/v1/embeddings" env:"CLICKY_CHATS_EMBEDDINGS_SERVER_URL"`
	ModelsURL                     string `usage:"The url for the to get the available models" default:"https://api.openai.com/v1/models" env:"CLICKY_CHATS_CHAT_COMPLETION_SERVER_URL"`
	APIURL                        string `usage:"URL for API calls" default:"http://localhost:8080/v1/chat/completions" env:"CLICKY_CHATS_SERVER_URL"`
	ModelAPIKey                   string `usage:"API key for API calls" env:"CLICKY_CHATS_MODEL_API_KEY"`
	AgentID                       string `usage:"Agent ID to identify this agent" default:"my-agent" env:"CLICKY_CHATS_AGENT_ID"`
}

func (s *Agent) Run(cmd *cobra.Command, _ []string) error {
	gormDB, err := db.New(s.DSN, false)
	if err != nil {
		return err
	}

	if err = runAgents(cmd.Context(), gormDB, s); err != nil {
		return err
	}

	<-cmd.Context().Done()
	return nil
}

func runAgents(ctx context.Context, gormDB *db.DB, s *Agent) error {
	chatCompletionRetentionPeriod, err := time.ParseDuration(s.ChatCompletionRetentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to parse chat completion retention period: %w", err)
	}
	chatCompletionPollingInterval, err := time.ParseDuration(s.ChatCompletionPollingInterval)
	if err != nil {
		return fmt.Errorf("failed to parse chat completion polling interval: %w", err)
	}

	apiKey := s.ModelAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	ccCfg := chatcompletion.Config{
		APIKey:            apiKey,
		ModelsURL:         s.ModelsURL,
		ChatCompletionURL: s.DefaultChatCompletionURL,
		PollingInterval:   chatCompletionPollingInterval,
		RetentionPeriod:   chatCompletionRetentionPeriod,
		AgentID:           s.AgentID,
	}
	if err := chatcompletion.Start(ctx, gormDB, ccCfg); err != nil {
		return err
	}

	runCompletionRetentionPeriod, err := time.ParseDuration(s.RunCompletionRetentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to parse run completion cleanup interval: %w", err)
	}
	runCompletionPollingInterval, err := time.ParseDuration(s.RunCompletionPollingInterval)
	if err != nil {
		return fmt.Errorf("failed to parse run completion polling interval: %w", err)
	}

	runCfg := run.Config{
		PollingInterval: runCompletionPollingInterval,
		RetentionPeriod: runCompletionRetentionPeriod,
		APIURL:          s.APIURL,
		APIKey:          s.ModelAPIKey,
		AgentID:         s.AgentID,
	}
	if err = run.Start(ctx, gormDB, runCfg); err != nil {
		return err
	}

	toolRunnerPollingInterval, err := time.ParseDuration(s.ToolRunnerPollingInterval)
	if err != nil {
		return fmt.Errorf("failed to parse run completion polling interval: %w", err)
	}

	stepRunnerCfg := steprunner.Config{
		PollingInterval: toolRunnerPollingInterval,
		APIURL:          s.ToolRunnerBaseURL,
		APIKey:          s.ModelAPIKey,
		AgentID:         s.AgentID,
	}
	if err = steprunner.Start(ctx, gormDB, stepRunnerCfg); err != nil {
		return err
	}

	imagePollingInterval, err := time.ParseDuration(s.ImagePollingInterval)
	if err != nil {
		return fmt.Errorf("failed to parse image polling interval: %w", err)
	}

	imageRetentionPeriod, err := time.ParseDuration(s.ImageRetentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to parse image response retention period: %w", err)
	}

	imageCfg := image.Config{
		PollingInterval: imagePollingInterval,
		RetentionPeriod: imageRetentionPeriod,
		ImagesURL:       s.DefaultImagesURL,
		APIKey:          s.ModelAPIKey,
		AgentID:         s.AgentID,
	}
	if err = image.Start(ctx, gormDB, imageCfg); err != nil {
		return err
	}

	/*
	 * Embeddings Agent
	 */
	embeddingRetentionPeriod, err := time.ParseDuration(s.ChatCompletionRetentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to parse embedding retention period: %w", err)
	}
	embeddingPollingInterval, err := time.ParseDuration(s.ChatCompletionPollingInterval)
	if err != nil {
		return fmt.Errorf("failed to parse embedding polling interval: %w", err)
	}

	embedCfg := embeddings.Config{
		APIKey:          s.ModelAPIKey,
		EmbeddingsURL:   s.DefaultEmbeddingsURL,
		PollingInterval: embeddingPollingInterval,
		RetentionPeriod: embeddingRetentionPeriod,
		AgentID:         s.AgentID,
	}
	if err = embeddings.Start(ctx, gormDB, embedCfg); err != nil {
		return err
	}

	return nil
}
