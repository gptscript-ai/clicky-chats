package db

import (
	"github.com/acorn-io/z"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
)

type Thread struct {
	Metadata `json:",inline"`
	// This is not part of the public API
	LockedByRunID string `json:"locked_by_run_id"`
}

func (t *Thread) IDPrefix() string {
	return "thread_"
}

func (t *Thread) SetThreadID(string) error { return nil }

func (t *Thread) GetThreadID() string { return t.ID }

func (t *Thread) ToPublic() any {
	//nolint:govet
	return &openai.ThreadObject{
		t.CreatedAt,
		t.ID,
		(*map[string]interface{})(z.Pointer(t.Metadata.Metadata)),
		openai.Thread,
	}
}

func (t *Thread) FromPublic(obj any) error {
	o, ok := obj.(*openai.ThreadObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && t != nil {
		//nolint:govet
		*t = Thread{
			Metadata{
				Base{
					o.Id,
					o.CreatedAt,
				},
				z.Dereference(o.Metadata),
			},
			"",
		}
	}

	return nil
}
