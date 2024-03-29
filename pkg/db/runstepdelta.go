package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type RunStepDelta struct {
	ID    string                                `json:"id"`
	Delta datatypes.JSONType[RunStepDeltaDelta] `json:"delta"`
}

func (r *RunStepDelta) ToPublic() any {
	//nolint:govet
	return &openai.RunStepDeltaObject{
		r.Delta.Data(),
		r.ID,
		openai.RunStepDeltaObjectObjectThreadRunStepDelta,
	}
}

func (r *RunStepDelta) FromPublic(obj any) error {
	o, ok := obj.(*openai.RunStepDeltaObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && r != nil {
		//nolint:govet
		*r = RunStepDelta{
			o.Id,
			datatypes.NewJSONType[RunStepDeltaDelta](o.Delta),
		}
	}

	return nil
}

type RunStepDeltaDelta struct {
	StepDetails *openai.RunStepDeltaObject_Delta_StepDetails `json:"step_details,omitempty"`
}

func EmitRunStepDeltaOutputEvent(gbd *gorm.DB, run *Run, toolCall *openai.RunStepDetailsToolCallsObject_ToolCalls_Item, index int) error {
	deltaToolCalls, err := extractToolCallOutputToDeltaToolCall(toolCall, index)
	if err != nil {
		return err
	}

	deltaStepDetails := new(openai.RunStepDeltaObject_Delta_StepDetails)
	if err := deltaStepDetails.FromRunStepDeltaStepDetailsToolCallsObject(openai.RunStepDeltaStepDetailsToolCallsObject{
		ToolCalls: z.Pointer([]openai.RunStepDeltaStepDetailsToolCallsObject_ToolCalls_Item{*deltaToolCalls}),
		Type:      openai.RunStepDeltaStepDetailsToolCallsObjectTypeToolCalls,
	}); err != nil {
		return err
	}

	run.EventIndex++
	runEvent := &RunEvent{
		JobResponse: JobResponse{
			RequestID: run.ID,
		},
		EventName:   string(openai.RunStepDeltaObjectObjectThreadRunStepDelta),
		ResponseIdx: run.EventIndex,
		RunStepDelta: datatypes.NewJSONType(&RunStepDelta{
			Delta: datatypes.NewJSONType(RunStepDeltaDelta{
				StepDetails: deltaStepDetails,
			}),
		}),
	}

	if err := Create(gbd, runEvent); err != nil {
		return err
	}

	return nil
}

func extractToolCallOutputToDeltaToolCall(toolCall *openai.RunStepDetailsToolCallsObject_ToolCalls_Item, index int) (*openai.RunStepDeltaStepDetailsToolCallsObject_ToolCalls_Item, error) {
	deltaToolCalls := new(openai.RunStepDeltaStepDetailsToolCallsObject_ToolCalls_Item)
	info, err := GetOutputForRunStepToolCall(toolCall)
	if err != nil {
		return nil, err
	}

	switch info.Name {
	case string(openai.RunStepDeltaStepDetailsToolCallsCodeObjectTypeCodeInterpreter):
		code := openai.RunStepDeltaStepDetailsToolCallsCodeObject{
			CodeInterpreter: &struct {
				Input   *string                                                                           `json:"input,omitempty"`
				Outputs *[]openai.RunStepDeltaStepDetailsToolCallsCodeObject_CodeInterpreter_Outputs_Item `json:"outputs,omitempty"`
			}{
				Input: &info.Arguments,
			},
			Id:    &info.ID,
			Index: index,
			Type:  openai.RunStepDeltaStepDetailsToolCallsCodeObjectTypeCodeInterpreter,
		}

		outputs := new(openai.RunStepDeltaStepDetailsToolCallsCodeObject_CodeInterpreter_Outputs_Item)
		//nolint:govet
		if err = outputs.FromRunStepDeltaStepDetailsToolCallsCodeOutputLogsObject(openai.RunStepDeltaStepDetailsToolCallsCodeOutputLogsObject{
			index,
			&info.Output,
			openai.RunStepDeltaStepDetailsToolCallsCodeOutputLogsObjectTypeLogs,
		}); err != nil {
			return nil, err
		}
		code.CodeInterpreter.Outputs = z.Pointer([]openai.RunStepDeltaStepDetailsToolCallsCodeObject_CodeInterpreter_Outputs_Item{*outputs})

		return deltaToolCalls, deltaToolCalls.FromRunStepDeltaStepDetailsToolCallsCodeObject(code)

	case string(openai.RunStepDeltaStepDetailsToolCallsRetrievalObjectTypeRetrieval):
		retrieval := openai.RunStepDeltaStepDetailsToolCallsRetrievalObject{
			Id:        &info.ID,
			Index:     index,
			Type:      openai.RunStepDeltaStepDetailsToolCallsRetrievalObjectTypeRetrieval,
			Retrieval: z.Pointer(make(map[string]any)),
		}

		return deltaToolCalls, deltaToolCalls.FromRunStepDeltaStepDetailsToolCallsRetrievalObject(retrieval)
	default:
		functionCall := openai.RunStepDeltaStepDetailsToolCallsFunctionObject{
			Id:    &info.ID,
			Index: index,
			Type:  openai.RunStepDeltaStepDetailsToolCallsFunctionObjectTypeFunction,
			Function: &struct {
				Arguments *string `json:"arguments,omitempty"`
				Name      *string `json:"name,omitempty"`
				Output    *string `json:"output"`
			}{
				Arguments: &info.Arguments,
				Name:      &info.Name,
				Output:    &info.Output,
			},
		}

		return deltaToolCalls, deltaToolCalls.FromRunStepDeltaStepDetailsToolCallsFunctionObject(functionCall)
	}
}
