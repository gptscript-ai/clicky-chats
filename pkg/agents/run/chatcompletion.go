package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/clicky-chats/pkg/tools"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func prepareChatCompletionRequest(ctx context.Context, builtInFunctionDefinitions map[string]*openai.FunctionObject, run *db.Run, assistant *db.Assistant, tools []db.Tool, messages []db.Message, runSteps []db.RunStep) (*db.CreateChatCompletionRequest, error) {
	chatMessages := make([]openai.ChatCompletionRequestMessage, 0, len(messages))

	if run.Instructions != "" {
		m := new(openai.ChatCompletionRequestMessage)
		if err := m.FromChatCompletionRequestSystemMessage(openai.ChatCompletionRequestSystemMessage{
			Role:    openai.ChatCompletionRequestSystemMessageRoleSystem,
			Content: run.Instructions,
		}); err != nil {
			return nil, err
		}

		chatMessages = append(chatMessages, *m)
	} else if assistantInstructions := z.Dereference(assistant.Instructions); assistantInstructions != "" {
		m := new(openai.ChatCompletionRequestMessage)
		if err := m.FromChatCompletionRequestSystemMessage(openai.ChatCompletionRequestSystemMessage{
			Role:    openai.ChatCompletionRequestSystemMessageRoleSystem,
			Content: assistantInstructions,
		}); err != nil {
			return nil, err
		}

		chatMessages = append(chatMessages, *m)
	}

	for _, message := range messages {
		m, err := createChatMessageFromThreadMessage(&message)
		if err != nil {
			return nil, err
		}

		chatMessages = append(chatMessages, *m)
	}
	for _, runStep := range runSteps {
		messages, err := createChatMessageFromToolOutput(runStep.StepDetails.Data())
		if err != nil {
			return nil, err
		}
		chatMessages = append(chatMessages, messages...)
	}

	toolDefinitions := make(map[string]*openai.FunctionObject, len(tools))
	for _, tool := range tools {
		prg, err := loader.ProgramFromSource(ctx, string(tool.Program), "")
		if err != nil {
			return nil, fmt.Errorf("failed to initialize program %q: %w", tool.ID, err)
		}

		toolDefinitions[tool.ID], err = programToFunction(&prg, tool.ID)
		if err != nil {
			return nil, err
		}
	}

	chatCompletionTools, err := assistant.ToolsToChatCompletionTools(builtInFunctionDefinitions, toolDefinitions)
	if err != nil {
		return nil, err
	}

	return &db.CreateChatCompletionRequest{
		Stream:      z.Pointer(true),
		Messages:    chatMessages,
		Model:       assistant.Model,
		Temperature: z.Pointer[float32](0.1),
		TopP:        z.Pointer[float32](0.95),
		Tools:       chatCompletionTools,
	}, nil
}

func createChatMessageFromThreadMessage(threadMessage *db.Message) (*openai.ChatCompletionRequestMessage, error) {
	m := new(openai.ChatCompletionRequestMessage)
	sb := strings.Builder{}
	for _, c := range threadMessage.Content {
		if text, err := c.AsMessageContentTextObject(); err == nil {
			sb.WriteString(text.Text.Value)
			sb.WriteString("\n")
		}
	}

	switch threadMessage.Role {
	case string(openai.ChatCompletionRequestAssistantMessageRoleAssistant):
		return m, m.FromChatCompletionRequestAssistantMessage(openai.ChatCompletionRequestAssistantMessage{
			Role:    openai.ChatCompletionRequestAssistantMessageRoleAssistant,
			Content: z.Pointer(sb.String()),
		})
	case string(openai.ChatCompletionRequestUserMessageRoleUser):
		userMessageContent := new(openai.ChatCompletionRequestUserMessage_Content)
		if err := userMessageContent.FromChatCompletionRequestUserMessageContent0(sb.String()); err != nil {
			return nil, err
		}

		return m, m.FromChatCompletionRequestUserMessage(openai.ChatCompletionRequestUserMessage{
			Role:    openai.ChatCompletionRequestUserMessageRoleUser,
			Content: *userMessageContent,
		})
	}

	return nil, fmt.Errorf("unknown message role: %s", threadMessage.Role)
}

func createChatMessageFromToolOutput(toolOutput openai.RunStepObject_StepDetails) ([]openai.ChatCompletionRequestMessage, error) {
	toolCall, err := toolOutput.AsRunStepDetailsToolCallsObject()
	if err != nil {
		return nil, err
	}

	toolCalls := make(openai.ChatCompletionMessageToolCalls, 0, len(toolCall.ToolCalls))
	messages := make([]openai.ChatCompletionRequestMessage, 1, len(toolCall.ToolCalls)+1)
	am := new(openai.ChatCompletionRequestMessage)
	for _, output := range toolCall.ToolCalls {
		toolInfo, err := db.GetOutputForRunStepToolCall(&output)
		if err != nil {
			return nil, err
		}

		m := new(openai.ChatCompletionRequestMessage)
		if err = m.FromChatCompletionRequestToolMessage(openai.ChatCompletionRequestToolMessage{
			Role:       openai.ChatCompletionRequestToolMessageRoleTool,
			Content:    toolInfo.Output,
			ToolCallId: toolInfo.ID,
		}); err != nil {
			return nil, err
		}
		messages = append(messages, *m)

		toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCall{
			Function: struct {
				Arguments string `json:"arguments"`
				Name      string `json:"name"`
			}{
				Arguments: toolInfo.Arguments,
				Name:      toolInfo.Name,
			},
			Id:   toolInfo.ID,
			Type: openai.ChatCompletionMessageToolCallTypeFunction,
		})
	}

	if err = am.FromChatCompletionRequestAssistantMessage(openai.ChatCompletionRequestAssistantMessage{
		Role:      openai.ChatCompletionRequestAssistantMessageRoleAssistant,
		ToolCalls: &toolCalls,
	}); err != nil {
		return nil, err
	}

	messages[0] = *am
	return messages, nil
}

// compileChunksAndApplyStatuses compiles the chat completion chunks into a run step and a message, if necessary.
// The parameters are passed in should have all ID values set except for the primary ID, which will be set on creation.
func compileChunksAndApplyStatuses(ctx context.Context, l *slog.Logger, gdb *gorm.DB, run *db.Run, stream <-chan db.ChatCompletionResponseChunk) error {
	var (
		runStep = &db.RunStep{
			AssistantID: run.AssistantID,
			RunID:       run.ID,
			ThreadID:    run.ThreadID,
		}
		message = &db.Message{
			Role:        string(openai.MessageObjectRoleAssistant),
			AssistantID: &run.AssistantID,
			ThreadID:    run.ThreadID,
			RunID:       &run.ID,
		}
	)

	statusCode, toolCalls, err := processAllChunks(ctx, gdb, run, runStep, message, stream)
	return finalizeStatuses(gdb, l, run, runStep, toolCalls, message, statusCode, err)
}

func processAllChunks(ctx context.Context, gdb *gorm.DB, run *db.Run, runStep *db.RunStep, message *db.Message, stream <-chan db.ChatCompletionResponseChunk) (int, []db.GenericToolCallInfo, error) {
	defer func() {
		go func() {
			//nolint:revive
			for range stream {
			}
		}()
	}()

	var (
		messageContent    string
		responseIsMessage bool
		toolCalls         []db.GenericToolCallInfo
	)
	for {
		select {
		case <-ctx.Done():
			return 0, toolCalls, ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				return http.StatusOK, toolCalls, nil
			}

			if chunk.Error != nil {
				statusCode := chunk.StatusCode
				if statusCode < 400 {
					statusCode = http.StatusInternalServerError
				}

				return statusCode, toolCalls, fmt.Errorf("unexpected chat completion response: %s", z.Dereference(chunk.Error))
			}

			// These chat completions should only have one choice.
			responseIsMessage = responseIsMessage || len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Data().Content != nil
			if !responseIsMessage {
				// If the response is not a message, then the response is a tool call.
				// Merge the chunk into the run step.
				runStepDelta, err := runStep.Merge(&toolCalls, chunk)
				if err != nil {
					return http.StatusInternalServerError, toolCalls, err
				}

				if runStepDelta != nil {
					// When we get a run step delta, then create it in the database for the event stream.
					runStepDelta.ID = runStep.ID
					if err = gdb.Transaction(func(tx *gorm.DB) error {
						if runStep.ID == "" {
							// The run step hasn't been created yet, so create it.
							runStep.Type = string(openai.RunStepObjectTypeToolCalls)
							if err = createRunStep(tx, run, runStep); err != nil {
								return err
							}
						} else {
							// Update the run step to have the most recent step details.
							if err = tx.Model(runStep).Where("id = ?", runStep.ID).Update("step_details", runStep.StepDetails).Error; err != nil {
								return err
							}
						}

						// Create the run step delta event.
						run.EventIndex++
						runEvent := &db.RunEvent{
							JobResponse: db.JobResponse{
								RequestID: run.ID,
							},
							EventName:    db.ThreadRunStepDeltaEvent,
							ResponseIdx:  run.EventIndex,
							RunStepDelta: datatypes.NewJSONType(runStepDelta),
						}

						if err = db.Create(tx, runEvent); err != nil {
							return err
						}

						return tx.Model(run).Clauses(clause.Returning{}).Where("id = ?", run.ID).Update("event_index", run.EventIndex).Error
					}); err != nil {
						return http.StatusInternalServerError, toolCalls, err
					}
				}
			} else if newContent := z.Dereference(chunk.Choices[0].Delta.Data().Content); newContent != "" {
				// In this case, the chat completion response is a message.
				messageContent += newContent
				if err := gdb.Transaction(func(tx *gorm.DB) error {
					if message.ID == "" {
						// The message hasn't been created yet, so create it.
						db.SetNewID(message)
						runStep.Type = string(openai.RunStepObjectTypeMessageCreation)

						stepDetails := new(openai.RunStepObject_StepDetails)
						//nolint:govet
						if err := stepDetails.FromRunStepDetailsMessageCreationObject(openai.RunStepDetailsMessageCreationObject{
							//nolint:revive
							struct {
								MessageId string `json:"message_id"`
							}{
								message.ID,
							},
							openai.RunStepDetailsMessageCreationObjectTypeMessageCreation,
						}); err != nil {
							return err
						}

						runStep.StepDetails = datatypes.NewJSONType(*stepDetails)
						// First create the run step.
						if err := createRunStep(tx, run, runStep); err != nil {
							return err
						}

						// Then create the message.
						if err := createMessageObject(tx, run, message); err != nil {
							return err
						}
					} else {
						if err := message.WithTextContent(messageContent); err != nil {
							return err
						}

						// Update the message with the current content.
						if err := tx.Model(message).Where("id = ?", message.ID).Update("content", message.Content).Error; err != nil {
							return err
						}
					}

					messageDelta, err := db.NewMessageDeltaWithText(chunk.Choices[0].Index, message.ID, newContent)
					if err != nil {
						return err
					}

					// Create the message delta event.
					run.EventIndex++
					runEvent := &db.RunEvent{
						JobResponse: db.JobResponse{
							RequestID: run.ID,
						},
						EventName:    db.ThreadMessageDeltaEvent,
						ResponseIdx:  run.EventIndex,
						MessageDelta: datatypes.NewJSONType(messageDelta),
					}
					if err = db.Create(tx, runEvent); err != nil {
						return err
					}

					return tx.Model(run).Clauses(clause.Returning{}).Where("id = ?", run.ID).Update("event_index", run.EventIndex).Error
				}); err != nil {
					return http.StatusInternalServerError, toolCalls, err
				}
			}
		}
	}
}

func createRunStep(gdb *gorm.DB, run *db.Run, runStep *db.RunStep) error {
	// Create the runStep and send the events.
	// Do this manually instead of calling db.Create so we can return the object.
	db.SetNewID(runStep)
	runStep.CreatedAt = int(time.Now().Unix())
	if err := gdb.Model(runStep).Clauses(clause.Returning{}).Create(runStep).Error; err != nil {
		return err
	}
	// Create an event that says that the run step has been created.
	run.EventIndex++
	runEvent := &db.RunEvent{
		JobResponse: db.JobResponse{
			RequestID: run.ID,
		},
		EventName:   db.ThreadRunStepCreatedEvent,
		ResponseIdx: run.EventIndex,
		RunStep:     datatypes.NewJSONType(runStep),
	}
	if err := db.Create(gdb, runEvent); err != nil {
		return err
	}

	if err := gdb.Model(runStep).Clauses(clause.Returning{}).Where("id = ?", runStep.ID).Update("status", string(openai.InProgress)).Error; err != nil {
		return err
	}
	// Create an event that says that the run step is in progress.
	run.EventIndex++
	runEvent = &db.RunEvent{
		JobResponse: db.JobResponse{
			RequestID: run.ID,
		},
		EventName:   db.ThreadRunStepInProgressEvent,
		ResponseIdx: run.EventIndex,
		RunStep:     datatypes.NewJSONType(runStep),
	}

	return db.Create(gdb, runEvent)
}

func createMessageObject(gdb *gorm.DB, run *db.Run, message *db.Message) error {
	// Create the message and send the events.
	// Do this manually instead of calling db.Create so we can return the object.
	// Also, the message object should already have its ID set.
	message.CreatedAt = int(time.Now().Unix())
	if err := gdb.Model(message).Clauses(clause.Returning{}).Create(message).Error; err != nil {
		return err
	}

	run.EventIndex++
	runEvent := &db.RunEvent{
		JobResponse: db.JobResponse{
			RequestID: run.ID,
		},
		EventName:   db.ThreadMessageCreatedEvent,
		ResponseIdx: run.EventIndex,
		Message:     datatypes.NewJSONType(message),
	}
	if err := db.Create(gdb, runEvent); err != nil {
		return err
	}

	// Put the message into the in_progress status.
	if err := gdb.Model(message).Clauses(clause.Returning{}).Where("id = ?", message.ID).Update("status", z.Pointer(string(openai.ExtendedMessageObjectStatusInProgress))).Error; err != nil {
		return err
	}

	run.EventIndex++
	runEvent = &db.RunEvent{
		JobResponse: db.JobResponse{
			RequestID: run.ID,
		},
		EventName:   db.ThreadMessageInProgressEvent,
		ResponseIdx: run.EventIndex,
		Message:     datatypes.NewJSONType(message),
	}

	return db.Create(gdb, runEvent)
}

// finalizeStatuses updates the various objects statuses based on the chat completion response.
// If the chat completion response asks for a tool to be called, then the run object needs to be put in the appropriate status.
// If the chat completion response just has a message, then the message should be completed and the run should be put in the completed status.
// If anything errors, then the run and run step should be put in a failed state. The message should be put in the incomplete status.
// If the run reaches a terminal state, then unlock the thread.
func finalizeStatuses(gdb *gorm.DB, l *slog.Logger, run *db.Run, runStep *db.RunStep, toolCalls []db.GenericToolCallInfo, message *db.Message, statusCode int, err error) error {
	l.Debug("Made chat completion request")
	// If the chat completion request failed, then we should put the run in a failed state.
	// If both of these IDs ar blank, then they were never created, which means we took no action on this run.
	if statusCode >= 400 {
		errStr := fmt.Errorf("unexpected status code: %d, error: %w", statusCode, err)
		errType := openai.RunObjectLastErrorCodeServerError

		if statusCode == http.StatusTooManyRequests {
			err = fmt.Errorf("too many requests: %d", statusCode)
			errType = openai.RunObjectLastErrorCodeRateLimitExceeded
		}
		l.Error("Chat completion request failed, failing run", "status_code", statusCode, "err", err)
		return gdb.Transaction(func(tx *gorm.DB) error {
			return failRun(tx, run, errStr, errType)
		})
	}

	newPublicStatus, newSystemStatus, statusErr := determineNewStatuses(gdb, run, runStep, toolCalls, message)
	if err != nil || statusErr != nil {
		err = errors.Join(err, statusErr)
		// On error, ensure tha the run step and message are marked as failed.
		txErr := gdb.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(runStep).Clauses(clause.Returning{}).Where("id = ?", runStep.ID).Updates(
				map[string]any{
					"status":    string(openai.ExtendedRunStepObjectStatusFailed),
					"failed_at": z.Pointer(int(time.Now().Unix())),
				},
			).Error; err != nil {
				return err
			}

			// Emit event for failure
			run.EventIndex++
			runEvent := &db.RunEvent{
				JobResponse: db.JobResponse{
					RequestID: run.ID,
				},
				RunStep:     datatypes.NewJSONType(runStep),
				EventName:   db.ThreadRunStepFailedEvent,
				ResponseIdx: run.EventIndex,
			}
			if err := db.Create(tx, runEvent); err != nil {
				return err
			}

			if message.ID != "" {
				if err := tx.Model(message).Clauses(clause.Returning{}).Where("id = ?", message.ID).Updates(
					map[string]any{
						"status":        string(openai.ExtendedMessageObjectStatusIncomplete),
						"incomplete_at": z.Pointer(int(time.Now().Unix())),
					},
				).Error; err != nil {
					return err
				}

				// Emit event for failure
				run.EventIndex++
				runEvent = &db.RunEvent{
					JobResponse: db.JobResponse{
						RequestID: run.ID,
					},
					Message:     datatypes.NewJSONType(message),
					EventName:   db.ThreadMessageIncompleteEvent,
					ResponseIdx: run.EventIndex,
				}
				if err := db.Create(tx, runEvent); err != nil {
					return err
				}
			}

			return failRun(tx, run, err, openai.RunObjectLastErrorCodeServerError)
		})

		return errors.Join(err, txErr)
	}

	// On success, ensure that the message and run step are marked as completed.
	return gdb.Transaction(func(tx *gorm.DB) error {
		var (
			runEvents   []*db.RunEvent
			completedAt *int
		)

		switch newPublicStatus {
		case openai.RunObjectStatusFailed, openai.RunObjectStatusCancelled, openai.RunObjectStatusExpired, openai.RunObjectStatusCompleted:
			if err = tx.Model(new(db.Thread)).Where("id = ?", run.ThreadID).Update("locked_by_run_id", nil).Error; err != nil {
				return err
			}

			completedAt = z.Pointer(int(time.Now().Unix()))

		case openai.RunObjectStatusRequiresAction:
			runEvents = append(runEvents, &db.RunEvent{
				JobResponse: db.JobResponse{
					RequestID: run.ID,
				},
				EventName: db.ThreadRunRequiresActionEvent,
				Run:       datatypes.NewJSONType(run),
			})
		}

		if message.ID != "" {
			if err := tx.Model(message).Where("id = ?", message.ID).Update("status", string(openai.ExtendedMessageObjectStatusCompleted)).Error; err != nil {
				return err
			}

			// Emit event for success
			run.EventIndex++
			runEvents = append(runEvents, &db.RunEvent{
				JobResponse: db.JobResponse{
					RequestID: run.ID,
				},
				Message:     datatypes.NewJSONType(message),
				EventName:   db.ThreadMessageCompletedEvent,
				ResponseIdx: run.EventIndex,
			})

			if err := tx.Model(runStep).Where("id = ?", runStep.ID).Update("status", string(openai.RunObjectStatusCompleted)).Error; err != nil {
				return err
			}

			// Emit event for success
			run.EventIndex++
			runEvents = append(runEvents, &db.RunEvent{
				JobResponse: db.JobResponse{
					RequestID: run.ID,
				},
				RunStep:     datatypes.NewJSONType(runStep),
				EventName:   db.ThreadRunStepCompletedEvent,
				ResponseIdx: run.EventIndex,
			})
		}

		if completedAt != nil {
			// This has to be the last event in the list so that the client will get all events in the correct order.
			runEvents = append(runEvents, &db.RunEvent{
				JobResponse: db.JobResponse{
					RequestID: run.ID,
				},
				EventName: db.ThreadRunCompletedEvent,
				Run:       datatypes.NewJSONType(run),
			})
			runEvents = append(runEvents, &db.RunEvent{
				JobResponse: db.JobResponse{
					RequestID: run.ID,
					Done:      true,
				},
			})
		}

		if err = tx.Model(run).Where("id = ?", run.ID).Updates(map[string]any{
			"event_index":     run.EventIndex,
			"completed_at":    completedAt,
			"status":          newPublicStatus,
			"usage":           run.Usage,
			"required_action": run.RequiredAction,
			"system_status":   newSystemStatus,
		}).Error; err != nil {
			return err
		}

		for _, re := range runEvents {
			run.EventIndex++
			re.ResponseIdx = run.EventIndex
			if err := db.Create(tx, re); err != nil {
				return err
			}
		}

		return nil
	})
}

func determineNewStatuses(gdb *gorm.DB, run *db.Run, runStep *db.RunStep, toolCalls []db.GenericToolCallInfo, message *db.Message) (openai.RunObjectStatus, *string, error) {
	if len(toolCalls) == 0 {
		if message.ID == "" {
			// No tool calls and no message means something went wrong.
			return openai.RunObjectStatusFailed, nil, fmt.Errorf("no action taken, the run has stalled")
		}
		// No tool calls means the run step is completed.
		return openai.RunObjectStatusCompleted, nil, nil
	}

	var (
		newSystemStatus    *string
		retrievalArguments string
		newPublicStatus    = openai.RunObjectStatusInProgress
		functionCalls      = make([]openai.RunToolCallObject, 0)
	)
	for _, tc := range toolCalls {
		if strings.HasPrefix(tc.Name, tools.GPTScriptToolNamePrefix) {
			if funcName := strings.TrimPrefix(tc.Name, tools.GPTScriptToolNamePrefix); funcName == string(openai.Retrieval) {
				retrievalArguments = tc.Arguments
			}
			newSystemStatus = z.Pointer(string(openai.RunObjectStatusRequiresAction))
			continue
		}

		//nolint:govet
		functionCalls = append(functionCalls, openai.RunToolCallObject{
			struct {
				Arguments string `json:"arguments"`
				Name      string `json:"name"`
			}{
				Arguments: tc.Arguments,
				Name:      tc.Name,
			},
			tc.ID,
			openai.RunToolCallObjectTypeFunction,
		})
		newPublicStatus = openai.RunObjectStatusRequiresAction
	}

	err := gdb.Transaction(func(tx *gorm.DB) error {
		if z.Dereference(newSystemStatus) == string(openai.RunObjectStatusRequiresAction) {
			// Update the run step to be processed by the step runner.
			if err := tx.Model(runStep).Updates(
				map[string]any{
					"runner_type":         tools.GPTScriptRunnerType,
					"retrieval_arguments": retrievalArguments,
				}).Error; err != nil {
				return err
			}
		}

		if len(functionCalls) > 0 {
			run.RequiredAction = datatypes.NewJSONType(&db.RunRequiredAction{
				SubmitToolOutputs: functionCalls,
				Type:              openai.RunObjectRequiredActionTypeSubmitToolOutputs,
			})
		}

		return tx.Model(run).Update("required_action", run.RequiredAction).Error
	})

	return newPublicStatus, newSystemStatus, err
}
