package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/extendedapi"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func (s *Server) ListAssistants(w http.ResponseWriter, r *http.Request, params openai.ListAssistantsParams) {
	gormDB, limit, err := processAssistantsAPIListParams[*db.Assistant](s.db.WithContext(r.Context()), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	}

	if extendedapi.IsExtendedAPIKey(r.Context()) {
		listAndRespond[*db.Assistant](gormDB, w, limit)
		return
	}
	listAndRespondOpenAI[*db.Assistant](gormDB, w, limit)
}

func (s *Server) CreateAssistant(w http.ResponseWriter, r *http.Request) {
	createAssistantRequest := new(extendedapi.CreateAssistantRequest)
	if err := readObjectFromRequest(r, createAssistantRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err := extendedapi.ValidateGPTScriptTools(createAssistantRequest.GPTScriptTools); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError(err.Error(), InvalidRequestErrorType).Error()))
		return
	}

	model, err := createAssistantRequest.Model.AsCreateAssistantRequestModel0()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process model.", InvalidRequestErrorType).Error()))
		return
	}

	tools := make([]openai.AssistantObject_Tools_Item, 0, len(z.Dereference(createAssistantRequest.Tools)))
	for _, tool := range z.Dereference(createAssistantRequest.Tools) {
		t := new(openai.AssistantObject_Tools_Item)
		if err := transposeObject(tool, t); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Failed to process tool.", InvalidRequestErrorType).Error()))
			return
		}
		tools = append(tools, *t)
	}

	//nolint:govet
	publicAssistant := &extendedapi.Assistant{
		openai.AssistantObject{
			0,
			createAssistantRequest.Description,
			z.Dereference(createAssistantRequest.FileIds),
			"",
			createAssistantRequest.Instructions,
			createAssistantRequest.Metadata,
			model,
			createAssistantRequest.Name,
			openai.AssistantObjectObjectAssistant,
			tools,
		},
		createAssistantRequest.GPTScriptTools,
	}

	if extendedapi.IsExtendedAPIKey(r.Context()) {
		createAndRespond(s.db.WithContext(r.Context()), w, new(db.Assistant), publicAssistant)
		return
	}
	createAndRespondOpenAI(s.db.WithContext(r.Context()), w, new(db.Assistant), publicAssistant)
}

func (s *Server) DeleteAssistant(w http.ResponseWriter, r *http.Request, assistantID string) {
	//nolint:govet
	deleteAndRespond[*db.Assistant](s.db.WithContext(r.Context()), w, assistantID, openai.DeleteAssistantResponse{
		true,
		assistantID,
		openai.AssistantDeleted,
	})
}

func (s *Server) GetAssistant(w http.ResponseWriter, r *http.Request, assistantID string) {
	ctx := r.Context()
	if extendedapi.IsExtendedAPIKey(ctx) {
		getAndRespond(s.db.WithContext(ctx), w, new(db.Assistant), assistantID)
		return
	}
	getAndRespondOpenAI(s.db.WithContext(ctx), w, new(db.Assistant), assistantID)
}

func (s *Server) ModifyAssistant(w http.ResponseWriter, r *http.Request, assistantID string) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("assistant_id").Error()))
		return
	}

	modifyAssistantRequest := new(extendedapi.ModifyAssistantRequest)
	if err := readObjectFromRequest(r, modifyAssistantRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err := extendedapi.ValidateGPTScriptTools(modifyAssistantRequest.GPTScriptTools); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError(err.Error(), InvalidRequestErrorType).Error()))
		return
	}

	if err := validateMetadata(modifyAssistantRequest.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	model, err := modifyAssistantRequest.Model.AsModifyAssistantRequestModel0()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process model.", InvalidRequestErrorType).Error()))
		return
	}

	tools := make([]openai.AssistantObject_Tools_Item, 0, len(*modifyAssistantRequest.Tools))
	for _, tool := range *modifyAssistantRequest.Tools {
		t := new(openai.AssistantObject_Tools_Item)
		if err := transposeObject(tool, t); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Failed to process tool.", InvalidRequestErrorType).Error()))
			return
		}
		tools = append(tools, *t)
	}

	//nolint:govet
	publicAssistant := extendedapi.Assistant{
		openai.AssistantObject{
			0,
			modifyAssistantRequest.Description,
			z.Dereference(modifyAssistantRequest.FileIds),
			"",
			modifyAssistantRequest.Instructions,
			modifyAssistantRequest.Metadata,
			model,
			modifyAssistantRequest.Name,
			openai.AssistantObjectObjectAssistant,
			tools,
		},
		modifyAssistantRequest.GPTScriptTools,
	}

	if extendedapi.IsExtendedAPIKey(r.Context()) {
		modifyAndRespond(s.db.WithContext(r.Context()), w, &db.Assistant{Metadata: db.Metadata{Base: db.Base{ID: assistantID}}}, publicAssistant)
		return
	}
	modifyAndRespondOpenAI(s.db.WithContext(r.Context()), w, &db.Assistant{Metadata: db.Metadata{Base: db.Base{ID: assistantID}}}, publicAssistant)
}

func (s *Server) ListAssistantFiles(w http.ResponseWriter, r *http.Request, assistantID string, params openai.ListAssistantFilesParams) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("assistant_id").Error()))
		return
	}

	gormDB, limit, err := processAssistantsAPIListParams[*db.AssistantFile](
		s.db.WithContext(r.Context()), params.Limit, params.Before, params.After, params.Order,
		&db.Assistant{Metadata: db.Metadata{Base: db.Base{ID: assistantID}}},
	)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	}

	listAndRespond[*db.AssistantFile](gormDB.Where("assistant_id = ?", assistantID), w, limit)
}

func (s *Server) CreateAssistantFile(w http.ResponseWriter, r *http.Request, assistantID string) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("assistant_id").Error()))
		return
	}

	createAssistantFileRequest := new(openai.CreateAssistantFileRequest)
	if err := readObjectFromRequest(r, createAssistantFileRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	//nolint:govet
	createAndRespond(s.db.WithContext(r.Context()), w, new(db.AssistantFile), &openai.AssistantFileObject{
		assistantID,
		0,
		"",
		openai.AssistantFile,
	})
}

func (s *Server) DeleteAssistantFile(w http.ResponseWriter, r *http.Request, assistantID string, fileID string) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("assistant_id").Error()))
		return
	}

	//nolint:govet
	deleteAndRespond[*db.AssistantFile](s.db.WithContext(r.Context()).Where("assistant_id = ?", assistantID), w, fileID, openai.DeleteAssistantFileResponse{
		true,
		fileID,
		openai.AssistantFileDeleted,
	})
}

func (s *Server) GetAssistantFile(w http.ResponseWriter, r *http.Request, assistantID string, fileID string) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("assistant_id").Error()))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("assistant_id = ?", assistantID), w, new(db.AssistantFile), fileID)
}

func (s *Server) CreateSpeech(w http.ResponseWriter, r *http.Request) {
	createSpeechRequest := new(openai.CreateSpeechRequest)
	if err := readObjectFromRequest(r, createSpeechRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	var responseFormat *string
	if createSpeechRequest.ResponseFormat != nil {
		responseFormat = (*string)(createSpeechRequest.ResponseFormat)
	}

	//nolint:govet
	newSpeech := &db.Speech{
		createSpeechRequest.Input,
		datatypes.NewJSONType(createSpeechRequest.Model),
		responseFormat,
		createSpeechRequest.Speed,
		string(createSpeechRequest.Voice),
	}

	// FIXME: The correct response here is the audio for the speech.
	if err := db.CreateAny(s.db.WithContext(r.Context()), newSpeech); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create speech.", InternalErrorType).Error()))
		return
	}
}

func (s *Server) CreateTranscription(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateTranslation(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateChatCompletion(w http.ResponseWriter, r *http.Request) {
	createCompletionRequest := new(openai.CreateChatCompletionRequest)
	if err := readObjectFromRequest(r, createCompletionRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	ccr := new(db.ChatCompletionRequest)
	if err := ccr.FromPublic(createCompletionRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	gormDB := s.db.WithContext(r.Context())
	if err := db.Create(gormDB, ccr); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create chat completion request.", InternalErrorType).Error()))
		return
	}

	if !z.Dereference(ccr.Stream) {
		waitForAndWriteResponse(r.Context(), w, gormDB, ccr.ID, new(db.ChatCompletionResponse))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	waitForAndStreamResponse[*db.ChatCompletionResponseChunk](r.Context(), w, gormDB, ccr.ID)
}

func (s *Server) CreateCompletion(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateEmbedding(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) ListFiles(w http.ResponseWriter, r *http.Request, params openai.ListFilesParams) {
	gormDB := s.db.WithContext(r.Context())
	if z.Dereference(params.Purpose) != "" {
		gormDB = gormDB.Where("purpose = ?", *params.Purpose)
	}
	listAndRespond[*db.File](gormDB, w, -1)
}

func (s *Server) CreateFile(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("purpose") == "" {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(NewAPIError("No purpose provided.", InvalidRequestErrorType).Error()))
		return
	}
	// Max memory is 512MB
	if err := r.ParseMultipartForm(1 << 29); err != nil {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(NewAPIError("Failed to parse multipart form.", InvalidRequestErrorType).Error()))
		return
	}
	if len(r.MultipartForm.File["file"]) == 0 {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(NewAPIError("No file uploaded.", InvalidRequestErrorType).Error()))
		return
	}
	if len(r.MultipartForm.File["file"]) > 1 {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(NewAPIError("Too many files uploaded.", InvalidRequestErrorType).Error()))
		return
	}

	fh := r.MultipartForm.File["file"][0]
	slog.Debug("Uploading file", "file", fh.Filename)

	file := &db.File{
		Filename: fh.Filename,
		Purpose:  r.FormValue("purpose"),
	}

	uploadedFile, err := fh.Open()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to process file.", InternalErrorType).Error()))
		return
	}

	file.Content = make([]byte, fh.Size)
	if _, err := uploadedFile.Read(file.Content); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(NewAPIError("Failed to read file.", InternalErrorType).Error()))
		return
	}

	if err = db.Create(s.db.WithContext(r.Context()), file); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create file.", InternalErrorType).Error()))
		return
	}

	//nolint:govet
	writeObjectToResponse(w, openai.OpenAIFile{
		len(file.Content),
		file.CreatedAt,
		file.Filename,
		file.ID,
		openai.OpenAIFileObjectFile,
		openai.OpenAIFilePurpose(file.Purpose),
		"",
		nil,
	})
}

func (s *Server) DeleteFile(w http.ResponseWriter, r *http.Request, fileID string) {
	//nolint:govet
	deleteAndRespond[*db.File](s.db.WithContext(r.Context()), w, fileID, openai.DeleteFileResponse{
		true,
		fileID,
		openai.DeleteFileResponseObjectFile,
	})
}

func (s *Server) RetrieveFile(w http.ResponseWriter, r *http.Request, fileID string) {
	getAndRespond(s.db.WithContext(r.Context()), w, new(db.File), fileID)
}

func (s *Server) DownloadFile(w http.ResponseWriter, r *http.Request, fileID string) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) ListPaginatedFineTuningJobs(w http.ResponseWriter, r *http.Request, params openai.ListPaginatedFineTuningJobsParams) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateFineTuningJob(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) RetrieveFineTuningJob(w http.ResponseWriter, r *http.Request, fineTuningJobID string) {
	getAndRespond(s.db.WithContext(r.Context()), w, new(db.FineTuningJob), fineTuningJobID)
}

func (s *Server) CancelFineTuningJob(w http.ResponseWriter, r *http.Request, fineTuningJobID string) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) ListFineTuningEvents(w http.ResponseWriter, r *http.Request, fineTuningJobID string, params openai.ListFineTuningEventsParams) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateImageEdit(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateImage(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateImageVariation(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) ListModels(w http.ResponseWriter, r *http.Request) {
	listAndRespond[*db.Model](s.db.WithContext(r.Context()), w, -1)
}

func (s *Server) DeleteModel(w http.ResponseWriter, r *http.Request, modelID string) {
	//nolint:govet
	deleteAndRespond[*db.Model](s.db.WithContext(r.Context()), w, modelID, openai.DeleteModelResponse{
		true,
		modelID,
		string(openai.ModelObjectModel),
	})
}

func (s *Server) RetrieveModel(w http.ResponseWriter, r *http.Request, modelID string) {
	getAndRespond(s.db.WithContext(r.Context()), w, new(db.Model), modelID)
}

func (s *Server) CreateModeration(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateThread(w http.ResponseWriter, r *http.Request) {
	createThreadRequest := new(openai.CreateThreadRequest)
	if err := readObjectFromRequest(r, createThreadRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err := validateMetadata(createThreadRequest.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	//nolint:govet
	publicThread := &openai.ThreadObject{
		// The first two fields will be set on create.
		0,
		"",
		createThreadRequest.Metadata,
		openai.Thread,
	}

	thread := new(db.Thread)
	if err := create(s.db.WithContext(r.Context()), thread, publicThread); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if createThreadRequest.Messages != nil {
		for _, message := range *createThreadRequest.Messages {
			content, err := db.MessageContentFromString(message.Content)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(NewAPIError("Failed to process message content.", InvalidRequestErrorType).Error()))
				return
			}

			//nolint:govet
			publicMessage := &openai.MessageObject{
				nil,
				[]openai.MessageObject_Content_Item{*content},
				0,
				z.Dereference(message.FileIds),
				"",
				message.Metadata,
				openai.ThreadMessage,
				openai.MessageObjectRole(message.Role),
				nil,
				thread.ID,
			}

			if err = create(s.db.WithContext(r.Context()), new(db.Message), publicMessage); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(err.Error()))
				return
			}
		}
	}

	writeObjectToResponse(w, thread.ToPublic())
}

func (s *Server) CreateThreadAndRun(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) DeleteThread(w http.ResponseWriter, r *http.Request, threadID string) {
	//nolint:govet
	deleteAndRespond[*db.Thread](s.db.WithContext(r.Context()), w, threadID, openai.DeleteThreadResponse{
		true,
		threadID,
		openai.ThreadDeleted,
	})
}

func (s *Server) GetThread(w http.ResponseWriter, r *http.Request, threadID string) {
	getAndRespond(s.db.WithContext(r.Context()), w, new(db.Thread), threadID)
}

func (s *Server) ModifyThread(w http.ResponseWriter, r *http.Request, threadID string) {
	reqBody := new(openai.ModifyThreadRequest)
	if err := readObjectFromRequest(r, reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err := validateMetadata(reqBody.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	modifyAndRespond(s.db.WithContext(r.Context()), w, &db.Thread{Metadata: db.Metadata{Base: db.Base{ID: threadID}}}, map[string]interface{}{"metadata": reqBody.Metadata})
}

func (s *Server) ListMessages(w http.ResponseWriter, r *http.Request, threadID string, params openai.ListMessagesParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	gormDB, limit, err := processAssistantsAPIListParams[*db.Message](
		s.db.WithContext(r.Context()), params.Limit, params.Before, params.After, params.Order,
		&db.Thread{Metadata: db.Metadata{Base: db.Base{ID: threadID}}},
	)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	listAndRespond[*db.Message](gormDB.Where("thread_id = ?", threadID), w, limit)
}

func (s *Server) CreateMessage(w http.ResponseWriter, r *http.Request, threadID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	createMessageRequest := new(openai.CreateMessageRequest)
	if err := readObjectFromRequest(r, createMessageRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	content, err := db.MessageContentFromString(createMessageRequest.Content)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process message content.", InvalidRequestErrorType).Error()))
		return
	}

	//nolint:govet
	publicMessage := &openai.MessageObject{
		nil,
		[]openai.MessageObject_Content_Item{*content},
		0,
		z.Dereference(createMessageRequest.FileIds),
		"",
		createMessageRequest.Metadata,
		openai.ThreadMessage,
		openai.MessageObjectRole(createMessageRequest.Role),
		nil,
		threadID,
	}

	createAndRespond(s.db.WithContext(r.Context()), w, new(db.Message), publicMessage)
}

func (s *Server) GetMessage(w http.ResponseWriter, r *http.Request, threadID string, messageID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("thread_id = ?", threadID), w, new(db.Message), messageID)
}

func (s *Server) ModifyMessage(w http.ResponseWriter, r *http.Request, threadID string, messageID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	reqBody := new(openai.ModifyMessageRequest)
	if err := readObjectFromRequest(r, reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err := validateMetadata(reqBody.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	modifyAndRespond(s.db.WithContext(r.Context()), w, &db.Message{Metadata: db.Metadata{Base: db.Base{ID: messageID}}}, map[string]interface{}{"metadata": reqBody.Metadata})
}

func (s *Server) ListMessageFiles(w http.ResponseWriter, r *http.Request, threadID string, messageID string, params openai.ListMessageFilesParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}
	if messageID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("message_id").Error()))
		return
	}

	gormDB, limit, err := processAssistantsAPIListParams[*db.MessageFile](
		s.db.WithContext(r.Context()), params.Limit, params.Before, params.After, params.Order,
		&db.Thread{Metadata: db.Metadata{Base: db.Base{ID: threadID}}},
		&db.Message{Metadata: db.Metadata{Base: db.Base{ID: messageID}}},
	)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	listAndRespond[*db.MessageFile](gormDB.Where("thread_id = ? AND message_id = ?", threadID, messageID), w, limit)
}

func (s *Server) GetMessageFile(w http.ResponseWriter, r *http.Request, threadID string, messageID string, fileID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}
	if messageID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("message_id").Error()))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("thread_id = ? AND message_id = ?", threadID, messageID), w, new(db.MessageFile), fileID)
}

func (s *Server) ListRuns(w http.ResponseWriter, r *http.Request, threadID string, params openai.ListRunsParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	gormDB, limit, err := processAssistantsAPIListParams[*db.Run](
		s.db.WithContext(r.Context()), params.Limit, params.Before, params.After, params.Order,
		&db.Thread{Metadata: db.Metadata{Base: db.Base{ID: threadID}}},
	)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	listAndRespond[*db.Run](gormDB.Where("thread_id = ?", threadID), w, limit)
}

func (s *Server) CreateRun(w http.ResponseWriter, r *http.Request, threadID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	createRunRequest := new(openai.CreateRunRequest)
	if err := readObjectFromRequest(r, createRunRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	gormDB := s.db.WithContext(r.Context())
	// If the thread is locked by another run, then return an error.
	thread := &db.Thread{
		Metadata: db.Metadata{
			Base: db.Base{
				ID: threadID,
			},
		},
	}
	if err := gormDB.Where("id = ?", threadID).First(thread).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewNotFoundError(thread).Error()))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to get thread.", InternalErrorType).Error()))
		return
	}

	if thread.LockedByRunID != "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Thread is locked by run %s.", thread.LockedByRunID), InvalidRequestErrorType).Error()))
		return
	}

	if err := validateMetadata(createRunRequest.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	var tools []openai.RunObject_Tools_Item
	if createRunRequest.Tools != nil {
		tools = make([]openai.RunObject_Tools_Item, 0, len(*createRunRequest.Tools))
		for _, tool := range *createRunRequest.Tools {
			t := new(openai.RunObject_Tools_Item)
			if err := transposeObject(tool, t); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(NewAPIError("Failed to process tool.", InvalidRequestErrorType).Error()))
				return
			}
			tools = append(tools, *t)
		}
	}

	//nolint:govet
	publicRun := &openai.RunObject{
		createRunRequest.AssistantId,
		nil,
		nil,
		0,
		0,
		nil,
		nil,
		"",
		z.Dereference(createRunRequest.Instructions),
		nil,
		createRunRequest.Metadata,
		z.Dereference(createRunRequest.Model),
		openai.ThreadRun,
		nil,
		nil,
		openai.RunObjectStatusQueued,
		threadID,
		tools,
		nil,
	}

	run := new(db.Run)
	if err := run.FromPublic(publicRun); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	if err := gormDB.Transaction(func(tx *gorm.DB) error {
		if err := db.Create(tx, run); err != nil {
			return err
		}

		return tx.Model(thread).Update("locked_by_run_id", run.ID).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Run %s already exists.", run.ID), InvalidRequestErrorType).Error()))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create run.", InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, run.ToPublic())
}

func (s *Server) GetRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("thread_id = ?", threadID), w, new(db.Run), runID)
}

func (s *Server) ModifyRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	reqBody := new(openai.ModifyRunRequest)
	if err := readObjectFromRequest(r, reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err := validateMetadata(reqBody.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	modifyAndRespond(s.db.WithContext(r.Context()), w, &db.Run{Metadata: db.Metadata{Base: db.Base{ID: runID}}}, map[string]interface{}{"metadata": reqBody.Metadata})
}

func (s *Server) CancelRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("run_id").Error()))
		return
	}

	publicRun, err := db.CancelRun(s.db.WithContext(r.Context()).Where("thread_id = ?", threadID), runID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(NewNotFoundError(&db.Run{Metadata: db.Metadata{Base: db.Base{ID: runID}}}).Error()))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to cancel run: %v", err), InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, publicRun)
}

func (s *Server) ListRunSteps(w http.ResponseWriter, r *http.Request, threadID string, runID string, params openai.ListRunStepsParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}
	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("run_id").Error()))
		return
	}

	gormDB, limit, err := processAssistantsAPIListParams[*db.RunStep](
		s.db.WithContext(r.Context()).Where("run_id = ?", runID), params.Limit, params.Before, params.After, params.Order,
		&db.Thread{Metadata: db.Metadata{Base: db.Base{ID: threadID}}},
		&db.Run{Metadata: db.Metadata{Base: db.Base{ID: runID}}},
	)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	listAndRespond[*db.RunStep](gormDB, w, limit)
}

func (s *Server) GetRunStep(w http.ResponseWriter, r *http.Request, threadID string, runID string, stepID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}
	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("run_id").Error()))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("thread_id = ?", threadID).Where("run_id = ?", runID), w, new(db.RunStep), stepID)
}

func (s *Server) SubmitToolOuputsToRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}
	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("run_id").Error()))
		return
	}

	outputs := new(openai.SubmitToolOutputsRunRequest)
	if err := readObjectFromRequest(r, outputs); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	// Get the latest run step.
	var runSteps []*db.RunStep
	if err := db.List(s.db.WithContext(r.Context()).Where("run_id = ?", runID).Where("status = ?", string(openai.InProgress)).Order("created_at desc").Limit(1), &runSteps); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to get run step.", InternalErrorType).Error()))
		return
	}
	if len(runSteps) == 0 {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(NewAPIError("Run step not found.", InvalidRequestErrorType).Error()))
		return
	}

	runStep := runSteps[0]

	runStepFunctionCalls, err := runStep.GetRunStepFunctionCalls()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to get run step function calls.", InternalErrorType).Error()))
		return
	}
	if runStep.Status != string(openai.InProgress) || len(runStepFunctionCalls) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Run step not in progress.", InvalidRequestErrorType).Error()))
		return
	}
	if len(runStepFunctionCalls) != len(outputs.ToolOutputs) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Mismatched number of tool calls and tool outputs: expected %d, got %d", len(runStepFunctionCalls), len(outputs.ToolOutputs)), InvalidRequestErrorType).Error()))
		return
	}

	// All expected tool calls must have been submitted.
	for _, output := range outputs.ToolOutputs {
		toolCallID := z.Dereference(output.ToolCallId)
		idx := slices.IndexFunc(runStepFunctionCalls, func(toolCall openai.RunStepDetailsToolCallsFunctionObject) bool {
			return toolCall.Id == toolCallID
		})
		if idx == -1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Tool call %s not found in run step.", toolCallID), InvalidRequestErrorType).Error()))
			return
		}

		runStepFunctionCalls[idx].Function.Output = new(string)
		*runStepFunctionCalls[idx].Function.Output = *output.Output
	}

	stepDetailsHack := map[string]any{
		"tool_calls": runStepFunctionCalls,
		"type":       openai.ToolCalls,
	}
	if err := s.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(runStep).Where("id = ?", runStep.ID).Updates(map[string]interface{}{"status": string(openai.RunObjectStatusCompleted), "step_details": datatypes.NewJSONType(stepDetailsHack)}).Error; err != nil {
			return err
		}

		return tx.Model(new(db.Run)).Where("id = ?", runID).Updates(map[string]interface{}{"status": string(openai.RunObjectStatusQueued), "required_action": nil}).Error
	}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to submit tool outputs: %v", err), InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, runStep.ToPublic())
}

func readObjectFromRequest(r *http.Request, obj any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return NewAPIError("Failed reading request body.", InvalidRequestErrorType)
	}
	if err := json.Unmarshal(body, obj); err != nil {
		return NewAPIError(fmt.Sprintf("Failed parsing request object: %v", err), InvalidRequestErrorType)
	}

	return nil
}

func writeObjectToResponse(w http.ResponseWriter, obj any) {
	body, err := json.Marshal(obj)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to write object to response.", InternalErrorType).Error()))
		return
	}
	_, _ = w.Write(body)
}

func get(gormDB *gorm.DB, obj Transformer, id string) error {
	if id == "" {
		return NewMustNotBeEmptyError("id")
	}

	return db.Get(gormDB, obj, id)
}

func getAndRespond(gormDB *gorm.DB, w http.ResponseWriter, obj Transformer, id string) {
	if err := get(gormDB, obj, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("No %s found with id '%s'.", strings.ToLower(strings.Split(fmt.Sprintf("%T", obj), ".")[1]), id), InvalidRequestErrorType).Error()))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to get %s: %v", strings.ToLower(strings.Split(fmt.Sprintf("%T", obj), ".")[1]), err), InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, obj.ToPublic())
}

func getAndRespondOpenAI(gormDB *gorm.DB, w http.ResponseWriter, obj ExtendedTransformer, id string) {
	if err := get(gormDB, obj, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("No %s found with id '%s'.", strings.ToLower(strings.Split(fmt.Sprintf("%T", obj), ".")[1]), id), InvalidRequestErrorType).Error()))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to get %s: %v", strings.ToLower(strings.Split(fmt.Sprintf("%T", obj), ".")[1]), err), InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, obj.ToPublicOpenAI())
}

func create(gormDB *gorm.DB, obj Transformer, publicObj any) error {
	if err := obj.FromPublic(publicObj); err != nil {
		return NewAPIError("Failed parsing request object.", InvalidRequestErrorType)
	}

	if err := db.Create(gormDB, obj); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return NewAPIError("Object already exists.", InvalidRequestErrorType)
		}
		return NewAPIError("Failed to create object.", InternalErrorType)
	}

	return nil
}

func createAndRespond(gormDB *gorm.DB, w http.ResponseWriter, obj Transformer, publicObj any) {
	if err := create(gormDB, obj, publicObj); err != nil {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	writeObjectToResponse(w, obj.ToPublic())
}

func createAndRespondOpenAI(gormDB *gorm.DB, w http.ResponseWriter, obj ExtendedTransformer, publicObj any) {
	if err := create(gormDB, obj, publicObj); err != nil {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	writeObjectToResponse(w, obj.ToPublicOpenAI())
}

func processAssistantsAPIListParams[T Transformer, O ~string](gormDB *gorm.DB, limit *int, before, after *string, order *O, ensureExists ...db.Storer) (*gorm.DB, int, error) {
	for _, e := range ensureExists {
		if err := gormDB.First(e).Error; err != nil {
			return nil, 0, NewNotFoundError(e)
		}
	}

	// Limit should be 1 more than the number desired so that we can tell if there are more results.
	if z.Dereference(limit) == 0 {
		limit = z.Pointer(21)
	} else if *limit < 1 || *limit > 100 {
		return nil, 0, NewAPIError("limit must be between 1 and 100.", InvalidRequestErrorType)
	} else {
		*limit++
	}

	gormDB = gormDB.Limit(*limit)

	// TODO(thedadams): what happens if before/after are not valid object IDs?
	// TODO(thedadams): what happens if before and after are set?
	// TODO(thedadams): what happens if before/after are in the wrong order?

	if b := z.Dereference(before); b != "" {
		beforeObj := *new(T)
		if err := db.Get(gormDB, beforeObj, b); err != nil {
			return nil, 0, NewNotFoundError(beforeObj)
		}

		gormDB = gormDB.Where("created_at < ?", beforeObj.GetCreatedAt())
	}
	if a := z.Dereference(after); a != "" {
		afterObj := *new(T)
		if err := db.Get(gormDB, afterObj, a); err != nil {
			return nil, 0, NewNotFoundError(afterObj)
		}

		gormDB = gormDB.Where("created_at > ?", afterObj.GetCreatedAt())
	}

	ordering := string(z.Dereference(order))
	if ordering == "" {
		ordering = "desc"
	} else if *order != "asc" && *order != "desc" {
		return nil, 0, NewAPIError("Order must be 'asc' or 'desc'.", InvalidRequestErrorType)
	}

	gormDB = gormDB.Order(fmt.Sprintf("created_at %s", ordering))

	return gormDB, *limit, nil
}

func list[T Transformer](gormDB *gorm.DB, objs *[]T) error {
	return db.List(gormDB, objs)
}

func listAndRespond[T Transformer](gormDB *gorm.DB, w http.ResponseWriter, limit int) {
	var objs []T
	if err := list(gormDB, &objs); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to list objects.", InternalErrorType).Error()))
		return
	}

	publicObjs := make([]any, 0, len(objs))
	for _, o := range objs {
		publicObjs = append(publicObjs, o.ToPublic())
	}

	var firstID, lastID string
	if len(objs) > 0 {
		firstID = objs[0].GetID()
		lastID = objs[len(objs)-1].GetID()
	}

	respondWithList(w, publicObjs, limit, firstID, lastID)
}

func listAndRespondOpenAI[T ExtendedTransformer](gormDB *gorm.DB, w http.ResponseWriter, limit int) {
	var objs []T
	if err := list(gormDB, &objs); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to list objects.", InternalErrorType).Error()))
		return
	}

	publicObjs := make([]any, 0, len(objs))
	for _, o := range objs {
		publicObjs = append(publicObjs, o.ToPublicOpenAI())
	}

	respondWithList(w, publicObjs, limit, objs[0].GetID(), objs[len(objs)-1].GetID())
}

func respondWithList(w http.ResponseWriter, publicObjs []any, limit int, firstID, lastID string) {
	result := map[string]any{"object": "list", "data": publicObjs}

	if limit != -1 {
		hasMore := len(publicObjs) >= limit
		if hasMore {
			publicObjs = publicObjs[:limit-1]
		}
		result["has_more"] = hasMore
		result["first_id"] = firstID
		result["last_id"] = lastID
	}

	writeObjectToResponse(w, result)
}

func modify(gormDB *gorm.DB, obj db.Storer, updates any) error {
	return db.Modify(gormDB, obj, obj.GetID(), updates)
}

func modifyAndRespond(gormDB *gorm.DB, w http.ResponseWriter, obj Transformer, updates any) {
	if err := modify(gormDB, obj, updates); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(NewNotFoundError(obj).Error()))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to modify object: %v", err), InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, obj.ToPublic())
}

func modifyAndRespondOpenAI(gormDB *gorm.DB, w http.ResponseWriter, obj ExtendedTransformer, updates any) {
	if err := modify(gormDB, obj, updates); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(NewNotFoundError(obj).Error()))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to modify object: %v", err), InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, obj.ToPublicOpenAI())
}

func deleteAndRespond[T Transformer](gormDB *gorm.DB, w http.ResponseWriter, id string, resp any) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("id").Error()))
		return
	}

	if err := db.Delete[T](gormDB, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			obj := *new(T)
			obj.SetID(id)
			_, _ = w.Write([]byte(NewNotFoundError(obj).Error()))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to delete object: %v", err), InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, resp)
}

func waitForResponse(ctx context.Context, gormDB *gorm.DB, id string, obj JobRunner) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			err := gormDB.Model(obj).Where("request_id = ?", id).First(obj).Error
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			time.Sleep(time.Second)
		}
	}
}

func waitForAndWriteResponse(ctx context.Context, w http.ResponseWriter, gormDB *gorm.DB, id string, respObj JobResponder) {
	if err := waitForResponse(ctx, gormDB, id, respObj); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to get response: %v", err), InternalErrorType).Error()))
		return
	}

	if errStr := respObj.GetErrorString(); errStr != "" {
		code := respObj.GetStatusCode()
		errorType := InternalErrorType
		if code < 500 {
			errorType = InvalidRequestErrorType
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(NewAPIError(errStr, errorType).Error()))
	} else {
		writeObjectToResponse(w, respObj.ToPublic())
	}
}

func waitForAndStreamResponse[T JobRespondStreamer](ctx context.Context, w http.ResponseWriter, gormDB *gorm.DB, id string) {
	index := -1
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		respObj := *new(T)
		if err := gormDB.Model(respObj).Where("request_id = ?", id).Where("response_idx > ?", index).Order("response_idx asc").First(&respObj).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			time.Sleep(time.Second)
			continue
		} else if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed streaming responses: %v", err), InternalErrorType).Error()))
			break
		} else if errStr := respObj.GetErrorString(); errStr != "" {
			_, _ = w.Write([]byte(fmt.Sprintf(`data: %v`, NewAPIError(errStr, InternalErrorType).Error())))
			break
		} else {
			index = respObj.GetIndex()
			if respObj.IsDone() {
				break
			}

			respObj.SetID(id)
			body, err := json.Marshal(respObj.ToPublic())
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(fmt.Sprintf(`data: %v`, NewAPIError(fmt.Sprintf("Failed to process streamed response: %v", err), InternalErrorType).Error())))
				break
			}

			d := make([]byte, 0, len(body)+8)
			_, _ = w.Write(append(append(append(d, []byte("data: ")...), body...), byte('\n')))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}

	_, _ = w.Write([]byte("data: [DONE]\n"))
}

// transposeObject will marshal the first object and unmarshal it into the second object.
func transposeObject(first json.Marshaler, second json.Unmarshaler) error {
	firstBytes, err := first.MarshalJSON()
	if err != nil {
		return err
	}

	return second.UnmarshalJSON(firstBytes)
}
