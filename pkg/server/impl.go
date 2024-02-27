package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/acorn-io/z"
	"github.com/google/uuid"
	"github.com/thedadams/clicky-chats/pkg/db"
	"github.com/thedadams/clicky-chats/pkg/generated/openai"
	"gorm.io/datatypes"
	gdb "gorm.io/gorm"
)

const (
	notEmptyErrorFormat = "value %s should not be empty on %s"
	notFoundError       = "not found"
)

func (s *Server) ListAssistants(w http.ResponseWriter, _ *http.Request, params openai.ListAssistantsParams) {
	gormDB, err := processAssistantsAPIListParams[*db.Assistant](s.db.DB, params.Limit, params.Before, params.After, params.Order)
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

	createAndRespond[*db.Assistant](s.db.DB, w, publicAssistant)
}

func (s *Server) DeleteAssistant(w http.ResponseWriter, _ *http.Request, assistantID string) {
	//nolint:govet
	deleteAndRespond[*db.Assistant](s.db.DB, w, assistantID, openai.DeleteAssistantResponse{
		true,
		assistantID,
		openai.AssistantDeleted,
	})
}

func (s *Server) GetAssistant(w http.ResponseWriter, _ *http.Request, assistantID string) {
	getAndRespond[*db.Assistant](s.db.DB, w, assistantID)
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

	modify[*db.Assistant](s.db.DB, w, assistantID, publicAssistant)
}

func (s *Server) ListAssistantFiles(w http.ResponseWriter, _ *http.Request, assistantID string, params openai.ListAssistantFilesParams) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "assistant_id", "list"))))
		return
	}

	gormDB, err := processAssistantsAPIListParams[*db.AssistantFile](s.db.DB.Where("assistant_id = ?", assistantID), params.Limit, params.Before, params.After, params.Order)
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
	createAndRespond[*db.AssistantFile](s.db.DB, w, &openai.AssistantFileObject{
		assistantID,
		0,
		"",
		openai.AssistantFile,
	})
}

func (s *Server) DeleteAssistantFile(w http.ResponseWriter, _ *http.Request, assistantID string, fileID string) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "assistant_id", "delete"))))
		return
	}

	//nolint:govet
	deleteAndRespond[*db.AssistantFile](s.db.DB.Where("assistant_id = ?", assistantID), w, fileID, openai.DeleteAssistantFileResponse{
		true,
		fileID,
		openai.AssistantFileDeleted,
	})
}

func (s *Server) GetAssistantFile(w http.ResponseWriter, _ *http.Request, assistantID string, fileID string) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "assistant_id", "get"))))
		return
	}

	getAndRespond[*db.AssistantFile](s.db.DB.Where("assistant_id = ?", assistantID), w, fileID)
}

func (s *Server) CreateSpeech(w http.ResponseWriter, r *http.Request) {
	createSpeechRequest := new(openai.CreateSpeechRequest)
	if err := readObjectFromRequest(r, createSpeechRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	//nolint:govet
	newSpeech := &db.Speech{
		createSpeechRequest.Input,
		datatypes.NewJSONType(createSpeechRequest.Model),
		(*string)(createSpeechRequest.ResponseFormat),
		createSpeechRequest.Speed,
		string(createSpeechRequest.Voice),
	}

	// FIXME: The correct response here is the audio for the speech.
	if err := db.Create(s.db.DB, newSpeech); err != nil {
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

	// FIXME: The correct response here is the audio for the speech.
	if err := db.Create(s.db.DB, createCompletionRequest); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}
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
	gormDB := s.db.DB
	if z.Dereference(params.Purpose) != "" {
		gormDB = s.db.Where("purpose = ?", *params.Purpose)
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
	file.SetID(uuid.New().String())
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

	if err = db.Create(s.db.DB, file); err != nil {
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
	deleteAndRespond[*db.File](s.db.DB, w, fileID, openai.DeleteFileResponse{
		true,
		fileID,
		openai.DeleteFileResponseObjectFile,
	})
}

func (s *Server) RetrieveFile(w http.ResponseWriter, _ *http.Request, fileID string) {
	getAndRespond[*db.File](s.db.DB, w, fileID)
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
	getAndRespond[*db.FineTuningJob](s.db.DB, w, fineTuningJobID)
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

func (s *Server) ListModels(w http.ResponseWriter, _ *http.Request) {
	listAndRespond[*db.Model](s.db.DB, w)
}

func (s *Server) DeleteModel(w http.ResponseWriter, _ *http.Request, modelID string) {
	//nolint:govet
	deleteAndRespond[*db.Model](s.db.DB, w, modelID, openai.DeleteModelResponse{
		true,
		modelID,
		string(openai.ModelObjectModel),
	})
}

func (s *Server) RetrieveModel(w http.ResponseWriter, _ *http.Request, modelID string) {
	getAndRespond[*db.Model](s.db.DB, w, modelID)
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

	// FIXME: This doesn't support creating a thread with messages.
	//nolint:govet
	publicThread := &openai.ThreadObject{
		// The first two fields will be set on create.
		0,
		"",
		createThreadRequest.Metadata,
		openai.Thread,
	}

	createAndRespond[*db.Thread](s.db.DB, w, publicThread)
}

func (s *Server) CreateThreadAndRun(w http.ResponseWriter, r *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) DeleteThread(w http.ResponseWriter, _ *http.Request, threadID string) {
	//nolint:govet
	deleteAndRespond[*db.Thread](s.db.DB, w, threadID, openai.DeleteThreadResponse{
		true,
		threadID,
		openai.ThreadDeleted,
	})
}

func (s *Server) GetThread(w http.ResponseWriter, _ *http.Request, threadID string) {
	getAndRespond[*db.Thread](s.db.DB, w, threadID)
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

	modify[*db.Thread](s.db.DB, w, threadID, map[string]interface{}{"metadata": reqBody.Metadata})
}

func (s *Server) ListMessages(w http.ResponseWriter, _ *http.Request, threadID string, params openai.ListMessagesParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "list"))))
		return
	}

	gormDB, err := processAssistantsAPIListParams[*db.Message](s.db.DB.Where("thread_id = ?", threadID), params.Limit, params.Before, params.After, params.Order)
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

	content := new(openai.MessageObject_Content_Item)
	if err := content.FromMessageContentTextObject(openai.MessageContentTextObject{
		Text: struct {
			Annotations []openai.MessageContentTextObject_Text_Annotations_Item `json:"annotations"`
			Value       string                                                  `json:"value"`
		}{
			Value: createMessageRequest.Content,
		},
		Type: openai.MessageContentTextObjectTypeText,
	}); err != nil {
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

	createAndRespond[*db.Message](s.db.DB, w, publicMessage)
}

func (s *Server) GetMessage(w http.ResponseWriter, _ *http.Request, threadID string, messageID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "get"))))
		return
	}

	getAndRespond[*db.Message](s.db.Where("thread_id = ?", threadID), w, messageID)
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

	modify[*db.Message](s.db.DB, w, messageID, map[string]interface{}{"metadata": reqBody.Metadata})
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

	gormDB, err := processAssistantsAPIListParams[*db.MessageFile](s.db.Where("thread_id = ? AND message_id = ?", threadID, messageID), params.Limit, params.Before, params.After, params.Order)
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

	getAndRespond[*db.MessageFile](s.db.Where("thread_id = ? AND message_id = ?", threadID, messageID), w, fileID)
}

func (s *Server) ListRuns(w http.ResponseWriter, r *http.Request, threadID string, params openai.ListRunsParams) {
	if threadID == "" {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "list"))))
		return
	}

	gormDB, err := processAssistantsAPIListParams[*db.Run](s.db.Where("thread_id = ?", threadID), params.Limit, params.Before, params.After, params.Order)
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
		"",
		threadID,
		tools,
		nil,
	}

	createAndRespond[*db.Run](s.db.DB, w, publicRun)
}

func (s *Server) GetRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "thread_id", "get"))))
		return
	}

	getAndRespond[*db.Run](s.db.Where("thread_id = ?", threadID), w, runID)
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

	modify[*db.Run](s.db.DB, w, runID, map[string]interface{}{"metadata": reqBody.Metadata})
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

	publicRun, err := db.CancelRun(s.db.Where("thread_id = ?", threadID), runID)
	if err != nil {
		if errors.Is(err, gdb.ErrRecordNotFound) {
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
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) GetRunStep(w http.ResponseWriter, r *http.Request, threadID string, runID string, stepID string) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) SubmitToolOuputsToRun(w http.ResponseWriter, r *http.Request, threadID string, runID string) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
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

func getAndRespond[T db.Transformer](gormDB *gdb.DB, w http.ResponseWriter, id string) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "id", "get"))))
		return
	}

	publicObj, err := db.GetPublicObject[T](gormDB, id)
	if err != nil {
		if errors.Is(err, gdb.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, notFoundError)))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, publicObj)
}

func createAndRespond[T db.Transformer](gormDB *gdb.DB, w http.ResponseWriter, publicObj any) {
	var err error
	publicObj, err = db.CreateFromPublic[T](gormDB, publicObj)
	if err != nil {
		if errors.Is(err, gdb.ErrDuplicatedKey) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error": "already exists"}`))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, publicObj)
}

func processAssistantsAPIListParams[T db.Transformer, O ~string](gormDB *gdb.DB, limit *int, before, after *string, order *O) (*gdb.DB, error) {
	if limit == nil || *limit == 0 {
		limit = z.Pointer(20)
	} else if *limit < 1 || *limit > 100 {
		return nil, fmt.Errorf("limit should be between 1 and 100")
	}

	gormDB = gormDB.Limit(*limit)

	var ordering string
	if order == nil || *order == "" {
		ordering = "desc"
	} else if *order != "asc" && *order != "desc" {
		return nil, fmt.Errorf("order should be asc or desc")
	} else {
		ordering = string(*order)
	}

	gormDB = gormDB.Order(fmt.Sprintf("created_at %s", ordering))

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

	return gormDB, nil
}

func listAndRespond[T db.Transformer](gormDB *gdb.DB, w http.ResponseWriter) {
	publicObjs, err := db.ListPublicObjects[T](gormDB)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, map[string]any{"object": "list", "data": publicObjs})
}

func modify[T db.Transformer](gormDB *gdb.DB, w http.ResponseWriter, id string, updates any) {
	publicObj, err := db.Modify[T](gormDB, id, updates)
	if err != nil {
		if errors.Is(err, gdb.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, notFoundError)))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%v"}`, err)))
		return
	}

	writeObjectToResponse(w, publicObj)
}

func deleteAndRespond[T db.Transformer](gormDB *gdb.DB, w http.ResponseWriter, id string, resp any) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": %s}`, fmt.Sprintf(notEmptyErrorFormat, "id", "delete"))))
		return
	}

	if err := db.Delete[T](gormDB, id); err != nil {
		if errors.Is(err, gdb.ErrRecordNotFound) {
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

// transposeObject will marshal the first object and unmarshal it into the second object.
func transposeObject(first json.Marshaler, second json.Unmarshaler) error {
	firstBytes, err := first.MarshalJSON()
	if err != nil {
		return err
	}

	return second.UnmarshalJSON(firstBytes)
}
