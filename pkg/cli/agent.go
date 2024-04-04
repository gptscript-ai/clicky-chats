package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gptscript-ai/clicky-chats/pkg/agents/audio"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/chatcompletion"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/embeddings"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/image"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/run"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/steprunner"
	"github.com/gptscript-ai/clicky-chats/pkg/agents/toolrunner"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	kb "github.com/gptscript-ai/clicky-chats/pkg/knowledgebases"
	"github.com/gptscript-ai/clicky-chats/pkg/server"
	"github.com/spf13/cobra"
)

type Agent struct {
	kb.Config

	DSN string `usage:"Server datastore" default:"sqlite://clicky-chats.db" env:"CLICKY_CHATS_DSN"`

	RetentionPeriod          string `usage:"Chat completion retention period" default:"5m" env:"CLICKY_CHATS_RETENTION_PERIOD"`
	PollingInterval          string `usage:"Chat completion polling interval" default:"1s" env:"CLICKY_CHATS_POLLING_INTERVAL"`
	DefaultChatCompletionURL string `usage:"The default URL for the chat completion agent to use" default:"https://api.openai.com/v1/chat/completions" env:"CLICKY_CHATS_CHAT_COMPLETION_SERVER_URL"`
	ModelsURL                string `usage:"The url for the to get the available models" default:"https://api.openai.com/v1/models" env:"CLICKY_CHATS_CHAT_COMPLETION_SERVER_URL"`

	ToolRunnerBaseURL string `usage:"Tool runner base URL" default:"http://localhost:8080/v1" env:"CLICKY_CHATS_TOOL_RUNNER_BASE_URL"`

	DefaultImagesURL string `usage:"The default base URL for the image agent to use" default:"https://api.openai.com/v1/images" env:"CLICKY_CHATS_IMAGES_SERVER_URL"`

	DefaultEmbeddingsURL string `usage:"The defaultURL for the embedding agent to use" default:"https://api.openai.com/v1/embeddings" env:"CLICKY_CHATS_EMBEDDINGS_SERVER_URL"`

	DefaultAudioURL string `usage:"The default URL for the translation agent to use" default:"https://api.openai.com/v1/audio" env:"CLICKY_CHATS_AUDIO_SERVER_URL"`

	APIURL      string `usage:"URL for API calls" default:"http://localhost:8080/v1/chat/completions" env:"CLICKY_CHATS_SERVER_URL"`
	ModelAPIKey string `usage:"API key for API calls" env:"CLICKY_CHATS_MODEL_API_KEY"`
	AgentID     string `usage:"Agent ID to identify this agent" default:"my-agent" env:"CLICKY_CHATS_AGENT_ID"`

	Cache   bool `usage:"Enable the cache for Function calling" default:"true" env:"CLICKY_CHATS_CACHE"`
	Confirm bool `usage:"Enable the confirmation for Function calling" default:"false" env:"CLICKY_CHATS_CONFIRM"`
}

func (s *Agent) Run(cmd *cobra.Command, _ []string) error {
	gormDB, err := db.New(s.DSN, false)
	if err != nil {
		return err
	}

	var kbm *kb.KnowledgeBaseManager
	if s.Config.KnowledgeRetrievalAPIURL != "" {
		kbm, err = kb.NewKnowledgeBaseManager(cmd.Context(), s.Config, gormDB)
		if err != nil {
			return err
		}
	} else {
		slog.Warn("No knowledge retrieval API URL provided, knowledge base manager will not be started - assistants using the `retrieval` tool won't work")
	}

	wg := new(sync.WaitGroup)
	if err = runAgents(cmd.Context(), wg, gormDB, kbm, s, new(server.Triggers)); err != nil {
		return err
	}

	wg.Wait()
	return nil
}

func runAgents(ctx context.Context, wg *sync.WaitGroup, gormDB *db.DB, kbm *kb.KnowledgeBaseManager, s *Agent, triggers *server.Triggers) error {
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

	triggers.Complete()

	ccCfg := chatcompletion.Config{
		APIKey:            apiKey,
		ModelsURL:         s.ModelsURL,
		ChatCompletionURL: s.DefaultChatCompletionURL,
		PollingInterval:   pollingInterval,
		RetentionPeriod:   retentionPeriod,
		AgentID:           s.AgentID,
		Trigger:           triggers.ChatCompletion,
	}
	if err := chatcompletion.Start(ctx, wg, gormDB, ccCfg); err != nil {
		return err
	}

	runCfg := run.Config{
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		APIURL:          s.APIURL,
		APIKey:          apiKey,
		AgentID:         s.AgentID,
		Trigger:         triggers.Run,
		RunStepTrigger:  triggers.RunStep,
	}
	if err = run.Start(ctx, wg, gormDB, runCfg); err != nil {
		return err
	}

	stepRunnerCfg := steprunner.Config{
		PollingInterval: pollingInterval,
		APIURL:          s.ToolRunnerBaseURL,
		APIKey:          apiKey,
		AgentID:         s.AgentID,
		Cache:           s.Cache,
		Confirm:         s.Confirm,
		Trigger:         triggers.RunStep,
		RunTrigger:      triggers.Run,
	}
	if err = steprunner.Start(ctx, wg, gormDB, kbm, stepRunnerCfg); err != nil {
		return err
	}

	imageCfg := image.Config{
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		ImagesBaseURL:   s.DefaultImagesURL,
		APIKey:          apiKey,
		AgentID:         s.AgentID,
		Trigger:         triggers.Image,
	}
	if err = image.Start(ctx, wg, gormDB, imageCfg); err != nil {
		return err
	}

	embedCfg := embeddings.Config{
		APIKey:          apiKey,
		EmbeddingsURL:   s.DefaultEmbeddingsURL,
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		AgentID:         s.AgentID,
		Trigger:         triggers.Embeddings,
	}
	if err = embeddings.Start(ctx, wg, gormDB, embedCfg); err != nil {
		return err
	}

	audioCfg := audio.Config{
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		AudioBaseURL:    s.DefaultAudioURL,
		APIKey:          apiKey,
		AgentID:         s.AgentID,
		Trigger:         triggers.Audio,
	}
	if err = audio.Start(ctx, wg, gormDB, audioCfg); err != nil {
		return err
	}

	toolRunnerCfg := toolrunner.Config{
		PollingInterval: pollingInterval,
		RetentionPeriod: retentionPeriod,
		APIURL:          s.ToolRunnerBaseURL,
		APIKey:          apiKey,
		AgentID:         s.AgentID,
		Cache:           s.Cache,
		Confirm:         s.Confirm,
		Trigger:         triggers.RunTool,
	}
	if err = toolrunner.Start(ctx, wg, gormDB, toolRunnerCfg); err != nil {
		return err
	}

	return nil
}
