package db

import (
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/gptscript/pkg/server"
	"gorm.io/datatypes"
)

type RunStepEvent struct {
	Base        `json:",inline"`
	JobResponse `json:",inline"`

	ToolSubCalls       datatypes.JSONMap       `json:"tool_sub_calls,omitempty"`
	ToolResults        int                     `json:"tool_results,omitempty"`
	Type               string                  `json:"type,omitempty"`
	ChatCompletionID   string                  `json:"chat_completion_id,omitempty"`
	ChatRequest        datatypes.JSONType[any] `json:"chat_request,omitempty"`
	ChatResponse       datatypes.JSONType[any] `json:"chat_response,omitempty"`
	ChatResponseCached bool                    `json:"chat_response_cached,omitempty"`
	Content            string                  `json:"content,omitempty"`
	RunID              string                  `json:"run_id,omitempty"`
	Input              string                  `json:"input,omitempty"`
	Output             string                  `json:"output,omitempty"`
	Err                string                  `json:"err,omitempty"`
	ResponseIdx        int                     `json:"response_idx"`
}

func (r *RunStepEvent) IDPrefix() string {
	return "run_step_event_"
}

func (r *RunStepEvent) GetIndex() int {
	return r.ResponseIdx
}

func (r *RunStepEvent) GetEvent() string {
	return ""
}

func (r *RunStepEvent) FromPublic(obj any) error {
	o, ok := obj.(*openai.XRunStepEventObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && r != nil {
		//nolint:govet
		*r = RunStepEvent{
			Base{
				CreatedAt: int(o.Time.Unix()),
			},
			JobResponse{},
			o.ToolSubCalls,
			z.Dereference(o.ToolResults),
			z.Dereference(o.Type),
			z.Dereference(o.ChatCompletionId),
			datatypes.NewJSONType(o.ChatRequest),
			datatypes.NewJSONType(o.ChatResponse),
			o.ChatResponseCached,
			z.Dereference(o.Content),
			o.RunId,
			z.Dereference(o.Input),
			z.Dereference(o.Output),
			z.Dereference(o.Err),
			0,
		}
	}

	return nil
}

func (r *RunStepEvent) ToPublic() any {
	//nolint:govet
	return &openai.XRunStepEventObject{
		&r.ChatCompletionID,
		r.ChatRequest,
		r.ChatResponse,
		r.ChatResponseCached,
		&r.Content,
		&r.Err,
		&r.Input,
		&r.Output,
		r.RunID,
		r.RequestID,
		time.Unix(int64(r.CreatedAt), 0).UTC(),
		&r.ToolResults,
		r.ToolSubCalls,
		z.Pointer(r.Type),
	}
}

func FromGPTScriptEvent(event server.Event, runID, runStepID string, index int, done bool) *RunStepEvent {
	toolSubCals := make(map[string]any, len(event.ToolSubCalls))
	for k, v := range event.ToolSubCalls {
		toolSubCals[k] = v
	}

	if runID == "" {
		runID = event.RunID
	}

	var err *string
	if event.Err != "" {
		err = &event.Err
	}
	//nolint:govet
	return &RunStepEvent{
		Base{
			CreatedAt: int(time.Now().Unix()),
		},
		JobResponse{
			RequestID: runStepID,
			Error:     err,
			Done:      done,
		},
		toolSubCals,
		event.ToolResults,
		string(event.Type),
		event.ChatCompletionID,
		datatypes.NewJSONType(event.ChatRequest),
		datatypes.NewJSONType(event.ChatResponse),
		event.ChatResponseCached,
		event.Content,
		runID,
		event.Input,
		event.Output,
		event.Err,
		index,
	}
}
