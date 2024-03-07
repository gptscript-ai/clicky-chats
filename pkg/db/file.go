package db

import "github.com/gptscript-ai/clicky-chats/pkg/generated/openai"

type File struct {
	Base
	Content  []byte `json:"file"`
	Purpose  string `json:"purpose"`
	Filename string `json:"filename"`
}

func (f *File) IDPrefix() string {
	return "file-"
}

func (f *File) ToPublic() any {
	//nolint:govet
	return &openai.OpenAIFile{
		len(f.Content),
		f.CreatedAt,
		f.Filename,
		f.ID,
		openai.OpenAIFileObjectFile,
		openai.OpenAIFilePurpose(f.Purpose),
		// These last two fields are deprecated and will never be set.
		"",
		nil,
	}
}

func (f *File) FromPublic(obj any) error {
	o, ok := obj.(*openai.OpenAIFile)
	if !ok {
		return InvalidTypeError{
			Expected: o,
			Got:      obj,
		}
	}

	if obj != nil && f != nil {
		//nolint:govet
		*f = File{
			Base{
				o.Id,
				o.CreatedAt,
			},
			f.Content,
			string(o.Purpose),
			o.Filename,
		}
	}

	return nil
}
