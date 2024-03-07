package server

import (
	"github.com/gptscript-ai/clicky-chats/pkg/db"
)

type Transformer interface {
	db.Storer
	ToPublic() any
	FromPublic(any) error
}

type ExtendedTransformer interface {
	Transformer
	ToPublicOpenAI() any
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
	db.Storer
	JobResponder
	GetIndex() int
}
