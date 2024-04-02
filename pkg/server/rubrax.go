package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"gorm.io/gorm"
)

func (s *Server) XListThreads(w http.ResponseWriter, r *http.Request, params openai.XListThreadsParams) {
	gormDB, limit, err := processAssistantsAPIListParams(s.db.WithContext(r.Context()), new(db.Thread), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	}

	listAndRespond[*db.Thread](gormDB, w, limit)
}

func (s *Server) XListTools(w http.ResponseWriter, r *http.Request, params openai.XListToolsParams) {
	gormDB, limit, err := processAssistantsAPIListParams(s.db.WithContext(r.Context()), new(db.Tool), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	}

	listAndRespond[*db.Tool](gormDB, w, limit)
}

func (s *Server) XCreateTool(w http.ResponseWriter, r *http.Request) {
	createToolRequest := new(openai.XCreateToolRequest)
	err := readObjectFromRequest(r, createToolRequest)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err = validateToolEnvVars(z.Dereference(createToolRequest.EnvVars)); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	//nolint:govet
	tool := &db.Tool{
		db.Base{},
		"",
		"",
		createToolRequest.Contents,
		createToolRequest.Url,
		createToolRequest.Subtool,
		z.Dereference(createToolRequest.EnvVars),
		nil,
	}

	tool.Name, tool.Description, tool.Program, err = toolToProgram(r.Context(), tool)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err = db.Create(s.db.WithContext(r.Context()), tool); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			_, _ = w.Write([]byte(NewAPIError("Object already exists.", InvalidRequestErrorType).Error()))
		} else {
			_, _ = w.Write([]byte(NewAPIError("Failed to create object.", InternalErrorType).Error()))
		}
		return
	}

	writeObjectToResponse(w, tool.ToPublic())
}

func (s *Server) XDeleteTool(w http.ResponseWriter, r *http.Request, toolID string) {
	//nolint:govet
	deleteAndRespond[*db.Tool](s.db.WithContext(r.Context()), w, toolID, openai.XDeleteToolResponse{
		true,
		toolID,
		openai.ToolDeleted,
	})
}

func (s *Server) XGetTool(w http.ResponseWriter, r *http.Request, toolID string) {
	getAndRespond(s.db.WithContext(r.Context()), w, new(db.Tool), toolID)
}

func (s *Server) XModifyTool(w http.ResponseWriter, r *http.Request, toolID string) {
	modifyToolRequest := new(openai.XModifyToolRequest)
	err := readObjectFromRequest(r, modifyToolRequest)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err = validateToolEnvVars(z.Dereference(modifyToolRequest.EnvVars)); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if z.Dereference(modifyToolRequest.Contents) == "" && z.Dereference(modifyToolRequest.Url) == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("s content or url").Error()))
		return
	}

	existingTool := new(db.Tool)
	if err = s.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err = db.Get(tx, existingTool, toolID); err != nil {
			return err
		}

		existingTool.Subtool = modifyToolRequest.Subtool
		existingTool.EnvVars = z.Dereference(modifyToolRequest.EnvVars)

		retool := z.Dereference(modifyToolRequest.Retool)
		if newURL := modifyToolRequest.Url; z.Dereference(newURL) != z.Dereference(existingTool.URL) {
			retool = true
			existingTool.URL = newURL
		} else if newContents := modifyToolRequest.Contents; z.Dereference(newContents) != z.Dereference(existingTool.Contents) {
			retool = true
			existingTool.Contents = newContents
		}

		if retool {
			existingTool.Name, existingTool.Description, existingTool.Program, err = toolToProgram(r.Context(), existingTool)
			if err != nil {
				return err
			}
		}

		if err = db.Modify(tx, existingTool, toolID, existingTool); err != nil {
			return err
		}

		return nil
	}); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	writeObjectToResponse(w, existingTool.ToPublic())
}

func (s *Server) XStreamRun(w http.ResponseWriter, r *http.Request, threadID string, runID string, params openai.XStreamRunParams) {
	if runID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("run_id").Error()))
		return
	}
	if threadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("thread_id").Error()))
		return
	}

	gormDB := s.db.WithContext(r.Context())
	run := &db.Run{
		Metadata: db.Metadata{
			Base: db.Base{
				ID: runID,
			},
		},
	}
	if err := db.Get(gormDB.Where("thread_id = ?", threadID), run, runID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(NewNotFoundError(run).Error()))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to get run: %v", err), InternalErrorType).Error()))
		return
	}

	if db.IsTerminal(run.Status) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Run %s is in terminal state: %s", runID, run.Status), InvalidRequestErrorType).Error()))
		return
	}

	waitForAndStreamResponse[*db.RunEvent](r.Context(), w, gormDB, runID, z.Dereference(params.Index))
}

func (s *Server) XListRunStepEvents(w http.ResponseWriter, r *http.Request, threadID string, runID string, stepID string, params openai.XListRunStepEventsParams) {
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
	if stepID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewMustNotBeEmptyError("step_id").Error()))
		return
	}

	step := new(db.RunStep)
	if err := db.Get(s.db.WithContext(r.Context()).Where("run_id = ?", runID).Where("id = ?", stepID), step, stepID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(NewNotFoundError(step).Error()))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to get step: %v", err), InternalErrorType).Error()))
		return
	}

	if z.Dereference(params.Stream) {
		// Doesn't make sense to stream events for a run step that is in terminal state.
		if db.IsTerminal(step.Status) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Run step %s is in terminal state: %s", stepID, step.Status), InvalidRequestErrorType).Error()))
			return
		}

		waitForAndStreamResponse[*db.RunStepEvent](r.Context(), w, s.db.WithContext(r.Context()), stepID, z.Dereference(params.Index))
		return
	}

	var objs []db.RunStepEvent
	if err := list(s.db.WithContext(r.Context()).Where("run_id = ?", runID).Where("request_id = ?", stepID).Order("response_idx asc"), &objs); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError("Failed to list objects.", InternalErrorType).Error()))
		return
	}

	publicObjs := make([]any, 0, len(objs))
	for _, o := range objs {
		// Any event that has Done == true is just a marker and doesn't actually contain an event.
		if !o.Done {
			o.SetID(stepID)
			publicObjs = append(publicObjs, o.ToPublic())
		}
	}

	respondWithList(w, publicObjs, false, -1, "", "")
}

func (s *Server) XRunTool(w http.ResponseWriter, r *http.Request) {
	runToolInput := new(openai.XRunToolRequest)
	if err := readObjectFromRequest(r, runToolInput); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if err := validateToolEnvVars(runToolInput.EnvVars); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	//nolint:govet
	runTool := new(db.RunToolObject)
	if err := runTool.FromPublic(runToolInput); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to create run tool: %v", err), InternalErrorType).Error()))
		return
	}

	if err := db.Create(s.db.WithContext(r.Context()), runTool); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to create run tool: %v", err), InternalErrorType).Error()))
		return
	}

	s.triggers.RunTool.Kick(runTool.ID)

	waitForAndStreamResponse[*db.RunStepEvent](r.Context(), w, s.db.WithContext(r.Context()), runTool.ID, 0)
}

func (s *Server) XInspectTool(w http.ResponseWriter, r *http.Request) {
	inspectToolInput := new(openai.XInspectToolRequest)
	if err := readObjectFromRequest(r, inspectToolInput); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	if !strings.HasSuffix(inspectToolInput.Url, ".gpt") {
		inspectToolInput.Url = strings.TrimPrefix(strings.TrimPrefix(inspectToolInput.Url, "https://"), "http://")
	}

	prg, err := loader.Program(r.Context(), inspectToolInput.Url, inspectToolInput.Subtool)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(NewAPIError(fmt.Sprintf("Failed to load program: %v", err), InternalErrorType).Error()))
		return
	}

	writeObjectToResponse(w, prg)
}
