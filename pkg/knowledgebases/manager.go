package knowledgebases

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/gptscript/pkg/gptscript"
	"github.com/gptscript-ai/gptscript/pkg/loader"
)

type Config struct {
	KnowledgeRetrievalAPIURL string `usage:"Knowledge retrieval API URL" env:"CLICKY_CHATS_KNOWLEDGE_RETRIEVAL_API_URL"`
}

type KnowledgeBaseManager struct {
	Config
	db *db.DB
}

func NewKnowledgeBaseManager(ctx context.Context, config Config, db *db.DB) (*KnowledgeBaseManager, error) {
	if !strings.HasPrefix(config.KnowledgeRetrievalAPIURL, "http") {
		url, err := launchKnowledge(ctx, config.KnowledgeRetrievalAPIURL)
		if err != nil {
			return nil, err
		}
		slog.Info("Launched knowledge retrieval API", "url", url)
		config.KnowledgeRetrievalAPIURL = url
	}
	return &KnowledgeBaseManager{
		Config: config,
		db:     db,
	}, nil
}

func launchKnowledge(ctx context.Context, tool string) (string, error) {
	prg, err := loader.Program(ctx, tool, "")
	if err != nil {
		return "", err
	}
	prg = prg.SetBlocking()
	g, err := gptscript.New(nil)
	if err != nil {
		return "", err
	}
	return g.Run(ctx, prg, os.Environ(), "")
}
