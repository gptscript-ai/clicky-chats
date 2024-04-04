package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type RunToolObject struct {
	JobRequest

	EnvVars       datatypes.JSONSlice[string] `json:"env_vars,omitempty"`
	File          string                      `json:"file"`
	Input         string                      `json:"input,omitempty"`
	Subtool       string                      `json:"subtool"`
	Cache         *bool                       `json:"cache,omitempty"`
	DangerousMode bool                        `json:"dangerous_mode,omitempty"`

	Output    string `json:"output,omitempty"`
	Status    string `json:"status,omitempty"`
	Confirmed *bool  `json:"confirmed,omitempty"`
}

func (r *RunToolObject) IDPrefix() string {
	return "run_tool_"
}

func (r *RunToolObject) ToPublic() any {
	//nolint:govet
	return &openai.XRunToolRequest{
		r.Cache,
		r.DangerousMode,
		r.EnvVars,
		r.File,
		r.Input,
		r.Subtool,
	}
}

func (r *RunToolObject) FromPublic(obj any) error {
	o, ok := obj.(*openai.XRunToolRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && r != nil {
		//nolint:govet
		*r = RunToolObject{
			JobRequest{},
			datatypes.NewJSONSlice(o.EnvVars),
			o.File,
			o.Input,
			o.Subtool,
			o.Cache,
			o.DangerousMode,
			"",
			string(openai.RunObjectStatusQueued),
			nil,
		}
	}

	return nil
}
