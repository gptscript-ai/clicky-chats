package server

import (
	"fmt"
	"strings"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
)

const (
	InvalidRequestErrorType = "invalid_request_error"
	InternalErrorType       = "internal_error"
)

type APIError struct {
	Code    any     `json:"code,omitempty"`
	Message string  `json:"message"`
	Param   *string `json:"param,omitempty"`
	Type    string  `json:"type"`
}

func NewAPIError(message, errorType string) *APIError {
	return &APIError{
		Message: message,
		Type:    errorType,
	}
}

func NewNotFoundError(obj db.Storer) *APIError {
	return NewAPIError(
		fmt.Sprintf("No %s found with id '%s'.", strings.ToLower(strings.Split(fmt.Sprintf("%T", obj), ".")[1]), obj.GetID()),
		InvalidRequestErrorType,
	)
}

func NewMustNotBeEmptyError(param string) *APIError {
	return NewAPIError(fmt.Sprintf("Parameter %s must not be empty.", param), InvalidRequestErrorType)
}

func (e *APIError) Error() string {
	return e.String()
}

func (e *APIError) String() string {
	if e == nil {
		return ""
	}
	if e.Code == nil {
		e.Code = "null"
	}
	if e.Param == nil {
		e.Param = z.Pointer("null")
	} else {
		*e.Param = fmt.Sprintf("%q", *e.Param)
	}
	return fmt.Sprintf(`{"error":{"message":%q,"type":%q,"param":%s,"code":%v}}`, e.Type, e.Message, *e.Param, e.Code)
}
