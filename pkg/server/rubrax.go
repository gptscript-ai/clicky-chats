package server

import (
	"errors"
	"net/http"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"gorm.io/gorm"
)

func (s *Server) ListTools(w http.ResponseWriter, r *http.Request, params openai.ListToolsParams) {
	gormDB, limit, err := processAssistantsAPIListParams(s.db.WithContext(r.Context()), new(db.Tool), params.Limit, params.Before, params.After, params.Order)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	}

	listAndRespond[*db.Tool](gormDB, w, limit)
}

func (s *Server) CreateTool(w http.ResponseWriter, r *http.Request) {
	createToolRequest := new(openai.XCreateToolRequest)
	err := readObjectFromRequest(r, createToolRequest)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	//nolint:govet
	tool := &db.Tool{
		db.Base{},
		createToolRequest.Name,
		createToolRequest.Description,
		createToolRequest.Contents,
		createToolRequest.Url,
		createToolRequest.Subtool,
		nil,
	}

	tool.Program, err = toolToProgram(r.Context(), tool)
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

func (s *Server) DeleteTool(w http.ResponseWriter, r *http.Request, toolID string) {
	//nolint:govet
	deleteAndRespond[*db.Tool](s.db.WithContext(r.Context()), w, toolID, openai.XDeleteToolResponse{
		true,
		toolID,
		openai.ToolDeleted,
	})
}

func (s *Server) GetTool(w http.ResponseWriter, r *http.Request, toolID string) {
	getAndRespond(s.db.WithContext(r.Context()), w, new(db.Tool), toolID)
}

func (s *Server) ModifyTool(w http.ResponseWriter, r *http.Request, toolID string) {
	modifyToolRequest := new(openai.XModifyToolRequest)
	err := readObjectFromRequest(r, modifyToolRequest)
	if err != nil {
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

		existingTool.Name = modifyToolRequest.Name
		if modifyToolRequest.Description != nil && *modifyToolRequest.Description != z.Dereference(existingTool.Description) {
			existingTool.Description = modifyToolRequest.Description
		}

		retool := z.Dereference(modifyToolRequest.Retool)
		if newURL := modifyToolRequest.Url; z.Dereference(newURL) != z.Dereference(existingTool.URL) {
			retool = true
			existingTool.URL = newURL
		} else if newContents := modifyToolRequest.Contents; z.Dereference(newContents) != z.Dereference(existingTool.Contents) {
			retool = true
			existingTool.Contents = newContents
		}

		if retool {
			existingTool.Program, err = toolToProgram(r.Context(), existingTool)
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
