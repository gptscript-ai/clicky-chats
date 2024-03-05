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
	"time"

	"github.com/acorn-io/z"
	"github.com/thedadams/clicky-chats/pkg/db"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	notEmptyErrorFormat = "value %s should not be empty on %s"
	notFoundError       = "not found"
)

func (s *Server) ListAssistants(w http.ResponseWriter, r *http.Request, params openai.ListAssistantsParams) {
	gormDB, err := processAssistantsAPIListParams[*db.Assistant](s.db.WithContext(r.Context()), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
	}

	listAndRespond[*db.Assistant](gormDB, w)
}

func (s *Server) CreateAssistant(w http.ResponseWriter, r *http.Request) {
	createAssistantRequest := new(openai.CreateAssistantRequest)
	if err := readObjectFromRequest(r, createAssistantRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	model, err := createAssistantRequest.Model.AsCreateAssistantRequestModel0()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "failed to process model: %v"}`, err)))
		return
	}

	tools := make([]openai.AssistantObject_Tools_Item, 0, len(*createAssistantRequest.Tools))
	for _, tool := range *createAssistantRequest.Tools {
		t := new(openai.AssistantObject_Tools_Item)
		if err := transposeObject(tool, t); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
			return
		}
		tools = append(tools, *t)
	}

	//nolint:govet
	publicAssistant := &openai.AssistantObject{
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
	}

	createAndRespond(s.db.WithContext(r.Context()), w, new(db.Assistant), publicAssistant)
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
	getAndRespond(s.db.WithContext(r.Context()), w, new(db.Assistant), assistantID)
}

func (s *Server) ModifyAssistant(w http.ResponseWriter, r *http.Request, assistantID string) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "assistant_id", "modify"))))
		return
	}

	modifyAssistantRequest := new(openai.ModifyAssistantRequest)
	if err := readObjectFromRequest(r, modifyAssistantRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if err := validateMetadata(modifyAssistantRequest.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	model, err := modifyAssistantRequest.Model.AsModifyAssistantRequestModel0()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "failed to process model: %v"}`, err)))
		return
	}

	tools := make([]openai.AssistantObject_Tools_Item, 0, len(*modifyAssistantRequest.Tools))
	for _, tool := range *modifyAssistantRequest.Tools {
		t := new(openai.AssistantObject_Tools_Item)
		if err := transposeObject(tool, t); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
			return
		}
		tools = append(tools, *t)
	}

	//nolint:govet
	publicAssistant := &openai.AssistantObject{
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
	}

	modify(s.db.WithContext(r.Context()), w, new(db.Assistant), assistantID, publicAssistant)
}

func (s *Server) ListAssistantFiles(w http.ResponseWriter, r *http.Request, assistantID string, params openai.ListAssistantFilesParams) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "assistant_id", "list"))))
		return
	}

	gormDB, err := processAssistantsAPIListParams[*db.AssistantFile](s.db.WithContext(r.Context()).Where("assistant_id = ?", assistantID), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
	}

	listAndRespond[*db.AssistantFile](gormDB, w)
}

func (s *Server) CreateAssistantFile(w http.ResponseWriter, r *http.Request, assistantID string) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "assistant_id", "create"))))
		return
	}

	createAssistantFileRequest := new(openai.CreateAssistantFileRequest)
	if err := readObjectFromRequest(r, createAssistantFileRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "assistant_id", "delete"))))
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "assistant_id", "get"))))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("assistant_id = ?", assistantID), w, new(db.AssistantFile), fileID)
}

func (s *Server) CreateSpeech(w http.ResponseWriter, r *http.Request) {
	createSpeechRequest := new(openai.CreateSpeechRequest)
	if err := readObjectFromRequest(r, createSpeechRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	ccr := new(db.ChatCompletionRequest)
	if err := ccr.FromPublic(createCompletionRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	gormDB := s.db.WithContext(r.Context())
	if err := db.Create(gormDB, ccr); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
	listAndRespond[*db.File](gormDB, w)
}

func (s *Server) CreateFile(w http.ResponseWriter, r *http.Request) {
	// Max memory is 512MB
	if err := r.ParseMultipartForm(1 << 29); err != nil {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	} else if len(r.MultipartForm.File["file"]) == 0 {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(`{"error": "no files"}`))
		return
	} else if len(r.MultipartForm.File["file"]) > 1 {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(`{"error": "too many files"}`))
		return
	} else if r.FormValue("purpose") == "" {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(`{"error": "no purpose provided"}`))
		return
	}

	fh := r.MultipartForm.File["file"][0]
	slog.Debug("Uploading file", "file", fh.Filename)

	file := &db.File{
		Filename: fh.Filename,
		Purpose:  r.FormValue("purpose"),
	}
	file.SetID(db.NewID())
	file.SetCreatedAt(int(time.Now().Unix()))

	uploadedFile, err := fh.Open()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	file.Content = make([]byte, fh.Size)
	if _, err := uploadedFile.Read(file.Content); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if err = db.CreateAny(s.db.WithContext(r.Context()), file); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
	listAndRespond[*db.Model](s.db.WithContext(r.Context()), w)
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if err := validateMetadata(createThreadRequest.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if createThreadRequest.Messages != nil {
		for _, message := range *createThreadRequest.Messages {
			content, err := db.MessageContentFromString(message.Content)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "error processing content: %v"}`, err)))
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

			if err := create(s.db.WithContext(r.Context()), new(db.Message), publicMessage); err != nil {
				if errors.Is(err, gorm.ErrDuplicatedKey) {
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if err := validateMetadata(reqBody.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	modify(s.db.WithContext(r.Context()), w, new(db.Thread), threadID, map[string]interface{}{"metadata": reqBody.Metadata})
}

func (s *Server) ListMessages(w http.ResponseWriter, r *http.Request, threadID string, params openai.ListMessagesParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "list"))))
		return
	}

	gormDB, err := processAssistantsAPIListParams[*db.Message](s.db.WithContext(r.Context()).Where("thread_id = ?", threadID), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	listAndRespond[*db.Message](gormDB, w)
}

func (s *Server) CreateMessage(w http.ResponseWriter, r *http.Request, threadID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "create"))))
		return
	}

	createMessageRequest := new(openai.CreateMessageRequest)
	if err := readObjectFromRequest(r, createMessageRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	content, err := db.MessageContentFromString(createMessageRequest.Content)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "error processing content: %v"}`, err)))
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "get"))))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("thread_id = ?", threadID), w, new(db.Message), messageID)
}

func (s *Server) ModifyMessage(w http.ResponseWriter, r *http.Request, threadID string, messageID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "update"))))
		return
	}

	reqBody := new(openai.ModifyMessageRequest)
	if err := readObjectFromRequest(r, reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if err := validateMetadata(reqBody.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	modify(s.db.WithContext(r.Context()), w, new(db.Message), messageID, map[string]interface{}{"metadata": reqBody.Metadata})
}

func (s *Server) ListMessageFiles(w http.ResponseWriter, r *http.Request, threadID string, messageID string, params openai.ListMessageFilesParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "list"))))
		return
	}
	if messageID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "message_id", "list"))))
		return
	}

	gormDB, err := processAssistantsAPIListParams[*db.MessageFile](s.db.WithContext(r.Context()).Where("thread_id = ? AND message_id = ?", threadID, messageID), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	listAndRespond[*db.MessageFile](gormDB, w)
}

func (s *Server) GetMessageFile(w http.ResponseWriter, r *http.Request, threadID string, messageID string, fileID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "get"))))
		return
	}
	if messageID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "message_id", "get"))))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("thread_id = ? AND message_id = ?", threadID, messageID), w, new(db.MessageFile), fileID)
}

func (s *Server) ListRuns(w http.ResponseWriter, r *http.Request, threadID string, params openai.ListRunsParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "list"))))
		return
	}

	gormDB, err := processAssistantsAPIListParams[*db.Run](s.db.WithContext(r.Context()).Where("thread_id = ?", threadID), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	listAndRespond[*db.Run](gormDB, w)
}

func (s *Server) CreateRun(w http.ResponseWriter, r *http.Request, threadID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "create"))))
		return
	}

	createRunRequest := new(openai.CreateRunRequest)
	if err := readObjectFromRequest(r, createRunRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	gormDB := s.db.WithContext(r.Context())
	// If the thread is locked by another run, then return an error.
	thread := new(db.Thread)
	if err := gormDB.Where("id = ?", threadID).First(thread).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf("thread with id %s not found", threadID))))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if thread.LockedByRunID != "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf("run with id %s is already running for thread %s", thread.LockedByRunID, threadID))))
		return
	}

	if err := validateMetadata(createRunRequest.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	var tools []openai.RunObject_Tools_Item
	if createRunRequest.Tools != nil {
		tools = make([]openai.RunObject_Tools_Item, 0, len(*createRunRequest.Tools))
		for _, tool := range *createRunRequest.Tools {
			t := new(openai.RunObject_Tools_Item)
			if err := transposeObject(tool, t); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
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
			_, _ = w.Write([]byte(`{"error": "already exists"}`))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, run.ToPublic())
}

func (s *Server) GetRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "get"))))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("thread_id = ?", threadID), w, new(db.Run), runID)
}

func (s *Server) ModifyRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "update"))))
		return
	}

	reqBody := new(openai.ModifyRunRequest)
	if err := readObjectFromRequest(r, reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if err := validateMetadata(reqBody.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	modify(s.db.WithContext(r.Context()), w, new(db.Run), runID, map[string]interface{}{"metadata": reqBody.Metadata})
}

func (s *Server) CancelRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "cancel"))))
		return
	}

	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "id", "cancel"))))
		return
	}

	publicRun, err := db.CancelRun(s.db.WithContext(r.Context()).Where("thread_id = ?", threadID), runID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, notFoundError)))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, publicRun)
}

func (s *Server) ListRunSteps(w http.ResponseWriter, r *http.Request, threadID string, runID string, params openai.ListRunStepsParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "list"))))
		return
	}
	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "run_id", "list"))))
		return
	}

	gormDB, err := processAssistantsAPIListParams[*db.RunStep](s.db.WithContext(r.Context()).Where("run_id = ?", runID), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	listAndRespond[*db.RunStep](gormDB, w)
}

func (s *Server) GetRunStep(w http.ResponseWriter, r *http.Request, threadID string, runID string, stepID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "get"))))
		return
	}
	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "run_id", "get"))))
		return
	}

	getAndRespond(s.db.WithContext(r.Context()).Where("thread_id = ?", threadID).Where("run_id = ?", runID), w, new(db.RunStep), stepID)
}

func (s *Server) SubmitToolOuputsToRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "submit"))))
		return
	}
	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "run_id", "submit"))))
		return
	}

	outputs := new(openai.SubmitToolOutputsRunRequest)
	if err := readObjectFromRequest(r, outputs); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	// Get the latest run step.
	var runSteps []*db.RunStep
	if err := db.List(s.db.WithContext(r.Context()).Where("run_id = ?", runID).Where("status = ?", string(openai.InProgress)).Order("created_at desc").Limit(1), &runSteps); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}
	if len(runSteps) == 0 {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "run step not found"}`))
		return
	}

	runStep := runSteps[0]

	runStepFunctionCalls, err := runStep.GetRunStepFunctionCalls()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "failed getting run step tool calls: %v"}`, err)))
		return
	}
	if runStep.Status != string(openai.InProgress) || len(runStepFunctionCalls) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, "run is not waiting on a tool output")))
		return
	}
	if len(runStepFunctionCalls) != len(outputs.ToolOutputs) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, fmt.Sprintf("missing tool calls, expecting %d, got %d", len(runStepFunctionCalls), len(outputs.ToolOutputs)))))
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
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, fmt.Sprintf("unexpected tool call with id %s", toolCallID))))
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, runStep.ToPublic())
}

func readObjectFromRequest(r *http.Request, obj any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, obj)
}

func writeObjectToResponse(w http.ResponseWriter, obj any) {
	body, err := json.Marshal(obj)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}
	_, err = w.Write(body)
	if err != nil {
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
	}
}

func getAndRespond(gormDB *gorm.DB, w http.ResponseWriter, obj db.Transformer, id string) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "id", "get"))))
		return
	}

	if err := db.Get(gormDB, obj, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, notFoundError)))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, obj.ToPublic())
}

func create(gormDB *gorm.DB, obj db.Transformer, publicObj any) error {
	if err := obj.FromPublic(publicObj); err != nil {
		return err
	}

	if err := db.Create(gormDB, obj); err != nil {
		return fmt.Errorf("error creating: %w", err)
	}

	return nil
}

func createAndRespond(gormDB *gorm.DB, w http.ResponseWriter, obj db.Transformer, publicObj any) {
	if err := create(gormDB, obj, publicObj); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, obj.ToPublic())
}

func processAssistantsAPIListParams[T db.Transformer, O ~string](gormDB *gorm.DB, limit *int, before, after *string, order *O) (*gorm.DB, error) {
	if limit == nil || *limit == 0 {
		limit = z.Pointer(20)
	} else if *limit < 1 || *limit > 100 {
		return nil, fmt.Errorf("limit should be between 1 and 100")
	}

	gormDB = gormDB.Limit(*limit)

	// TODO(thedadams): what happens if before/after are not valid object IDs?
	// TODO(thedadams): what happens if before and after are set?
	// TODO(thedadams): what happens if before/after are in the wrong order?

	if b := z.Dereference(before); b != "" {
		beforeObj := *new(T)
		if err := db.Get(gormDB, beforeObj, b); err != nil {
			return nil, fmt.Errorf("cannot find before object: %v", err)
		}

		gormDB = gormDB.Where("created_at < ?", beforeObj.GetCreatedAt())
	}
	if a := z.Dereference(after); a != "" {
		afterObj := *new(T)
		if err := db.Get(gormDB, afterObj, a); err != nil {
			return nil, fmt.Errorf("cannot find after object: %v", err)
		}

		gormDB = gormDB.Where("created_at > ?", afterObj.GetCreatedAt())
	}

	ordering := string(z.Dereference(order))
	if ordering == "" {
		ordering = "desc"
	} else if *order != "asc" && *order != "desc" {
		return nil, fmt.Errorf("order should be asc or desc")
	}

	gormDB = gormDB.Order(fmt.Sprintf("created_at %s", ordering))

	return gormDB, nil
}

func listAndRespond[T db.Transformer](gormDB *gorm.DB, w http.ResponseWriter) {
	var objs []T
	if err := db.List(gormDB, &objs); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	publicObjs := make([]any, 0, len(objs))
	for _, o := range objs {
		publicObjs = append(publicObjs, o.ToPublic())
	}

	writeObjectToResponse(w, map[string]any{"object": "list", "data": publicObjs})
}

func modify(gormDB *gorm.DB, w http.ResponseWriter, obj db.Transformer, id string, updates any) {
	if err := db.Modify(gormDB, obj, id, updates); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, notFoundError)))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, obj.ToPublic())
}

func deleteAndRespond[T db.Transformer](gormDB *gorm.DB, w http.ResponseWriter, id string, resp any) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "id", "delete"))))
		return
	}

	if err := db.Delete[T](gormDB, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, notFoundError)))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, resp)
}

func waitForResponse(ctx context.Context, gormDB *gorm.DB, id string, obj db.JobRunner) error {
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

func waitForAndWriteResponse(ctx context.Context, w http.ResponseWriter, gormDB *gorm.DB, id string, respObj db.JobResponder) {
	if err := waitForResponse(ctx, gormDB, id, respObj); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	if errStr := respObj.GetErrorString(); errStr != "" {
		w.WriteHeader(respObj.GetStatusCode())
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, errStr)))
	} else {
		writeObjectToResponse(w, respObj.ToPublic())
	}
}

func waitForAndStreamResponse[T db.JobRespondStreamer](ctx context.Context, w http.ResponseWriter, gormDB *gorm.DB, id string) {
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
			_, _ = w.Write([]byte(fmt.Sprintf(`data: {"error": "%v"}`, err)))
			break
		} else if errStr := respObj.GetErrorString(); errStr != "" {
			_, _ = w.Write([]byte(fmt.Sprintf(`data: {"error": "%v"}`, errStr)))
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
				_, _ = w.Write([]byte(fmt.Sprintf(`data: {"error": "%v"}`, err)))
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
