package db

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/acorn-io/z"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

func SetNewID(obj Storer) {
	// Use base64 encoding here to be consistent with what OpenAI does
	obj.SetID(fmt.Sprintf("%s%s", obj.IDPrefix(), base64.URLEncoding.EncodeToString(sha256.New().Sum([]byte(uuid.NewString()))[:12])))
}

type Storer interface {
	IDPrefix() string
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
