package db

import (
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type RunEvent struct {
	JobResponse `json:",inline"`
	Base        `json:",inline"`

	EventName    string                            `json:"event_name"`
	Run          datatypes.JSONType[*Run]          `json:"run,omitempty"`
	Thread       datatypes.JSONType[*Thread]       `json:"thread,omitempty"`
	RunStep      datatypes.JSONType[*RunStep]      `json:"run_step,omitempty"`
	RunStepDelta datatypes.JSONType[*RunStepDelta] `json:"run_step_delta,omitempty"`
	Message      datatypes.JSONType[*Message]      `json:"message,omitempty"`
	MessageDelta datatypes.JSONType[*MessageDelta] `json:"message_delta,omitempty"`
	ResponseIdx  int                               `json:"response_idx"`
}

func (r *RunEvent) IDPrefix() string {
	return "run-event_"
}

func (r *RunEvent) GetIndex() int {
	return r.ResponseIdx
}

func (r *RunEvent) GetEvent() string {
	return r.EventName
}

func (r *RunEvent) ToPublic() any {
	switch {
	case r.Run.Data() != nil:
		return r.Run.Data().ToPublic()
	case r.Thread.Data() != nil:
		return r.Thread.Data().ToPublic()
	case r.RunStep.Data() != nil:
		return r.RunStep.Data().ToPublic()
	case r.RunStepDelta.Data() != nil:
		return r.RunStepDelta.Data().ToPublic()
	case r.Message.Data() != nil:
		return r.Message.Data().ToPublic()
	case r.MessageDelta.Data() != nil:
		return r.MessageDelta.Data().ToPublic()
	default:
		return nil
	}
}

func (r *RunEvent) FromPublic(obj any) error {
	var out transformer

	switch obj.(type) {
	case *openai.RunObject:
		r.Run = datatypes.NewJSONType[*Run](new(Run))
		out = r.Run.Data()
	case *openai.ThreadObject:
		r.Thread = datatypes.NewJSONType[*Thread](new(Thread))
		out = r.Thread.Data()
	case *openai.RunStepObject:
		r.RunStep = datatypes.NewJSONType[*RunStep](new(RunStep))
		out = r.RunStep.Data()
	case *openai.RunStepDeltaObject:
		r.RunStepDelta = datatypes.NewJSONType[*RunStepDelta](new(RunStepDelta))
		out = r.RunStepDelta.Data()
	case *openai.MessageObject:
		r.Message = datatypes.NewJSONType[*Message](new(Message))
		out = r.Message.Data()
	case *openai.MessageDeltaObject:
		r.MessageDelta = datatypes.NewJSONType[*MessageDelta](new(MessageDelta))
		out = r.MessageDelta.Data()
	default:
		return InvalidTypeError{Expected: nil, Got: obj}
	}

	return out.FromPublic(obj)
}

type transformer interface {
	ToPublic() any
	FromPublic(any) error
}
