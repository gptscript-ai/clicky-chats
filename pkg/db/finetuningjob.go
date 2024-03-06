package db

import (
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
)

type FineTuningJob struct {
	Base            `json:",inline"`
	Error           datatypes.JSONType[FineTuningJobError]           `json:"error"`
	FineTunedModel  *string                                          `json:"fine_tuned_model"`
	FinishedAt      *int                                             `json:"finished_at"`
	Hyperparameters datatypes.JSONType[FineTuningJobHyperParameters] `json:"hyperparameters"`
	Model           string                                           `json:"model"`
	OrganizationID  string                                           `json:"organization_id"`
	ResultFiles     datatypes.JSONSlice[string]                      `json:"result_files"`
	Status          string                                           `json:"status"`
	TrainedTokens   *int                                             `json:"trained_tokens"`
	TrainingFile    string                                           `json:"training_file"`
	ValidationFile  *string                                          `json:"validation_file"`
}

func (f *FineTuningJob) IDPrefix() string {
	return "ftjob-"
}

func (f *FineTuningJob) ToPublic() any {
	//nolint:govet
	return &openai.FineTuningJob{
		f.CreatedAt,
		&struct {
			Code    string  `json:"code"`
			Message string  `json:"message"`
			Param   *string `json:"param"`
		}{
			f.Error.Data().Code,
			f.Error.Data().Message,
			f.Error.Data().Param,
		},
		f.FineTunedModel,
		f.FinishedAt,
		struct {
			NEpochs openai.FineTuningJob_Hyperparameters_NEpochs `json:"n_epochs"`
		}{
			f.Hyperparameters.Data().NEpochs.Data(),
		},
		f.ID,
		f.Model,
		openai.FineTuningJobObjectFineTuningJob,
		f.OrganizationID,
		f.ResultFiles,
		openai.FineTuningJobStatus(f.Status),
		f.TrainedTokens,
		f.TrainingFile,
		f.ValidationFile,
	}
}

func (f *FineTuningJob) FromPublic(obj any) error {
	o, ok := obj.(*openai.FineTuningJob)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	var err FineTuningJobError
	if o.Error != nil {
		err = FineTuningJobError{
			Code:    o.Error.Code,
			Message: o.Error.Message,
			Param:   o.Error.Param,
		}
	}
	if o != nil && f != nil {
		//nolint:govet
		*f = FineTuningJob{
			Base{
				o.Id,
				o.CreatedAt,
			},
			datatypes.NewJSONType(err),
			o.FineTunedModel,
			o.FinishedAt,
			datatypes.NewJSONType(FineTuningJobHyperParameters{
				datatypes.NewJSONType(o.Hyperparameters.NEpochs),
			}),
			o.Model,
			o.OrganizationId,
			o.ResultFiles,
			string(o.Status),
			o.TrainedTokens,
			o.TrainingFile,
			o.ValidationFile,
		}
	}

	return nil
}

type FineTuningJobError struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Param   *string `json:"param"`
}

type FineTuningJobHyperParameters struct {
	NEpochs datatypes.JSONType[openai.FineTuningJob_Hyperparameters_NEpochs] `json:"n_epochs"`
}
