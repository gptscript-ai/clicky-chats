package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type Run struct {
	Metadata       `json:",inline"`
	AssistantID    string                                           `json:"assistant_id"`
	ThreadID       string                                           `json:"thread_id"`
	Status         string                                           `json:"status"`
	RequiredAction datatypes.JSONType[*RunRequiredAction]           `json:"required_action"`
	LastError      datatypes.JSONType[*RunLastError]                `json:"last_error"`
	ExpiresAt      int                                              `json:"expires_at,omitempty"`
	StartedAt      *int                                             `json:"started_at,omitempty"`
	CancelledAt    *int                                             `json:"cancelled_at,omitempty"`
	CompletedAt    *int                                             `json:"completed_at,omitempty"`
	FailedAt       *int                                             `json:"failed_at,omitempty"`
	Model          string                                           `json:"model"`
	Instructions   string                                           `json:"instructions,omitempty"`
	Tools          datatypes.JSONSlice[openai.RunObject_Tools_Item] `json:"tools"`
	FileIDs        datatypes.JSONSlice[string]                      `json:"file_ids,omitempty"`
	Usage          datatypes.JSONType[*openai.RunCompletionUsage]   `json:"usage"`

	// These are not part of the public API
	ClaimedBy       *string `json:"claimed_by,omitempty"`
	SystemClaimedBy *string `json:"system_claimed_by,omitempty"`
	SystemStatus    *string `json:"system_status,omitempty"`
}

func (r *Run) IDPrefix() string {
	return "run_"
}

func (r *Run) ToPublic() any {
	//nolint:govet
	return &openai.RunObject{
		r.AssistantID,
		r.CancelledAt,
		r.CompletedAt,
		r.CreatedAt,
		r.ExpiresAt,
		r.FailedAt,
		r.FileIDs,
		r.ID,
		r.Instructions,
		r.LastError.Data().toPublic(),
		z.Pointer[map[string]interface{}](r.Metadata.Metadata),
		r.Model,
		openai.ThreadRun,
		r.RequiredAction.Data().toPublic(),
		r.StartedAt,
		openai.RunObjectStatus(r.Status),
		r.ThreadID,
		r.Tools,
		r.Usage.Data(),
	}
}

func (r *Run) FromPublic(obj any) error {
	o, ok := obj.(*openai.RunObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && r != nil {
		if o.Status == "" {
			o.Status = openai.RunObjectStatusQueued
		}
		var requiredAction *RunRequiredAction
		if o.RequiredAction != nil {
			requiredAction = &RunRequiredAction{
				SubmitToolOutputs: o.RequiredAction.SubmitToolOutputs.ToolCalls,
				Type:              o.RequiredAction.Type,
			}
		}
		var lastError *RunLastError
		if o.LastError != nil {
			lastError = &RunLastError{
				Code:    string(o.LastError.Code),
				Message: o.LastError.Message,
			}
		}

		*r = Run{
			//nolint:govet
			Metadata{
				Base{
					o.Id,
					o.CreatedAt,
				},
				z.Dereference(o.Metadata),
			},
			o.AssistantId,
			o.ThreadId,
			string(o.Status),
			datatypes.NewJSONType(requiredAction),
			datatypes.NewJSONType(lastError),
			o.ExpiresAt,
			o.StartedAt,
			o.CancelledAt,
			o.CompletedAt,
			o.FailedAt,
			o.Model,
			o.Instructions,
			datatypes.NewJSONSlice(o.Tools),
			o.FileIds,
			datatypes.NewJSONType(o.Usage),

			nil,
			nil,
			nil,
		}
	}

	return nil
}

type RunLastError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r *RunLastError) toPublic() *struct {
	Code    openai.RunObjectLastErrorCode `json:"code"`
	Message string                        `json:"message"`
} {
	if r == nil {
		return nil
	}

	return &struct {
		Code    openai.RunObjectLastErrorCode `json:"code"`
		Message string                        `json:"message"`
	}{
		Code:    openai.RunObjectLastErrorCode(r.Code),
		Message: r.Message,
	}
}

type RunRequiredAction struct {
	SubmitToolOutputs []openai.RunToolCallObject         `json:"submit_tool_outputs"`
	Type              openai.RunObjectRequiredActionType `json:"type"`
}

func (r *RunRequiredAction) toPublic() *struct {
	SubmitToolOutputs struct {
		ToolCalls []openai.RunToolCallObject `json:"tool_calls"`
	} `json:"submit_tool_outputs"`
	Type openai.RunObjectRequiredActionType `json:"type"`
} {
	if r == nil {
		return nil
	}

	return &struct {
		SubmitToolOutputs struct {
			ToolCalls []openai.RunToolCallObject `json:"tool_calls"`
		} `json:"submit_tool_outputs"`
		Type openai.RunObjectRequiredActionType `json:"type"`
	}{
		SubmitToolOutputs: struct {
			ToolCalls []openai.RunToolCallObject `json:"tool_calls"`
		}{
			ToolCalls: r.SubmitToolOutputs,
		},
		Type: r.Type,
	}
}
