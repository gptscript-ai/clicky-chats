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
	"strconv"
	"strings"
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	kb "github.com/gptscript-ai/clicky-chats/pkg/knowledgebases"
	"github.com/oapi-codegen/runtime"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) ListAssistants(w http.ResponseWriter, r *http.Request, params openai.ListAssistantsParams) {
	gormDB, limit, err := processAssistantsAPIListParams(s.db.WithContext(r.Context()), new(db.Assistant), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	}

	listAndRespond[*db.Assistant](gormDB, w, limit)
}

func (s *Server) CreateAssistant(w http.ResponseWriter, r *http.Request) {
	createAssistantRequest := new(openai.CreateAssistantRequest)
	if err := readObjectFromRequest(r, createAssistantRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	model, err := createAssistantRequest.Model.AsCreateAssistantRequestModel0()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process model.", InvalidRequestErrorType).Error()))
		return
	}

	var retrievalEnabled bool
	tools := make([]openai.AssistantObject_Tools_Item, 0, len(z.Dereference(createAssistantRequest.Tools)))
	for _, tool := range z.Dereference(createAssistantRequest.Tools) {
		t := new(openai.AssistantObject_Tools_Item)
		if err := transposeObject(tool, t); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Failed to process tool.", InvalidRequestErrorType).Error()))
			return
		}

		if t, err := t.AsAssistantToolsRetrieval(); err == nil && t.Type == openai.AssistantToolsRetrievalTypeRetrieval {
			retrievalEnabled = true
		}

		if t, err := t.AsAssistantToolsFunction(); err == nil && t.Type == openai.AssistantToolsFunctionTypeFunction {
			if err := validateToolFunctionName(t.Function.Name); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Invalid function name: %s.", err.Error()), InvalidRequestErrorType).Error()))
				return
			}
		}
		tools = append(tools, *t)
	}

	if len(z.Dereference(createAssistantRequest.FileIds)) != 0 && !retrievalEnabled {
		// Turn on retrieval automatically if we have files
		retrievalTool := new(openai.AssistantObject_Tools_Item)
		//nolint:govet
		if err = retrievalTool.FromAssistantToolsRetrieval(openai.AssistantToolsRetrieval{
			openai.AssistantToolsRetrievalTypeRetrieval,
		}); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(NewAPIError("Failed to add retrieval tool.", InvalidRequestErrorType).Error()))
			return
		}
		tools = append(tools, *retrievalTool)
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

	// We're splitting creation in DB and returning the response here, since we first want
	// to manage the assistant knowledge base

	// Create the assistant in the database
	a := new(db.Assistant)
	if err := create(s.db.WithContext(r.Context()), a, publicAssistant); err != nil {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	// Handle the assistant knowledge base
	initKnowledgeBase := func(kbm *kb.KnowledgeBaseManager, asst *db.Assistant) error {
		kb, err := kbm.NewAssistantKnowledgeBase(r.Context(), asst.ID)
		if err != nil {
			return err
		}

		slog.Debug("Created assistant knowledge base", "id", asst.ID, "name", asst.Name, "kb", kb)

		for _, file := range asst.FileIDs {
			err := kbm.AddFile(r.Context(), kb, file)
			if err != nil {
				slog.Error("Failed to add file to assistant knowledge base", "file", file, "err", err)
				return err
			}
			slog.Debug("Added files to assistant knowledge base", "id", asst.ID, "name", asst.Name, "kb", kb, "#files", len(asst.FileIDs))
		}
		return nil
	}

	if s.kbm != nil {
		if err := initKnowledgeBase(s.kbm, a); err != nil {
			slog.Error("Failed to initialize assistant knowledge base", "id", a.ID, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to initialize assistant knowledge base: %s", err.Error()), InternalErrorType).Error()))
			err = db.Delete[db.Assistant](s.db.WithContext(r.Context()), a.ID)
			if err != nil {
				slog.Error("Failed to cleanup assistant that failed to create", "id", a.ID, "err", err)
			}
			return
		}
	}

	// Now return the assistant in the response
	writeObjectToResponse(w, a.ToPublic())
}

func (s *Server) DeleteAssistant(w http.ResponseWriter, r *http.Request, assistantID string) {
	if s.kbm != nil {
		err := s.kbm.DeleteKnowledgeBase(r.Context(), assistantID)
		if err != nil {
			slog.Error("Failed to delete assistant knowledge base", "id", assistantID, "err", err)
		}
	}

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
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("assistant_id").Error()))
		return
	}

	modifyAssistantRequest := new(openai.ModifyAssistantRequest)
	if err := readObjectFromRequest(r, modifyAssistantRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	err := validateMetadata(modifyAssistantRequest.Metadata)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	var model openai.ModifyAssistantRequestModel0
	if modifyAssistantRequest.Model != nil {
		model, err = modifyAssistantRequest.Model.AsModifyAssistantRequestModel0()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Failed to process model.", InvalidRequestErrorType).Error()))
			return
		}
	}

	var tools []openai.AssistantObject_Tools_Item
	if modifyAssistantRequest.Tools != nil {
		tools = make([]openai.AssistantObject_Tools_Item, 0, len(*modifyAssistantRequest.Tools))
		for _, tool := range *modifyAssistantRequest.Tools {
			t := new(openai.AssistantObject_Tools_Item)
			if err = transposeObject(tool, t); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(NewAPIError("Failed to process tool.", InvalidRequestErrorType).Error()))
				return
			}
			tools = append(tools, *t)
		}
	}

	var targetFileIDs []string
	if modifyAssistantRequest.FileIds != nil {
		targetFileIDs = z.Dereference(modifyAssistantRequest.FileIds)

		existingFileIDs, err := s.kbm.ListFiles(r.Context(), assistantID)
		if err != nil {
			slog.Error("Failed to list assistant files", "id", assistantID, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(NewAPIError("Failed to list assistant files.", InternalErrorType).Error()))
			return
		}

		slog.Debug("Potential file changes", "existing", existingFileIDs, "target", targetFileIDs)

		// Add any new files to the assistant knowledge base
		for _, fileID := range targetFileIDs {
			if !slices.Contains(existingFileIDs, fileID) {
				slog.Debug("Adding file to assistant knowledge base", "id", assistantID, "file", fileID)
				if err := s.kbm.AddFile(r.Context(), assistantID, fileID); err != nil {
					slog.Error("Failed to add file to assistant knowledge base", "id", assistantID, "file", fileID, "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(NewAPIError("Failed to add file to assistant knowledge base.", InternalErrorType).Error()))
					return
				}
			}
		}

		// Remove any files that are no longer in the assistant knowledge base
		for _, fileID := range existingFileIDs {
			if !slices.Contains(targetFileIDs, fileID) {
				slog.Debug("Removing file from assistant knowledge base", "id", assistantID, "file", fileID)
				if err := s.kbm.RemoveFile(r.Context(), assistantID, fileID); err != nil {
					slog.Error("Failed to remove file from assistant knowledge base", "id", assistantID, "file", fileID, "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(NewAPIError("Failed to remove file from assistant knowledge base.", InternalErrorType).Error()))
					return
				}
			}
		}
	}

	if len(tools) == 0 {
		// This request isn't updating the tools on the assistant.
		// Therefore, get the tool from the existing assistant.
		existingAssistant := &db.Assistant{
			Metadata: db.Metadata{
				Base: db.Base{ID: assistantID},
			},
		}
		if err = db.Get(s.db.WithContext(r.Context()), existingAssistant, assistantID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(NewNotFoundError(existingAssistant).Error()))
				return
			}
			slog.Error("Failed to get assistant", "id", assistantID, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(NewAPIError("Failed to get assistant.", InternalErrorType).Error()))
			return
		}

		tools = existingAssistant.Tools
	}

	retrievalIndex := -1
	for i, tool := range tools {
		if t, err := tool.AsAssistantToolsRetrieval(); err == nil && t.Type == openai.AssistantToolsRetrievalTypeRetrieval {
			retrievalIndex = i
			break
		}
	}

	if len(targetFileIDs) > 0 && retrievalIndex == -1 {
		// Ensure the assistant has the retrieval tool
		retrievalTool := new(openai.AssistantObject_Tools_Item)
		//nolint:govet
		if err = retrievalTool.FromAssistantToolsRetrieval(openai.AssistantToolsRetrieval{
			openai.AssistantToolsRetrievalTypeRetrieval,
		}); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(NewAPIError("Failed to add retrieval tool.", InvalidRequestErrorType).Error()))
			return
		}

		tools = append(tools, *retrievalTool)
	} else if len(targetFileIDs) == 0 && retrievalIndex != -1 {
		// Ensure the assistant does not have the retrieval tool
		tools = append(tools[:retrievalIndex], tools[retrievalIndex+1:]...)
	}

	assistant := &db.Assistant{
		Metadata: db.Metadata{
			Base:     db.Base{ID: assistantID},
			Metadata: z.Dereference(modifyAssistantRequest.Metadata),
		},
		Description:  modifyAssistantRequest.Description,
		FileIDs:      targetFileIDs,
		Instructions: modifyAssistantRequest.Instructions,
		Model:        model,
		Name:         modifyAssistantRequest.Name,
		Tools:        datatypes.NewJSONSlice(tools),
	}

	modifyAndRespond(s.db.WithContext(r.Context()), w, assistant, assistant)
}

func (s *Server) ListAssistantFiles(w http.ResponseWriter, r *http.Request, assistantID string, params openai.ListAssistantFilesParams) {
	if assistantID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("assistant_id").Error()))
		return
	}

	gormDB, limit, err := processAssistantsAPIListParams(
		s.db.WithContext(r.Context()), new(db.AssistantFile), params.Limit, params.Before, params.After, params.Order,
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

	speech := new(db.CreateSpeechRequest)
	if err := speech.FromPublic(createSpeechRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	var (
		ctx    = r.Context()
		gormDB = s.db.WithContext(ctx)
	)
	if err := db.Create(gormDB, speech); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create speech.", InternalErrorType).Error()))
		return
	}

	// Kick the audio runner to check for new requests.
	ready := s.triggers.Audio.Kick(speech.ID)

	speechResponse := new(db.CreateSpeechResponse)
	if err := waitForResponse(ctx, ready, gormDB, speech.ID, speechResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to get response: %v", err), InternalErrorType).Error()))
		return
	}

	if errStr := speechResponse.GetErrorString(); errStr != "" {
		code := speechResponse.GetStatusCode()
		errorType := InternalErrorType
		if code < 500 {
			errorType = InvalidRequestErrorType
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(NewAPIError(errStr, errorType).Error()))
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := w.Write(speechResponse.Content); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to write response: %v", err), InternalErrorType).Error()))
		return
	}
}

func (s *Server) CreateTranscription(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB max
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(NewAPIError("Failed to parse multipart form.", InvalidRequestErrorType).Error()))
		return
	}

	value := r.MultipartForm.Value
	if len(value) < 1 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Invalid number of multipart form values.", InvalidRequestErrorType).Error()))
	}

	publicReq := new(openai.CreateTranscriptionRequest)

	// Extract non-file fields
	if languages, ok := value["language"]; ok {
		if len(languages) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of languages.", InvalidRequestErrorType).Error()))
			return
		}

		publicReq.Prompt = &languages[0]
	}

	models := value["model"]
	if len(models) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Invalid number of models.", InvalidRequestErrorType).Error()))
		return
	}
	if err := (&publicReq.Model).FromCreateTranscriptionRequestModel1(openai.CreateTranscriptionRequestModel1(models[0])); err != nil {
		if err = (&publicReq.Model).FromCreateTranscriptionRequestModel0(models[0]); err != nil {
			// Invalid model type
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of models.", InvalidRequestErrorType).Error()))
			return
		}
	}

	if prompts, ok := value["prompt"]; ok {
		if len(prompts) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of prompts.", InvalidRequestErrorType).Error()))
			return
		}

		publicReq.Prompt = &prompts[0]
	}

	if formats, ok := value["response_format"]; ok {
		if len(formats) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of response_formats.", InvalidRequestErrorType).Error()))
			return
		}

		publicReq.ResponseFormat = z.Pointer(openai.CreateTranscriptionRequestResponseFormat(formats[0]))
	}

	if temperatures, ok := value["temperature"]; ok {
		if len(temperatures) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of temperatures.", InvalidRequestErrorType).Error()))
			return
		}

		temperature, err := strconv.ParseFloat(temperatures[0], 32)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Failed to process temperature.", InvalidRequestErrorType).Error()))
			return
		}

		publicReq.Temperature = z.Pointer(float32(temperature))
	}

	if timestampGranularities, ok := value["timestamp_granularities"]; ok {
		if len(timestampGranularities) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of timestamp_granularities.", InvalidRequestErrorType).Error()))
			return
		}

		var granularities *[]openai.CreateTranscriptionRequestTimestampGranularities
		for _, g := range timestampGranularities[0] {
			*granularities = append(*granularities, openai.CreateTranscriptionRequestTimestampGranularities(g))
		}

		if len(*granularities) > 0 {
			publicReq.TimestampGranularities = granularities
		}
	}

	// Extract file field
	files := r.MultipartForm.File["file"]
	if len(files) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Invalid number of files.", InvalidRequestErrorType).Error()))
		return
	}
	(&publicReq.File).InitFromMultipart(files[0])

	agentReq := new(db.CreateTranscriptionRequest)
	if err := agentReq.FromPublic(publicReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	var (
		ctx    = r.Context()
		gormDB = s.db.WithContext(ctx)
	)
	if err := db.Create(gormDB, agentReq); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create transcription request.", InternalErrorType).Error()))
		return
	}

	// Kick the audio runner to check for new requests.
	ready := s.triggers.Audio.Kick(agentReq.ID)

	waitForAndWriteResponse(ctx, ready, w, gormDB, agentReq.ID, new(db.CreateTranscriptionResponse))
}

func (s *Server) CreateTranslation(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB max
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(NewAPIError("Failed to parse multipart form.", InvalidRequestErrorType).Error()))
		return
	}

	value := r.MultipartForm.Value
	if len(value) < 1 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Invalid number of multipart form values.", InvalidRequestErrorType).Error()))
	}

	publicReq := new(openai.CreateTranslationRequest)

	// Extract non-file fields
	models := value["model"]
	if len(models) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Invalid number of models.", InvalidRequestErrorType).Error()))
		return
	}
	if err := (&publicReq.Model).FromCreateTranslationRequestModel1(openai.CreateTranslationRequestModel1(models[0])); err != nil {
		if err = (&publicReq.Model).FromCreateTranslationRequestModel0(models[0]); err != nil {
			// Invalid model type
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of models.", InvalidRequestErrorType).Error()))
			return
		}
	}

	if prompts, ok := value["prompt"]; ok {
		if len(prompts) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of prompts.", InvalidRequestErrorType).Error()))
			return
		}

		publicReq.Prompt = &prompts[0]
	}

	if formats, ok := value["response_format"]; ok {
		if len(formats) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of response_formats.", InvalidRequestErrorType).Error()))
			return
		}

		publicReq.Prompt = &formats[0]
	}

	if temperatures, ok := value["temperature"]; ok {
		if len(temperatures) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Invalid number of temperatures.", InvalidRequestErrorType).Error()))
			return
		}

		temperature, err := strconv.ParseFloat(temperatures[0], 32)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Failed to process temperature.", InvalidRequestErrorType).Error()))
			return
		}

		publicReq.Temperature = z.Pointer(float32(temperature))
	}

	// Extract file field
	files := r.MultipartForm.File["file"]
	if len(files) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Invalid number of files.", InvalidRequestErrorType).Error()))
		return
	}
	(&publicReq.File).InitFromMultipart(files[0])

	agentReq := new(db.CreateTranslationRequest)
	if err := agentReq.FromPublic(publicReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	var (
		ctx    = r.Context()
		gormDB = s.db.WithContext(ctx)
	)
	if err := db.Create(gormDB, agentReq); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create translation request.", InternalErrorType).Error()))
		return
	}

	// Kick the audio runner to check for new requests.
	ready := s.triggers.Audio.Kick(agentReq.ID)

	waitForAndWriteResponse(ctx, ready, w, gormDB, agentReq.ID, new(db.CreateTranslationResponse))
}

func (s *Server) CreateChatCompletion(w http.ResponseWriter, r *http.Request) {
	createCompletionRequest := new(openai.CreateChatCompletionRequest)
	if err := readObjectFromRequest(r, createCompletionRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	ccr := new(db.CreateChatCompletionRequest)
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

	// Kick the chat completion runner to check for new requests, and get the ready signal.
	ready := s.triggers.ChatCompletion.Kick(ccr.ID)

	if !z.Dereference(ccr.Stream) {
		waitForAndWriteResponse(r.Context(), ready, w, gormDB, ccr.ID, new(db.CreateChatCompletionResponse))
		return
	}

	waitForAndStreamResponse[*db.ChatCompletionResponseChunk](r.Context(), w, gormDB, ccr.ID, 0)
}

func (s *Server) CreateCompletion(w http.ResponseWriter, _ *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateEmbedding(w http.ResponseWriter, r *http.Request) {
	createEmbeddingRequest := new(openai.CreateEmbeddingRequest)
	if err := readObjectFromRequest(r, createEmbeddingRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	cer := new(db.CreateEmbeddingRequest)
	if err := cer.FromPublic(createEmbeddingRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	gormDB := s.db.WithContext(r.Context())
	if err := db.Create(gormDB, cer); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create embeddings request.", InternalErrorType).Error()))
		return
	}

	// Kick the embeddings runner to check for new requests.
	ready := s.triggers.Embeddings.Kick(cer.ID)

	waitForAndWriteResponse(r.Context(), ready, w, gormDB, cer.ID, new(db.CreateEmbeddingResponse))
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

func (s *Server) DownloadFile(w http.ResponseWriter, _ *http.Request, _ string) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) ListPaginatedFineTuningJobs(w http.ResponseWriter, _ *http.Request, _ openai.ListPaginatedFineTuningJobsParams) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateFineTuningJob(w http.ResponseWriter, _ *http.Request) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) RetrieveFineTuningJob(w http.ResponseWriter, r *http.Request, fineTuningJobID string) {
	getAndRespond(s.db.WithContext(r.Context()), w, new(db.FineTuningJob), fineTuningJobID)
}

func (s *Server) CancelFineTuningJob(w http.ResponseWriter, _ *http.Request, _ string) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) ListFineTuningEvents(w http.ResponseWriter, _ *http.Request, _ string, _ openai.ListFineTuningEventsParams) {
	//TODO implement me
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateImageEdit(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to parse multipart form.", InvalidRequestErrorType).Error()))
		return
	}

	publicReq := new(openai.CreateImageEditRequest)
	if err := runtime.BindMultipart(publicReq, *reader); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to bind multipart form.", InvalidRequestErrorType).Error()))
		return
	}

	agentReq := new(db.CreateImageEditRequest)
	if err := agentReq.FromPublic(publicReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	var (
		ctx    = r.Context()
		gormDB = s.db.WithContext(ctx)
	)
	if err := db.Create(gormDB, agentReq); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create chat completion request.", InternalErrorType).Error()))
		return
	}

	// Kick the image runner to check for new requests.
	ready := s.triggers.Image.Kick(agentReq.ID)

	waitForAndWriteResponse(ctx, ready, w, gormDB, agentReq.ID, new(db.ImagesResponse))
}

func (s *Server) CreateImage(w http.ResponseWriter, r *http.Request) {
	publicReq := new(openai.CreateImageRequest)
	if err := readObjectFromRequest(r, publicReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	agentReq := new(db.CreateImageRequest)
	if err := agentReq.FromPublic(publicReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	var (
		ctx    = r.Context()
		gormDB = s.db.WithContext(ctx)
	)
	if err := db.Create(gormDB, agentReq); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create chat completion request.", InternalErrorType).Error()))
		return
	}

	// Kick the image runner to check for new requests.
	ready := s.triggers.Image.Kick(agentReq.ID)

	waitForAndWriteResponse(ctx, ready, w, gormDB, agentReq.ID, new(db.ImagesResponse))
}

func (s *Server) CreateImageVariation(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to parse multipart form.", InvalidRequestErrorType).Error()))
		return
	}

	publicReq := new(openai.CreateImageVariationRequest)
	if err := runtime.BindMultipart(publicReq, *reader); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to bind multipart form.", InvalidRequestErrorType).Error()))
		return
	}

	agentReq := new(db.CreateImageVariationRequest)
	if err := agentReq.FromPublic(publicReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	var (
		ctx    = r.Context()
		gormDB = s.db.WithContext(ctx)
	)
	if err := db.Create(gormDB, agentReq); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to create image variation request.", InternalErrorType).Error()))
		return
	}

	// Kick the image runner to check for new requests.
	ready := s.triggers.Image.Kick(agentReq.ID)

	waitForAndWriteResponse(ctx, ready, w, gormDB, agentReq.ID, new(db.ImagesResponse))
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

func (s *Server) CreateModeration(w http.ResponseWriter, _ *http.Request) {
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
	if err := s.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := create(tx, thread, publicThread); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return err
		}

		if createThreadRequest.Messages == nil {
			// No messages to create
			return nil
		}

		for _, message := range *createThreadRequest.Messages {
			content, err := db.MessageContentFromString(message.Content)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(NewAPIError("Failed to process message content.", InvalidRequestErrorType).Error()))
				return err
			}

			//nolint:govet
			publicMessage := &openai.MessageObject{
				nil,
				nil,
				[]openai.MessageObject_Content_Item{*content},
				0,
				z.Dereference(message.FileIds),
				"",
				nil,
				nil,
				message.Metadata,
				openai.ThreadMessage,
				openai.MessageObjectRole(message.Role),
				nil,
				"",
				thread.ID,
			}

			if err := create(tx, new(db.Message), publicMessage); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(err.Error()))
				return err
			}
		}

		return nil
	}); err != nil {
		slog.Error("failed to create thread")
		return
	}

	writeObjectToResponse(w, thread.ToPublic())
}

func (s *Server) CreateThreadAndRun(w http.ResponseWriter, r *http.Request) {
	createThreadAndRunRequest := new(openai.CreateThreadAndRunRequest)
	if err := readObjectFromRequest(r, createThreadAndRunRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err := validateMetadata(createThreadAndRunRequest.Metadata); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	//nolint:govet
	publicThread := &openai.ThreadObject{
		// The first two fields will be set on create.
		0,
		"",
		createThreadAndRunRequest.Metadata,
		openai.Thread,
	}

	var (
		gormDB = s.db.WithContext(r.Context())
		thread = new(db.Thread)
	)
	if err := gormDB.Transaction(func(tx *gorm.DB) error {
		if err := create(tx, thread, publicThread); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
			return err
		}

		if publicThread := createThreadAndRunRequest.Thread; publicThread != nil && publicThread.Messages != nil {
			for _, message := range *publicThread.Messages {
				content, err := db.MessageContentFromString(message.Content)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(NewAPIError("Failed to process message content.", InvalidRequestErrorType).Error()))
					return err
				}

				//nolint:govet
				publicMessage := &openai.MessageObject{
					nil,
					nil,
					[]openai.MessageObject_Content_Item{*content},
					0,
					z.Dereference(message.FileIds),
					"",
					nil,
					nil,
					message.Metadata,
					openai.ThreadMessage,
					openai.MessageObjectRole(message.Role),
					nil,
					"",
					thread.ID,
				}

				if err = create(tx, new(db.Message), publicMessage); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(err.Error()))
					return err
				}
			}
		}

		return nil
	}); err != nil {
		slog.Error("failed to create thread", "err", err)
		return
	}

	var tools []openai.RunObject_Tools_Item
	for _, tool := range z.Dereference(createThreadAndRunRequest.Tools) {
		t := new(openai.RunObject_Tools_Item)
		if err := transposeObject(tool, t); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError("Failed to process tool.", InvalidRequestErrorType).Error()))
			return
		}
		tools = append(tools, *t)
	}

	//nolint:govet
	publicRun := &openai.RunObject{
		createThreadAndRunRequest.AssistantId,
		nil,
		nil,
		0,
		nil,
		nil,
		nil,
		"",
		z.Dereference(createThreadAndRunRequest.Instructions),
		nil,
		createThreadAndRunRequest.Metadata,
		z.Dereference(createThreadAndRunRequest.Model),
		openai.ThreadRun,
		nil,
		nil,
		openai.RunObjectStatusQueued,
		thread.ID,
		tools,
		nil,
	}

	run := new(db.Run)
	if err := run.FromPublic(publicRun); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError("Failed to process request.", InvalidRequestErrorType).Error()))
		return
	}

	runCreatedEvent := &db.RunEvent{
		EventName: string(openai.ThreadRunCreated),
		Run:       datatypes.NewJSONType(run),
	}
	runQueuedEvent := &db.RunEvent{
		EventName:   string(openai.ThreadRunQueued),
		Run:         datatypes.NewJSONType(run),
		ResponseIdx: 1,
	}

	if err := gormDB.Transaction(func(tx *gorm.DB) error {
		run.EventIndex = 1
		if err := db.Create(tx, run); err != nil {
			return err
		}

		runCreatedEvent.RequestID = run.ID
		if err := db.Create(tx, runCreatedEvent); err != nil {
			return err
		}

		runQueuedEvent.RequestID = run.ID
		if err := db.Create(tx, runQueuedEvent); err != nil {
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

	gormDB, limit, err := processAssistantsAPIListParams(
		s.db.WithContext(r.Context()), new(db.Message), params.Limit, params.Before, params.After, params.Order,
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
		nil,
		[]openai.MessageObject_Content_Item{*content},
		0,
		z.Dereference(createMessageRequest.FileIds),
		"",
		nil,
		nil,
		createMessageRequest.Metadata,
		openai.ThreadMessage,
		openai.MessageObjectRole(createMessageRequest.Role),
		nil,
		"",
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

	gormDB, limit, err := processAssistantsAPIListParams(
		s.db.WithContext(r.Context()), new(db.MessageFile), params.Limit, params.Before, params.After, params.Order,
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

	gormDB, limit, err := processAssistantsAPIListParams(
		s.db.WithContext(r.Context()), new(db.Run), params.Limit, params.Before, params.After, params.Order,
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
		nil,
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

	runCreatedEvent := &db.RunEvent{
		EventName: string(openai.ThreadRunCreated),
		Run:       datatypes.NewJSONType(run),
	}
	runQueuedEvent := &db.RunEvent{
		EventName:   string(openai.ThreadRunQueued),
		Run:         datatypes.NewJSONType(run),
		ResponseIdx: 1,
	}

	if err := gormDB.Transaction(func(tx *gorm.DB) error {
		run.EventIndex = 1
		if err := db.Create(tx, run); err != nil {
			return err
		}

		runCreatedEvent.RequestID = run.ID
		if err := db.Create(tx, runCreatedEvent); err != nil {
			return err
		}

		runQueuedEvent.RequestID = run.ID
		if err := db.Create(tx, runQueuedEvent); err != nil {
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

	// Kick the run runner to check for new requests.
	// Don't need the ready channel here because the response is getting written immediately.
	s.triggers.Run.Kick(run.ID)

	if !z.Dereference(createRunRequest.Stream) {
		writeObjectToResponse(w, run.ToPublic())
		return
	}

	waitForAndStreamResponse[*db.RunEvent](r.Context(), w, gormDB, run.ID, 0)
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

	gormDB, limit, err := processAssistantsAPIListParams(
		s.db.WithContext(r.Context()), new(db.RunStep), params.Limit, params.Before, params.After, params.Order,
		&db.Thread{Metadata: db.Metadata{Base: db.Base{ID: threadID}}},
		&db.Run{Metadata: db.Metadata{Base: db.Base{ID: runID}}},
	)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	listAndRespond[*db.RunStep](gormDB.Where("run_id = ?", runID), w, limit)
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

	getAndRespond(s.db.WithContext(r.Context()).Where("run_id = ?", runID), w, new(db.RunStep), stepID)
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
	if err := db.List(s.db.WithContext(r.Context()).Where("run_id = ?", runID).Where("status = ?", string(openai.RunObjectStatusInProgress)).Order("created_at desc").Limit(1), &runSteps); err != nil {
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
	if runStep.Status != string(openai.RunStepObjectStatusInProgress) || len(runStepFunctionCalls) == 0 {
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

	var eventIndexStart int
	stepDetailsHack := map[string]any{
		"tool_calls": runStepFunctionCalls,
		"type":       openai.RunStepDetailsToolCallsObjectTypeToolCalls,
	}
	if err = s.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		run := new(db.Run)
		if err := tx.Model(run).Clauses(clause.Returning{}).Where("id = ?", runID).Updates(map[string]any{"status": string(openai.RunObjectStatusQueued), "required_action": nil}).Error; err != nil {
			return err
		}

		if err := tx.Model(runStep).Clauses(clause.Returning{}).Where("id = ?", runStep.ID).Updates(map[string]any{"status": string(openai.RunObjectStatusCompleted), "step_details": datatypes.NewJSONType(stepDetailsHack)}).Error; err != nil {
			return err
		}

		run.EventIndex++
		runEvent := &db.RunEvent{
			EventName: string(openai.ThreadRunStepCompleted),
			JobResponse: db.JobResponse{
				RequestID: run.ID,
			},
			RunStep:     datatypes.NewJSONType(runStep),
			ResponseIdx: run.EventIndex,
		}

		if err := db.Create(tx, runEvent); err != nil {
			return err
		}

		eventIndexStart = run.EventIndex
		return tx.Model(run).Clauses(clause.Returning{}).Where("id = ?", runID).Updates(map[string]any{"event_index": run.EventIndex}).Error
	}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to submit tool outputs: %v", err), InternalErrorType).Error()))
		return
	}

	if !z.Dereference(outputs.Stream) {
		writeObjectToResponse(w, runStep.ToPublic())
		return
	}

	waitForAndStreamResponse[*db.RunEvent](r.Context(), w, s.db.WithContext(r.Context()), runID, eventIndexStart)
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

func processAssistantsAPIListParams[O ~string](gormDB *gorm.DB, obj Transformer, limit *int, before, after *string, order *O, ensureExists ...db.Storer) (*gorm.DB, int, error) {
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

	gormDBInstance := gormDB.Limit(*limit)

	ordering := string(z.Dereference(order))
	if ordering == "" {
		ordering = "desc"
	} else if *order != "asc" && *order != "desc" {
		return nil, 0, NewAPIError("Order must be 'asc' or 'desc'.", InvalidRequestErrorType)
	}

	alligator := "<"
	if ordering == "desc" {
		alligator = ">"
	}

	// TODO(thedadams): what happens if before/after are not valid object IDs?
	// TODO(thedadams): what happens if before and after are set?
	// TODO(thedadams): what happens if before/after are in the wrong order?

	if b := z.Dereference(before); b != "" {
		obj.SetID(b)
		if err := db.Get(gormDB, obj, b); err != nil {
			return nil, 0, NewNotFoundError(obj)
		}

		gormDBInstance = gormDBInstance.Where(fmt.Sprintf("created_at %s ?", alligator), obj.GetCreatedAt()).Or(fmt.Sprintf("created_at %s= ? AND id %[1]s ?", alligator), obj.GetCreatedAt(), obj.GetID())
	}
	if a := z.Dereference(after); a != "" {
		obj.SetID(a)
		if err := db.Get(gormDB, obj, a); err != nil {
			return nil, 0, NewNotFoundError(obj)
		}

		gormDBInstance = gormDBInstance.Where(fmt.Sprintf("? %s created_at", alligator), obj.GetCreatedAt()).Or(fmt.Sprintf("? %s= created_at AND ? %[1]s id", alligator), obj.GetCreatedAt(), obj.GetID())
	}

	gormDBInstance = gormDBInstance.Order("created_at " + ordering).Order("id " + ordering)

	return gormDBInstance, *limit, nil
}

func list[T any](gormDB *gorm.DB, objs *[]T) error {
	return db.List(gormDB, objs)
}

func listAndRespond[T Transformer](gormDB *gorm.DB, w http.ResponseWriter, limit int) {
	var objs []T
	if err := list(gormDB, &objs); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to list objects.", InternalErrorType).Error()))
		return
	}

	var (
		firstID, lastID string
		hasMore         bool
	)
	if len(objs) > 0 && limit > 0 {
		hasMore = len(objs) >= limit
		if hasMore {
			objs = objs[:len(objs)-1]
		}
		firstID = objs[0].GetID()
		lastID = objs[len(objs)-1].GetID()
	}

	publicObjs := make([]any, 0, len(objs))
	for _, o := range objs {
		publicObjs = append(publicObjs, o.ToPublic())
	}

	respondWithList(w, publicObjs, hasMore, limit, firstID, lastID)
}

func respondWithList(w http.ResponseWriter, publicObjs []any, hasMore bool, limit int, firstID, lastID string) {
	result := map[string]any{"object": "list", "data": publicObjs}

	if limit != -1 {
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

func waitForResponse(ctx context.Context, readyIndicator <-chan struct{}, gormDB *gorm.DB, id string, obj JobRunner) error {
	timer := time.NewTimer(time.Second)
	defer func() {
		if !timer.Stop() {
			// Ensure the timer channel has been drained.
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// Ensure the timer channel is drained
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		case <-readyIndicator:
		}

		err := gormDB.Model(obj).Where("request_id = ?", id).First(obj).Error
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if !timer.Stop() {
			// Ensure the timer channel has been drained.
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(time.Second)
	}
}

func waitForAndWriteResponse(ctx context.Context, readyIndicator <-chan struct{}, w http.ResponseWriter, gormDB *gorm.DB, id string, respObj JobResponder) {
	if err := waitForResponse(ctx, readyIndicator, gormDB, id, respObj); err != nil {
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

func waitForAndStreamResponse[T JobRespondStreamer](ctx context.Context, w http.ResponseWriter, gormDB *gorm.DB, id string, index int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	var printDoneEvent bool
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		respObj := *new(T)
		if err := gormDB.Model(respObj).Where("request_id = ?", id).Where("response_idx >= ?", index).Order("response_idx asc").First(&respObj).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			time.Sleep(time.Second)
			continue
		} else if err != nil {
			slog.Error("Failed to get response chunk", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed streaming responses: %v", err), InternalErrorType).Error()))
			break
		} else if errStr := respObj.GetErrorString(); errStr != "" {
			slog.Error("Failed to get response chunk", "err", errStr)
			_, _ = w.Write([]byte(fmt.Sprintf(`data: %v`, NewAPIError(errStr, InternalErrorType).Error())))
			break
		}

		index = respObj.GetIndex() + 1
		if respObj.IsDone() {
			break
		}

		respObj.SetID(id)
		body, err := json.Marshal(respObj.ToPublic())
		if err != nil {
			slog.Error("Failed to marshal response", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(fmt.Sprintf(`data: %v`, NewAPIError(fmt.Sprintf("Failed to process streamed response: %v", err), InternalErrorType).Error())))
			break
		}

		event := respObj.GetEvent()
		if event != "" {
			printDoneEvent = true
			event = fmt.Sprintf("event: %s\n", event)
		}

		d := make([]byte, 0, len(body)+len(event)+9)
		_, _ = w.Write(append(append(append(append(d, []byte(event)...), []byte("data: ")...), body...), []byte("\n\n")...))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	doneMessage := "data: [DONE]\n\n"
	if printDoneEvent {
		doneMessage = "event: done\ndata: [DONE]\n\n"
	}
	_, _ = w.Write([]byte(doneMessage))
}

// transposeObject will marshal the first object and unmarshal it into the second object.
func transposeObject(first json.Marshaler, second json.Unmarshaler) error {
	firstBytes, err := first.MarshalJSON()
	if err != nil {
		return err
	}

	return second.UnmarshalJSON(firstBytes)
}
