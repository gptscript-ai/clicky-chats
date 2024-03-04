package db

import (
	"net/http"
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
	ToPublic() any
	FromPublic(any) error
	IsDone() bool
}

type JobResponder interface {
	GetRequestID() string
	GetStatusCode() int
	GetErrorString() string
	ToPublic() any
	FromPublic(any) error
	IsDone() bool
}

type JobRespondStreamer interface {
	Storer
	JobResponder
	GetIndex() int
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
	Base      `json:",inline"`
	ClaimedBy *string `json:"claimed_by,omitempty"`
	Done      bool    `json:"done"`
}

func (j JobRequest) IsDone() bool {
	return j.Done
}

type JobResponse struct {
	RequestID  string  `json:"request_id"`
	Error      *string `json:"error"`
	StatusCode int     `json:"status_code"`
	Done       bool    `json:"done"`
}

func (j JobResponse) GetStatusCode() int {
	if j.StatusCode > 0 || j.Error == nil {
		return j.StatusCode
	}

	return http.StatusInternalServerError
}

func (j JobResponse) GetErrorString() string {
	return z.Dereference(j.Error)
}

func (j JobResponse) IsDone() bool {
	return j.Done
}

func (j JobResponse) GetRequestID() string {
	return j.RequestID
}
