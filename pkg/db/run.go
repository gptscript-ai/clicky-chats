package db

import (
	"github.com/acorn-io/z"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type Run struct {
	ThreadChild    `json:",inline"`
	AssistantID    string                                           `json:"assistant_id"`
	Status         string                                           `json:"status"`
	RequiredAction datatypes.JSONType[RunRequiredAction]            `json:"required_action"`
	LastError      datatypes.JSONType[RunLastError]                 `json:"last_error"`
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
}

func (r *Run) ToPublic() any {
	lastError := r.LastError.Data()

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
		&struct {
			Code    openai.RunObjectLastErrorCode `json:"code"`
			Message string                        `json:"message"`
		}{
			Code:    lastError.Code,
			Message: lastError.Message,
		},
		z.Pointer[map[string]interface{}](r.Metadata.Metadata),
		r.Model,
		openai.ThreadRun,
		&struct {
			SubmitToolOutputs struct {
				ToolCalls []openai.RunToolCallObject `json:"tool_calls"`
			} `json:"submit_tool_outputs"`
			Type openai.RunObjectRequiredActionType `json:"type"`
		}{
			SubmitToolOutputs: struct {
				ToolCalls []openai.RunToolCallObject `json:"tool_calls"`
			}{
				ToolCalls: r.RequiredAction.Data().SubmitToolOutputs.ToolCalls,
			},
			Type: openai.SubmitToolOutputs,
		},
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
		var requiredAction RunRequiredAction
		if o.RequiredAction != nil {
			requiredAction = RunRequiredAction{
				SubmitToolOutputs: SubmitToolOutputs{
					ToolCalls: o.RequiredAction.SubmitToolOutputs.ToolCalls,
				},
				Type: o.RequiredAction.Type,
			}
		}
		var lastError RunLastError
		if o.LastError != nil {
			lastError = RunLastError{
				Code:    o.LastError.Code,
				Message: o.LastError.Message,
			}
		}

		*r = Run{
			//nolint:govet
			ThreadChild{
				Metadata{
					Base{
						o.Id,
						o.CreatedAt,
					},
					z.Dereference(o.Metadata),
				},
				o.ThreadId,
			},
			o.AssistantId,
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
		}
	}

	return nil
}

type RunLastError struct {
	Code    openai.RunObjectLastErrorCode `json:"code"`
	Message string                        `json:"message"`
}

type RunRequiredAction struct {
	SubmitToolOutputs SubmitToolOutputs                  `json:"submit_tool_outputs"`
	Type              openai.RunObjectRequiredActionType `json:"type"`
}

type SubmitToolOutputs struct {
	ToolCalls []openai.RunToolCallObject `json:"tool_calls"`
}
