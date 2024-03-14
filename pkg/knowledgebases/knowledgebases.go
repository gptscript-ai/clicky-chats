package knowledgebases

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
)

const (
	AssistantPrefix string = "as-" // Assistant-internal (i.e. not shared) knowledge base prefix
	SharedPrefix    string = "kb-" // Shared knowledge base prefix
)

/*
 * Handling Knowledge Base IDs
 */

func GetAssistantKnowledgeBaseID(assistantID string) string {
	return AssistantPrefix + assistantID
}

func NewSharedKnowledgeBaseID() string {
	return SharedPrefix + uuid.New().String()
}

/*
 * Create Knowledge Bases
 */

func (m *KnowledgeBaseManager) NewAssistantKnowledgeBase(ctx context.Context, assistantID string) (string, error) {
	return m.CreateKnowledgeBase(ctx, GetAssistantKnowledgeBaseID(assistantID))
}

func (m *KnowledgeBaseManager) NewSharedKnowledgeBase(ctx context.Context) (string, error) {
	return m.CreateKnowledgeBase(ctx, NewSharedKnowledgeBaseID())
}

type CreateKnowledgeBaseRequest struct {
	Name     string `json:"name"`
	EmbedDim int    `json:"embed_dim"`
}

type CreateKnowledgeBaseResponse struct {
	CreateKnowledgeBaseRequest `json:",inline"`
}

func (m *KnowledgeBaseManager) CreateKnowledgeBase(ctx context.Context, id string) (string, error) {

	url := m.KnowledgeRetrievalAPIURL + "/datasets/create"
	payload := CreateKnowledgeBaseRequest{
		Name:     id,
		EmbedDim: 0,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}

	if res.StatusCode > 400 {
		return "", fmt.Errorf("failed to create knowledge base: %s", res.Status)
	}

	defer res.Body.Close()
	resp, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	var response CreateKnowledgeBaseResponse
	err = json.Unmarshal(resp, &response)
	if err != nil {
		return "", err
	}

	return response.Name, nil
}

func (m *KnowledgeBaseManager) DeleteKnowledgeBase(ctx context.Context, id string) error {
	url := m.KnowledgeRetrievalAPIURL + "/datasets/" + id

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode > 400 {
		return fmt.Errorf("failed to delete knowledge base: %s", res.Status)
	}

	defer res.Body.Close()
	return nil
}

type IngestFileRequest struct {
	Filename string `json:"filename,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	Content  string `json:"content,omitempty"` // Base64 encoded content
}

func (m *KnowledgeBaseManager) AddFile(ctx context.Context, id string, fileID string) error {
	url := m.KnowledgeRetrievalAPIURL + "/datasets/" + id + "/ingest"

	gdb := m.db.WithContext(ctx)
	file := new(db.File)
	err := db.Get(gdb, file, fileID)
	if err != nil {
		return err
	}

	contentB64 := base64.StdEncoding.EncodeToString(file.Content)

	payload := IngestFileRequest{
		FileID:   file.ID,
		Filename: file.Filename,
		Content:  contentB64,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	// try to read the response body to get the error message
	var b []byte
	b, _ = io.ReadAll(res.Body)

	if res.StatusCode > 400 {
		if len(b) > 0 {
			return fmt.Errorf("failed to ingest file: %s", string(b))
		}
		return fmt.Errorf("failed to ingest file: %s", res.Status)
	}

	defer res.Body.Close()
	return nil
}
