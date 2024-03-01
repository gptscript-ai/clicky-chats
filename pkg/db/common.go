package db

import (
	"time"

	"github.com/acorn-io/z"
	"github.com/google/uuid"
	"github.com/thedadams/clicky-chats/pkg/types"
	"gorm.io/datatypes"
)

func NewID() string {
	return uuid.New().String()
}

type Storer interface {
	SetID(string)
	GetID() string
	SetCreatedAt(int)
	GetCreatedAt() int
}

type Transformer interface {
	Storer
	ToPublic() any
	FromPublic(any) error
}

type Threader interface {
	Transformer
	SetThreadID(string) error
	GetThreadID() string
}

type JobRunner interface {
	GetResponseID() string
	ToPublic() any
	FromPublic(any) error
}

type JobResponder interface {
	GetStatusCode() int
	GetErrorString() string
	ToPublic() any
	FromPublic(any) error
}

func NewBase() Base {
	return Base{
		ID:        NewID(),
		CreatedAt: int(time.Now().Unix()),
	}
}

type Base struct {
	ID        string `json:"id" gorm:"primarykey"`
	CreatedAt int    `json:"created_at,omitempty"`
}

func (b *Base) SetID(id string) {
	b.ID = id
}

func (b *Base) GetID() string {
	return b.ID
}

func (b *Base) SetCreatedAt(t int) {
	b.CreatedAt = t
}

func (b *Base) GetCreatedAt() int {
	return b.CreatedAt
}

func NewMetadata(metadata map[string]any) Metadata {
	return Metadata{
		Base:     NewBase(),
		Metadata: metadata,
	}
}

type Metadata struct {
	Base     `json:",inline"`
	Metadata datatypes.JSONMap `json:"metadata,omitempty"`
}

func NewThreadChild(threadID string, metadata map[string]any) ThreadChild {
	return ThreadChild{
		Metadata: NewMetadata(metadata),
		ThreadID: threadID,
	}
}

type ThreadChild struct {
	Metadata
	ThreadID string `json:"thread_id"`
}

func (t *ThreadChild) SetThreadID(id string) error {
	if t.ThreadID != "" && t.ThreadID != id {
		return types.ErrThreadID(t.ThreadID)
	}

	t.ThreadID = id
	return nil
}

func (t *ThreadChild) GetThreadID() string {
	return t.ThreadID
}

type JobRequest struct {
	Base       `json:",inline"`
	ResponseID *string `json:"response_id,omitempty"`
	ClaimedBy  *string `json:"claimed_by,omitempty"`
}

func (j JobRequest) GetResponseID() string {
	return z.Dereference(j.ResponseID)
}

type JobResponse struct {
	Error      *string `json:"error"`
	StatusCode int     `json:"status_code"`
}

func (j JobResponse) GetStatusCode() int {
	return j.StatusCode
}

func (j JobResponse) GetErrorString() string {
	return z.Dereference(j.Error)
}
