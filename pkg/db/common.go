package db

import (
	"github.com/thedadams/clicky-chats/pkg/types"
	"gorm.io/datatypes"
)

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

type Metadata struct {
	Base     `json:",inline"`
	Metadata datatypes.JSONMap `json:"metadata,omitempty"`
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
