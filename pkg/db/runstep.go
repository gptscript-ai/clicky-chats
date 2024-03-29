package db

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/tools"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type RunStep struct {
	Metadata    `json:",inline"`
	AssistantID string                                               `json:"assistant_id"`
	CancelledAt *int                                                 `json:"cancelled_at"`
	CompletedAt *int                                                 `json:"completed_at"`
	ExpiredAt   *int                                                 `json:"expired_at"`
	FailedAt    *int                                                 `json:"failed_at"`
	LastError   datatypes.JSONType[RunLastError]                     `json:"last_error"`
	RunID       string                                               `json:"run_id"`
	Status      string                                               `json:"status"`
	StepDetails datatypes.JSONType[openai.RunStepObject_StepDetails] `json:"step_details"`
	ThreadID    string                                               `json:"thread_id"`
	Type        string                                               `json:"type"`
	Usage       datatypes.JSONType[*openai.RunStepCompletionUsage]   `json:"usage"`

	// These are not part of the public API
	ClaimedBy          *string `json:"claimed_by,omitempty"`
	RunnerType         *string `json:"runner_type,omitempty"`
	RetrievalArguments string  `json:"retrieval_arguments,omitempty"`
}

func (r *RunStep) IDPrefix() string {
	return "step_"
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
		openai.RunStepObjectObjectThreadRunStep,
		r.RunID,
		openai.RunStepObjectStatus(r.Status),
		r.StepDetails.Data(),
		r.ThreadID,
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

			nil,
			nil,
			"",
		}
	}

	return nil
}

// Merge will merge the given chunk into the current run step.
func (r *RunStep) Merge(toolCalls *[]GenericToolCallInfo, chunk ChatCompletionResponseChunk) (*RunStepDelta, error) {
	chunkChoice := chunk.Choices[0]
	delta := chunkChoice.Delta.Data()

	var runStepDelta *RunStepDelta
	if delta.ToolCalls != nil {
		for _, chunkTC := range *delta.ToolCalls {
			// Expand the tool calls slice so that the index is valid.
			*toolCalls = expandSlice(*toolCalls, chunkTC.Index)
			tc := (*toolCalls)[chunkTC.Index]

			if id := z.Dereference(chunkTC.Id); id != "" {
				tc.ID = id
			}

			if chunkFunction := chunkTC.Function; chunkFunction != nil {
				if name := z.Dereference(chunkFunction.Name); name != "" {
					tc.Name += name
				}
				args := chunkFunction.Arguments
				if a := z.Dereference(args); a != "" {
					tc.Arguments += a
				}

				deltaStepToolCall := new(openai.RunStepDeltaStepDetailsToolCallsObject_ToolCalls_Item)

				switch strings.TrimPrefix(tc.Name, tools.GPTScriptToolNamePrefix) {
				case "code_interpreter":
					//nolint:govet
					if err := deltaStepToolCall.FromRunStepDeltaStepDetailsToolCallsCodeObject(openai.RunStepDeltaStepDetailsToolCallsCodeObject{
						&struct {
							Input   *string                                                                           `json:"input,omitempty"`
							Outputs *[]openai.RunStepDeltaStepDetailsToolCallsCodeObject_CodeInterpreter_Outputs_Item `json:"outputs,omitempty"`
						}{
							Input: args,
						},
						&tc.ID,
						chunkTC.Index,
						openai.RunStepDeltaStepDetailsToolCallsCodeObjectTypeCodeInterpreter,
					}); err != nil {
						return nil, err
					}

				case "retrieval":
					//nolint:govet
					if err := deltaStepToolCall.FromRunStepDeltaStepDetailsToolCallsRetrievalObject(openai.RunStepDeltaStepDetailsToolCallsRetrievalObject{
						&tc.ID,
						chunkTC.Index,
						z.Pointer(make(map[string]interface{})),
						openai.RunStepDeltaStepDetailsToolCallsRetrievalObjectTypeRetrieval,
					}); err != nil {
						return nil, err
					}

				default:
					//nolint:govet
					if err := deltaStepToolCall.FromRunStepDeltaStepDetailsToolCallsFunctionObject(openai.RunStepDeltaStepDetailsToolCallsFunctionObject{
						&struct {
							Arguments *string `json:"arguments,omitempty"`
							Name      *string `json:"name,omitempty"`
							Output    *string `json:"output"`
						}{
							args,
							&tc.Name,
							nil,
						},
						&tc.ID,
						chunkTC.Index,
						openai.RunStepDeltaStepDetailsToolCallsFunctionObjectTypeFunction,
					}); err != nil {
						return nil, err
					}
				}

				stepDetails := new(openai.RunStepDeltaObject_Delta_StepDetails)
				//nolint:govet
				if err := stepDetails.FromRunStepDeltaStepDetailsToolCallsObject(openai.RunStepDeltaStepDetailsToolCallsObject{
					&[]openai.RunStepDeltaStepDetailsToolCallsObject_ToolCalls_Item{*deltaStepToolCall},
					openai.RunStepDeltaStepDetailsToolCallsObjectTypeToolCalls,
				}); err != nil {
					return nil, err
				}
				runStepDelta = &RunStepDelta{
					tc.ID,
					datatypes.NewJSONType(RunStepDeltaDelta{StepDetails: stepDetails}),
				}
			}

			(*toolCalls)[chunkTC.Index] = tc

			toolCall, err := runStepFromGenericToolCallInfo(tc)
			if err != nil {
				return nil, err
			}

			stepDetails := r.StepDetails.Data()
			//nolint:govet
			if err = stepDetails.FromRunStepDetailsToolCallsObject(openai.RunStepDetailsToolCallsObject{
				[]openai.RunStepDetailsToolCallsObject_ToolCalls_Item{*toolCall},
				openai.RunStepDetailsToolCallsObjectTypeToolCalls,
			}); err != nil {
				return nil, err
			}

			r.StepDetails = datatypes.NewJSONType(stepDetails)
		}
	}

	return runStepDelta, nil
}

func (r *RunStep) BeforeUpdate(tx *gorm.DB) error {
	if !tx.Statement.Changed("status") {
		return nil
	}

	existing := new(RunStep)
	if err := tx.First(existing, tx.Statement.Clauses["WHERE"].Expression).Error; err != nil {
		return err
	}

	if isTerminal(existing.Status) {
		return fmt.Errorf("cannot update runstep %s in terminal state %s", existing.ID, existing.Status)
	}

	return nil
}

func (r *RunStep) GetRunStepFunctionCalls() ([]openai.RunStepDetailsToolCallsFunctionObject, error) {
	runStepDetails, err := ExtractRunStepDetails(r.StepDetails.Data())
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

type RunStepDetailsFunction struct {
	Arguments string  `json:"arguments"`
	Name      string  `json:"name"`
	Output    *string `json:"output"`
}

func ExtractRunStepDetails(details openai.RunStepObject_StepDetails) (any, error) {
	if tc, err := details.AsRunStepDetailsToolCallsObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsObjectTypeToolCalls {
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

func SetOutputForRunStepToolCall(item *openai.RunStepDetailsToolCallsObject_ToolCalls_Item, output string) error {
	if tc, err := item.AsRunStepDetailsToolCallsFunctionObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsFunctionObjectTypeFunction {
		tc.Function.Output = z.Pointer(output)
		return item.FromRunStepDetailsToolCallsFunctionObject(tc)
	}
	if tc, err := item.AsRunStepDetailsToolCallsCodeObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsCodeObjectTypeCodeInterpreter {
		if err = json.Unmarshal([]byte(fmt.Sprintf(`[{"logs":%q,"type":"logs"}]`, output)), &tc.CodeInterpreter.Outputs); err != nil {
			return err
		}
		return item.FromRunStepDetailsToolCallsCodeObject(tc)
	}
	if tc, err := item.AsRunStepDetailsToolCallsRetrievalObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsRetrievalObjectTypeRetrieval {
		if err = json.Unmarshal([]byte(output), &tc.Retrieval); err != nil {
			return err
		}

		return item.FromRunStepDetailsToolCallsRetrievalObject(tc)
	}

	return fmt.Errorf("failed to extract tool call item")
}

func GetOutputForRunStepToolCall(item *openai.RunStepDetailsToolCallsObject_ToolCalls_Item) (GenericToolCallInfo, error) {
	info := GenericToolCallInfo{}
	if tc, err := item.AsRunStepDetailsToolCallsFunctionObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsFunctionObjectTypeFunction {
		info.ID = tc.Id
		info.Name = tc.Function.Name
		info.Arguments = tc.Function.Arguments
		info.Output = z.Dereference(tc.Function.Output)
		return info, nil
	}
	if tc, err := item.AsRunStepDetailsToolCallsCodeObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsCodeObjectTypeCodeInterpreter {
		var logs openai.RunStepDetailsToolCallsCodeOutputLogsObject
		if len(tc.CodeInterpreter.Outputs) != 0 {
			logs, err = tc.CodeInterpreter.Outputs[0].AsRunStepDetailsToolCallsCodeOutputLogsObject()
		}
		info.ID = tc.Id
		info.Name = tools.GPTScriptToolNamePrefix + string(openai.RunStepDetailsToolCallsCodeObjectTypeCodeInterpreter)
		info.Arguments = tc.CodeInterpreter.Input
		info.Output = logs.Logs
		return info, err
	}
	if tc, err := item.AsRunStepDetailsToolCallsRetrievalObject(); err == nil && tc.Type == openai.RunStepDetailsToolCallsRetrievalObjectTypeRetrieval {
		b, err := json.Marshal(tc.Retrieval)
		info.ID = tc.Id
		info.Name = tools.GPTScriptToolNamePrefix + string(openai.RunStepDetailsToolCallsRetrievalObjectTypeRetrieval)
		info.Arguments = string(b)
		info.Output = ""
		return info, err
	}

	return info, fmt.Errorf("failed to extract tool call output")
}

func runStepFromGenericToolCallInfo(info GenericToolCallInfo) (*openai.RunStepDetailsToolCallsObject_ToolCalls_Item, error) {
	item := new(openai.RunStepDetailsToolCallsObject_ToolCalls_Item)
	name := strings.TrimPrefix(info.Name, tools.GPTScriptToolNamePrefix)
	if name == string(openai.RunStepDetailsToolCallsCodeObjectTypeCodeInterpreter) {
		//nolint:govet
		return item, item.FromRunStepDetailsToolCallsCodeObject(openai.RunStepDetailsToolCallsCodeObject{
			struct {
				Input   string                                                                  `json:"input"`
				Outputs []openai.RunStepDetailsToolCallsCodeObject_CodeInterpreter_Outputs_Item `json:"outputs"`
			}{
				Input: info.Arguments,
			},
			info.ID,
			openai.RunStepDetailsToolCallsCodeObjectTypeCodeInterpreter,
		})
	} else if name == string(openai.RunStepDetailsToolCallsRetrievalObjectTypeRetrieval) {
		//nolint:govet
		return item, item.FromRunStepDetailsToolCallsRetrievalObject(openai.RunStepDetailsToolCallsRetrievalObject{
			info.ID,
			make(map[string]any),
			openai.RunStepDetailsToolCallsRetrievalObjectTypeRetrieval,
		})
	}

	//nolint:govet
	return item, item.FromRunStepDetailsToolCallsFunctionObject(openai.RunStepDetailsToolCallsFunctionObject{
		struct {
			Arguments string  `json:"arguments"`
			Name      string  `json:"name"`
			Output    *string `json:"output"`
		}{
			Arguments: info.Arguments,
			Name:      info.Name,
			Output:    nil,
		},
		info.ID,
		openai.RunStepDetailsToolCallsFunctionObjectTypeFunction,
	})
}

type GenericToolCallInfo struct {
	ID        string
	Name      string
	Arguments string
	Output    string
}
