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
	"github.com/gptscript-ai/clicky-chats/pkg/agents/translation"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/spf13/cobra"
)

type Agent struct {
	DSN string `usage:"Server datastore" default:"sqlite://clicky-chats.db" env:"CLICKY_CHATS_DSN"`

	RetentionPeriod          string `usage:"Chat completion retention period" default:"5m" env:"CLICKY_CHATS_RETENTION_PERIOD"`
	PollingInterval          string `usage:"Chat completion polling interval" default:"1s" env:"CLICKY_CHATS_POLLING_INTERVAL"`
	DefaultChatCompletionURL string `usage:"The default URL for the chat completion agent to use" default:"https://api.openai.com/v1/chat/completions" env:"CLICKY_CHATS_CHAT_COMPLETION_SERVER_URL"`
	ModelsURL                string `usage:"The url for the to get the available models" default:"https://api.openai.com/v1/models" env:"CLICKY_CHATS_CHAT_COMPLETION_SERVER_URL"`

	ToolRunnerBaseURL string `usage:"Tool runner base URL" default:"http://localhost:8080/v1" env:"CLICKY_CHATS_TOOL_RUNNER_BASE_URL"`

	DefaultImagesURL string `usage:"The default base URL for the image agent to use" default:"https://api.openai.com/v1/images" env:"CLICKY_CHATS_IMAGES_SERVER_URL"`

	DefaultEmbeddingsURL string `usage:"The defaultURL for the embedding agent to use" default:"https://api.openai.com/v1/embeddings" env:"CLICKY_CHATS_EMBEDDINGS_SERVER_URL"`

	DefaultTranslationsURL string `usage:"The default URL for the translation agent to use" default:"https://api.openai.com/v1/audio/translations" env:"CLICKY_CHATS_TRANSLATIONS_SERVER_URL"`

	APIURL      string `usage:"URL for API calls" default:"http://localhost:8080/v1/chat/completions" env:"CLICKY_CHATS_SERVER_URL"`
	ModelAPIKey string `usage:"API key for API calls" env:"CLICKY_CHATS_MODEL_API_KEY"`
	AgentID     string `usage:"Agent ID to identify this agent" default:"my-agent" env:"CLICKY_CHATS_AGENT_ID"`
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
	retentionPeriod, err := time.ParseDuration(s.RetentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to parse chat completion retention period: %w", err)
	}
	pollingInterval, err := time.ParseDuration(s.PollingInterval)
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
		PollingInterval:   pollingInterval,
		RetentionPeriod:   retentionPeriod,
		AgentID:           s.AgentID,
	}
	if err := chatcompletion.Start(ctx, gormDB, ccCfg); err != nil {
		return err
	}

	runCfg := run.Config{
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		APIURL:          s.APIURL,
		APIKey:          s.ModelAPIKey,
		AgentID:         s.AgentID,
	}
	if err = run.Start(ctx, gormDB, runCfg); err != nil {
		return err
	}

	stepRunnerCfg := steprunner.Config{
		PollingInterval: pollingInterval,
		APIURL:          s.ToolRunnerBaseURL,
		APIKey:          s.ModelAPIKey,
		AgentID:         s.AgentID,
	}
	if err = steprunner.Start(ctx, gormDB, stepRunnerCfg); err != nil {
		return err
	}

	imageCfg := image.Config{
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		ImagesBaseURL:   s.DefaultImagesURL,
		APIKey:          apiKey,
		AgentID:         s.AgentID,
	}
	if err = image.Start(ctx, gormDB, imageCfg); err != nil {
		return err
	}

	/*
	 * Embeddings Agent
	 */
	embedCfg := embeddings.Config{
		APIKey:          s.ModelAPIKey,
		EmbeddingsURL:   s.DefaultEmbeddingsURL,
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		AgentID:         s.AgentID,
	}
	if err = embeddings.Start(ctx, gormDB, embedCfg); err != nil {
		return err
	}

	translationCfg := translation.Config{
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		TranslationsURL: s.DefaultTranslationsURL,
		APIKey:          apiKey,
		AgentID:         s.AgentID,
	}
	if err = translation.Start(ctx, gormDB, translationCfg); err != nil {
		return err
	}

	return nil
}
