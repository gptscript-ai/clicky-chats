package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type RunStepDelta struct {
	ID    string                                              `json:"id"`
	Delta datatypes.JSONType[openai.XRunStepDeltaObjectDelta] `json:"delta"`
}

func (r *RunStepDelta) ToPublic() any {
	//nolint:govet
	return &openai.XRunStepDeltaObject{
		r.Delta.Data(),
		r.ID,
		openai.ThreadRunStepDelta,
	}
}

func (r *RunStepDelta) FromPublic(obj any) error {
	o, ok := obj.(*openai.XRunStepDeltaObject)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o != nil && r != nil {
		//nolint:govet
		*r = RunStepDelta{
			o.Id,
			datatypes.NewJSONType(o.Delta),
		}
	}

	return nil
}

func EmitRunStepDeltaOutputEvent(gbd *gorm.DB, run *Run, toolCall openai.RunStepDetailsToolCallsObject_ToolCalls_Item, index int) error {
	deltaToolCalls, err := extractToolCallOutputToDeltaToolCall(toolCall, index)
	if err != nil {
		return err
	}

	deltaStepDetails := new(openai.XRunStepDeltaObjectDelta_StepDetails)
	if err := deltaStepDetails.FromXRunStepDeltaObjectDeltaToolCalls(openai.XRunStepDeltaObjectDeltaToolCalls{
		ToolCalls: []openai.XRunStepDeltaObjectDeltaToolCalls_ToolCalls_Item{*deltaToolCalls},
		Type:      openai.ToolCalls,
	}); err != nil {
		return err
	}

	run.EventIndex++
	runEvent := &RunEvent{
		JobResponse: JobResponse{
			RequestID: run.ID,
		},
		EventName:   ThreadRunStepDeltaEvent,
		ResponseIdx: run.EventIndex,
		RunStepDelta: datatypes.NewJSONType(&RunStepDelta{
			Delta: datatypes.NewJSONType(openai.XRunStepDeltaObjectDelta{
				StepDetails: deltaStepDetails,
			}),
		}),
	}

	if err := Create(gbd, runEvent); err != nil {
		return err
	}

	return nil
}

func extractToolCallOutputToDeltaToolCall(toolCall openai.RunStepDetailsToolCallsObject_ToolCalls_Item, index int) (*openai.XRunStepDeltaObjectDeltaToolCalls_ToolCalls_Item, error) {
	deltaToolCalls := new(openai.XRunStepDeltaObjectDeltaToolCalls_ToolCalls_Item)
	info, err := GetOutputForRunStepToolCall(toolCall)
	if err != nil {
		return nil, err
	}

	switch info.Name {
	case string(openai.RunStepDetailsToolCallsCodeObjectTypeCodeInterpreter):
		code := openai.XRunStepDeltaObjectDeltaToolCallsObjectCode{
			CodeInterpreter: openai.XRunStepDetailsToolCallsCodeObject{
				Outputs: nil,
			},
			Id:    info.ID,
			Index: index,
			Type:  openai.Code,
		}

		outputs := new(openai.XRunStepDetailsToolCallsCodeObject_Outputs_Item)
		if err = outputs.FromXRunStepDetailsToolCallsCodeObjectLogOutput(openai.XRunStepDetailsToolCallsCodeObjectLogOutput{
			Index: index,
			Log:   info.Output,
			Type:  openai.Log,
		}); err != nil {
			return nil, err
		}
		code.CodeInterpreter.Outputs = z.Pointer([]openai.XRunStepDetailsToolCallsCodeObject_Outputs_Item{*outputs})

		return deltaToolCalls, deltaToolCalls.FromXRunStepDeltaObjectDeltaToolCallsObjectCode(code)

	case string(openai.RunStepDetailsToolCallsRetrievalObjectTypeRetrieval):
		retrieval := openai.XRunStepDeltaObjectDeltaToolCallsObjectRetrieval{
			Id:        info.ID,
			Index:     index,
			Type:      openai.Retrieval,
			Retrieval: z.Pointer(make(map[string]any)),
		}

		return deltaToolCalls, deltaToolCalls.FromXRunStepDeltaObjectDeltaToolCallsObjectRetrieval(retrieval)
	default:
		functionCall := openai.XRunStepDeltaObjectDeltaToolCallsObjectFunction{
			Id:    info.ID,
			Index: index,
			Type:  openai.XRunStepDeltaObjectDeltaToolCallsObjectFunctionTypeFunction,
			Function: &openai.XRunStepDeltaDetailsToolCallsFunctionObject{
				Name:   info.Name,
				Output: &info.Output,
			},
		}

		return deltaToolCalls, deltaToolCalls.FromXRunStepDeltaObjectDeltaToolCallsObjectFunction(functionCall)
	}
}
