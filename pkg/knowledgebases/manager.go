package knowledgebases

import (
	"github.com/gptscript-ai/clicky-chats/pkg/db"
)

type Config struct {
	KnowledgeRetrievalAPIURL string `usage:"Knowledge retrieval API URL" default:"http://localhost:8000" env:"CLICKY_CHATS_KNOWLEDGE_RETRIEVAL_API_URL"`
}

type KnowledgeBaseManager struct {
	Config
	db *db.DB
}

func NewKnowledgeBaseManager(config Config, db *db.DB) *KnowledgeBaseManager {
	return &KnowledgeBaseManager{
		Config: config,
		db:     db,
	}
}
