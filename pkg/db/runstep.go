package db

import (
	"fmt"

	"github.com/acorn-io/z"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type RunStep struct {
	Metadata    `json:",inline"`
	AssistantID string                                               `json:"assistant_id"`
	CancelledAt *int                                                 `json:"cancelled_at"`
	CompletedAt *int                                                 `json:"completed_at"`
	ExpiredAt   *int                                                 `json:"expired_at"`
	FailedAt    *int                                                 `json:"failed_at"`
	LastError   datatypes.JSONType[RunLastError]                     `json:"last_error"`
	RunId       string                                               `json:"run_id"`
	Status      string                                               `json:"status"`
	StepDetails datatypes.JSONType[openai.RunStepObject_StepDetails] `json:"step_details"`
	ThreadId    string                                               `json:"thread_id"`
	Type        string                                               `json:"type"`
	Usage       datatypes.JSONType[*openai.RunStepCompletionUsage]   `json:"usage"`
}

func (r *RunStep) ToPublic() any {
	lastError := r.LastError.Data()
	//nolint:govet
	return &openai.RunStepObject{
		r.AssistantID,
		r.CancelledAt,
		r.CompletedAt,
		r.CreatedAt,
		r.ExpiredAt,
		r.FailedAt,
		r.ID,
		&struct {
			Code    openai.RunStepObjectLastErrorCode `json:"code"`
			Message string                            `json:"message"`
		}{
			Code:    openai.RunStepObjectLastErrorCode(lastError.Code),
			Message: lastError.Message,
		},
		z.Pointer[map[string]interface{}](r.Metadata.Metadata),
		openai.ThreadRunStep,
		r.RunId,
		openai.RunStepObjectStatus(r.Status),
		r.StepDetails.Data(),
		r.ThreadId,
		openai.RunStepObjectType(r.Type),
		r.Usage.Data(),
	}
}

func (r *RunStep) FromPublic(obj any) error {
	o, ok := obj.(*openai.RunStepObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && r != nil {
		var lastError RunLastError
		if o.LastError != nil {
			lastError = RunLastError{
				Code:    string(o.LastError.Code),
				Message: o.LastError.Message,
			}
		}

		//nolint:govet
		*r = RunStep{
			Metadata{
				Base{
					o.Id,
					o.CreatedAt,
				},
				z.Dereference(o.Metadata),
			},
			o.AssistantId,
			o.CancelledAt,
			o.CompletedAt,
			o.ExpiredAt,
			o.FailedAt,
			datatypes.NewJSONType(lastError),
			o.RunId,
			string(o.Status),
			datatypes.NewJSONType(o.StepDetails),
			o.ThreadId,
			string(o.Type),
			datatypes.NewJSONType(o.Usage),
		}
	}

	return nil
}

func (r *RunStep) GetRunStepFunctionCalls() ([]openai.RunStepDetailsToolCallsFunctionObject, error) {
	runStepDetails, err := extractRunStepDetails(r.StepDetails.Data())
	if err != nil {
		return nil, err
	}

	runStepDetailsToolCalls, ok := runStepDetails.(openai.RunStepDetailsToolCallsObject)
	if !ok {
		return nil, nil
	}

	toolCalls := make([]openai.RunStepDetailsToolCallsFunctionObject, 0, len(runStepDetailsToolCalls.ToolCalls))
	for _, tc := range runStepDetailsToolCalls.ToolCalls {
		var fc openai.RunStepDetailsToolCallsFunctionObject
		extractedToolCall, err := extractRunStepToolCallItem(tc)
		if fc, ok = extractedToolCall.(openai.RunStepDetailsToolCallsFunctionObject); !ok || err != nil {
			return nil, fmt.Errorf("run step does not contain tool calls: %w", err)
		}
		//golint:govet
		toolCalls = append(toolCalls, fc)
	}

	return toolCalls, nil
}

func RunStepDetailsFromRunRequiredActionToolCalls(runRequiredActions []openai.RunToolCallObject) (*openai.RunStepObject_StepDetails, error) {
	toolCalls := make([]openai.RunStepDetailsToolCallsObject_ToolCalls_Item, 0, len(runRequiredActions))
	for _, tc := range runRequiredActions {
		var runStepToolCallItem openai.RunStepDetailsToolCallsObject_ToolCalls_Item
		var err error
		if tc.Function.Name == string(openai.AssistantToolsCodeTypeCodeInterpreter) {
			runStepToolCallItem, err = constructRunStepToolCallItem(openai.RunStepDetailsToolCallsCodeObject{
				CodeInterpreter: struct {
					Input   string                                                                  `json:"input"`
					Outputs []openai.RunStepDetailsToolCallsCodeObject_CodeInterpreter_Outputs_Item `json:"outputs"`
				}{
					tc.Function.Arguments,
					nil,
				},
				Id:   tc.Id,
				Type: openai.RunStepDetailsToolCallsCodeObjectType(tc.Type),
			})
		} else if tc.Function.Name == string(openai.AssistantToolsRetrievalTypeRetrieval) {
			runStepToolCallItem, err = constructRunStepToolCallItem(openai.RunStepDetailsToolCallsRetrievalObject{
				// For now, this is always going to be an empty object.
				Retrieval: nil,
				Id:        tc.Id,
				Type:      openai.RunStepDetailsToolCallsRetrievalObjectType(tc.Type),
			})
		} else {
			runStepToolCallItem, err = constructRunStepToolCallItem(openai.RunStepDetailsToolCallsFunctionObject{
				Function: RunStepDetailsFunction{
					tc.Function.Arguments,
					tc.Function.Name,
					nil,
				},
				Id:   tc.Id,
				Type: openai.RunStepDetailsToolCallsFunctionObjectType(tc.Type),
			})
		}
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool call id %s: %w", tc.Id, err)
		}

		toolCalls = append(toolCalls, runStepToolCallItem)
	}

	stepDetails := openai.RunStepDetailsToolCallsObject{
		ToolCalls: toolCalls,
		Type:      openai.ToolCalls,
	}

	details := new(openai.RunStepObject_StepDetails)
	if err := details.FromRunStepDetailsToolCallsObject(stepDetails); err != nil {
		return nil, fmt.Errorf("failed to convert step details: %w", err)
	}

	return details, nil
}

type RunStepDetailsFunction struct {
	Arguments string  `json:"arguments"`
	Name      string  `json:"name"`
	Output    *string `json:"output"`
}

func constructRunStepToolCallItem(v any) (openai.RunStepDetailsToolCallsObject_ToolCalls_Item, error) {
	var err error
	runStepToolCallItem := new(openai.RunStepDetailsToolCallsObject_ToolCalls_Item)
	switch v := v.(type) {
	case openai.RunStepDetailsToolCallsFunctionObject:
		err = runStepToolCallItem.FromRunStepDetailsToolCallsFunctionObject(v)
	case openai.RunStepDetailsToolCallsCodeObject:
		err = runStepToolCallItem.FromRunStepDetailsToolCallsCodeObject(v)
	case openai.RunStepDetailsToolCallsRetrievalObject:
		err = runStepToolCallItem.FromRunStepDetailsToolCallsRetrievalObject(v)
	}

	return *runStepToolCallItem, err
}

func extractRunStepDetails(details openai.RunStepObject_StepDetails) (any, error) {
	if tc, err := details.AsRunStepDetailsToolCallsObject(); err == nil && tc.Type == openai.ToolCalls {
		return tc, nil
	}
	if tc, err := details.AsRunStepDetailsMessageCreationObject(); err == nil && tc.Type == openai.RunStepDetailsMessageCreationObjectTypeMessageCreation {
		return tc, nil
	}

	return nil, fmt.Errorf("failed to extract run step details")
}

func extractRunStepToolCallItem(item openai.RunStepDetailsToolCallsObject_ToolCalls_Item) (any, error) {
	if tc, err := item.AsRunStepDetailsToolCallsFunctionObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsFunctionObjectTypeFunction {
		return tc, nil
	}
	if tc, err := item.AsRunStepDetailsToolCallsCodeObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsCodeObjectTypeCodeInterpreter {
		return tc, nil
	}
	if tc, err := item.AsRunStepDetailsToolCallsRetrievalObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsRetrievalObjectTypeRetrieval {
		return tc, nil
	}

	return nil, fmt.Errorf("failed to extract tool call item")
}

func GetFunctionFromRunStepFunctionCall(item openai.RunStepDetailsToolCallsObject_ToolCalls_Item) (RunStepDetailsFunction, string, error) {
	tc, _ := item.AsRunStepDetailsToolCallsFunctionObject()
	return tc.Function, tc.Id, nil
}
