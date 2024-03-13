package server

import (
	"net/http"

	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
)

func (s *Server) ListAssistants(w http.ResponseWriter, r *http.Request, params openai.ListAssistantsParams) {
	//nolint:govet
	s.ExtendedListAssistants(w, r, openai.ExtendedListAssistantsParams{
		params.Limit,
		(*openai.ExtendedListAssistantsParamsOrder)(params.Order),
		params.After,
		params.Before,
	})
}

func (s *Server) CreateAssistant(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateAssistant(w, r)
}

func (s *Server) DeleteAssistant(w http.ResponseWriter, r *http.Request, assistantID string) {
	s.ExtendedDeleteAssistant(w, r, assistantID)
}

func (s *Server) GetAssistant(w http.ResponseWriter, r *http.Request, assistantID string) {
	s.ExtendedGetAssistant(w, r, assistantID)
}

func (s *Server) ModifyAssistant(w http.ResponseWriter, r *http.Request, assistantID string) {
	s.ExtendedModifyAssistant(w, r, assistantID)
}

func (s *Server) ListAssistantFiles(w http.ResponseWriter, r *http.Request, assistantID string, params openai.ListAssistantFilesParams) {
	//nolint:govet
	s.ExtendedListAssistantFiles(w, r, assistantID, openai.ExtendedListAssistantFilesParams{
		params.Limit,
		(*openai.ExtendedListAssistantFilesParamsOrder)(params.Order),
		params.After,
		params.Before,
	})
}

func (s *Server) CreateAssistantFile(w http.ResponseWriter, r *http.Request, assistantID string) {
	s.ExtendedCreateAssistantFile(w, r, assistantID)
}

func (s *Server) DeleteAssistantFile(w http.ResponseWriter, r *http.Request, assistantID string, fileID string) {
	s.ExtendedDeleteAssistantFile(w, r, assistantID, fileID)
}

func (s *Server) GetAssistantFile(w http.ResponseWriter, r *http.Request, assistantID string, fileID string) {
	s.ExtendedGetAssistantFile(w, r, assistantID, fileID)
}

func (s *Server) CreateSpeech(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateSpeech(w, r)
}

func (s *Server) CreateTranscription(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateTranscription(w, r)
}

func (s *Server) CreateTranslation(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateTranslation(w, r)
}

func (s *Server) CreateChatCompletion(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateChatCompletion(w, r)
}

func (s *Server) CreateCompletion(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateCompletion(w, r)
}

func (s *Server) CreateEmbedding(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateEmbedding(w, r)
}

func (s *Server) ListFiles(w http.ResponseWriter, r *http.Request, params openai.ListFilesParams) {
	//nolint:govet
	s.ExtendedListFiles(w, r, openai.ExtendedListFilesParams(params))
}

func (s *Server) CreateFile(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateFile(w, r)
}

func (s *Server) DeleteFile(w http.ResponseWriter, r *http.Request, fileID string) {
	s.ExtendedDeleteFile(w, r, fileID)
}

func (s *Server) RetrieveFile(w http.ResponseWriter, r *http.Request, fileID string) {
	s.ExtendedRetrieveFile(w, r, fileID)
}

func (s *Server) DownloadFile(w http.ResponseWriter, r *http.Request, fileID string) {
	s.ExtendedDownloadFile(w, r, fileID)
}

func (s *Server) ListPaginatedFineTuningJobs(w http.ResponseWriter, r *http.Request, params openai.ListPaginatedFineTuningJobsParams) {
	//nolint:govet
	s.ExtendedListPaginatedFineTuningJobs(w, r, openai.ExtendedListPaginatedFineTuningJobsParams(params))
}

func (s *Server) CreateFineTuningJob(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateFineTuningJob(w, r)
}

func (s *Server) RetrieveFineTuningJob(w http.ResponseWriter, r *http.Request, fineTuningJobID string) {
	s.ExtendedRetrieveFineTuningJob(w, r, fineTuningJobID)
}

func (s *Server) CancelFineTuningJob(w http.ResponseWriter, r *http.Request, fineTuningJobID string) {
	s.ExtendedCancelFineTuningJob(w, r, fineTuningJobID)
}

func (s *Server) ListFineTuningEvents(w http.ResponseWriter, r *http.Request, fineTuningJobID string, params openai.ListFineTuningEventsParams) {
	//nolint:govet
	s.ExtendedListFineTuningEvents(w, r, fineTuningJobID, openai.ExtendedListFineTuningEventsParams(params))
}

func (s *Server) CreateImageEdit(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateImageEdit(w, r)
}

func (s *Server) CreateImage(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateImage(w, r)
}

func (s *Server) CreateImageVariation(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateImageVariation(w, r)
}

func (s *Server) ListModels(w http.ResponseWriter, r *http.Request) {
	s.ExtendedListModels(w, r)
}

func (s *Server) DeleteModel(w http.ResponseWriter, r *http.Request, model string) {
	s.ExtendedDeleteModel(w, r, model)
}

func (s *Server) RetrieveModel(w http.ResponseWriter, r *http.Request, model string) {
	s.ExtendedRetrieveModel(w, r, model)
}

func (s *Server) CreateModeration(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateModeration(w, r)
}

func (s *Server) CreateThread(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateThread(w, r)
}

func (s *Server) CreateThreadAndRun(w http.ResponseWriter, r *http.Request) {
	s.ExtendedCreateThreadAndRun(w, r)
}

func (s *Server) DeleteThread(w http.ResponseWriter, r *http.Request, threadID string) {
	s.ExtendedDeleteThread(w, r, threadID)
}

func (s *Server) GetThread(w http.ResponseWriter, r *http.Request, threadID string) {
	s.ExtendedGetThread(w, r, threadID)
}

func (s *Server) ModifyThread(w http.ResponseWriter, r *http.Request, threadID string) {
	s.ExtendedModifyThread(w, r, threadID)
}

func (s *Server) ListMessages(w http.ResponseWriter, r *http.Request, threadID string, params openai.ListMessagesParams) {
	//nolint:govet
	s.ExtendedListMessages(w, r, threadID, openai.ExtendedListMessagesParams{
		params.Limit,
		(*openai.ExtendedListMessagesParamsOrder)(params.Order),
		params.After,
		params.Before,
	})
}

func (s *Server) CreateMessage(w http.ResponseWriter, r *http.Request, threadID string) {
	s.ExtendedCreateMessage(w, r, threadID)
}

func (s *Server) GetMessage(w http.ResponseWriter, r *http.Request, threadID string, messageID string) {
	s.ExtendedGetMessage(w, r, threadID, messageID)
}

func (s *Server) ModifyMessage(w http.ResponseWriter, r *http.Request, threadID string, messageID string) {
	s.ExtendedModifyMessage(w, r, threadID, messageID)
}

func (s *Server) ListMessageFiles(w http.ResponseWriter, r *http.Request, threadID string, messageID string, params openai.ListMessageFilesParams) {
	//nolint:govet
	s.ExtendedListMessageFiles(w, r, threadID, messageID, openai.ExtendedListMessageFilesParams{
		params.Limit,
		(*openai.ExtendedListMessageFilesParamsOrder)(params.Order),
		params.After,
		params.Before,
	})
}

func (s *Server) GetMessageFile(w http.ResponseWriter, r *http.Request, threadID string, messageID string, fileID string) {
	s.ExtendedGetMessageFile(w, r, threadID, messageID, fileID)
}

func (s *Server) ListRuns(w http.ResponseWriter, r *http.Request, threadID string, params openai.ListRunsParams) {
	//nolint:govet
	s.ExtendedListRuns(w, r, threadID, openai.ExtendedListRunsParams{
		params.Limit,
		(*openai.ExtendedListRunsParamsOrder)(params.Order),
		params.After,
		params.Before,
	})
}

func (s *Server) CreateRun(w http.ResponseWriter, r *http.Request, threadID string) {
	s.ExtendedCreateRun(w, r, threadID)
}

func (s *Server) GetRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	s.ExtendedGetRun(w, r, threadID, runID)
}

func (s *Server) ModifyRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	s.ExtendedModifyRun(w, r, threadID, runID)
}

func (s *Server) CancelRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	s.ExtendedCancelRun(w, r, threadID, runID)
}

func (s *Server) ListRunSteps(w http.ResponseWriter, r *http.Request, threadID string, runID string, params openai.ListRunStepsParams) {
	//nolint:govet
	s.ExtendedListRunSteps(w, r, threadID, runID, openai.ExtendedListRunStepsParams{
		params.Limit,
		(*openai.ExtendedListRunStepsParamsOrder)(params.Order),
		params.After,
		params.Before,
	})
}

func (s *Server) GetRunStep(w http.ResponseWriter, r *http.Request, threadID string, runID string, stepID string) {
	s.ExtendedGetRunStep(w, r, threadID, runID, stepID)
}

func (s *Server) SubmitToolOuputsToRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	s.ExtendedSubmitToolOuputsToRun(w, r, threadID, runID)
}
