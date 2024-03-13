package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/agents"
	"github.com/gptscript-ai/clicky-chats/pkg/db"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	"github.com/gptscript-ai/gptscript/pkg/loader"
	"github.com/gptscript-ai/gptscript/pkg/types"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	minPollingInterval  = time.Second
	minRequestRetention = 5 * time.Minute
)

type Config struct {
	PollingInterval, RetentionPeriod time.Duration
	APIURL, APIKey, AgentID          string
}

func Start(ctx context.Context, gdb *db.DB, cfg Config) error {
	a, err := newAgent(gdb, cfg)
	if err != nil {
		return err
	}

	a.builtInToolDefinitions, err = populateTools(ctx)
	if err != nil {
		return err
	}

	a.Start(ctx)

	return nil
}

type agent struct {
	pollingInterval, retentionPeriod time.Duration
	id, apiKey, url                  string
	client                           *http.Client
	db                               *db.DB
	builtInToolDefinitions           map[string]*openai.FunctionObject
}

func newAgent(db *db.DB, cfg Config) (*agent, error) {
	if cfg.PollingInterval < minPollingInterval {
		return nil, fmt.Errorf("polling interval must be at least %s", minPollingInterval)
	}
	if cfg.RetentionPeriod < minRequestRetention {
		return nil, fmt.Errorf("request retention must be at least %s", minRequestRetention)
	}

	return &agent{
		pollingInterval: cfg.PollingInterval,
		retentionPeriod: cfg.RetentionPeriod,
		client:          http.DefaultClient,
		apiKey:          cfg.APIKey,
		db:              db,
		id:              cfg.AgentID,
		url:             cfg.APIURL,
	}, nil
}

func (a *agent) Start(ctx context.Context) {
	// Start the "job runner"
	go func() {
		for {
			if err := a.run(ctx); err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					slog.Error("failed run iteration", "err", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(a.pollingInterval):
				}
			}
		}
	}()

	// Start cleanup
	go func() {
		cleanupInterval := a.retentionPeriod / 2
		for {
			slog.Debug("Looking for completed runs")

			// Look for a new chat completion request and claim it.
			var runs []db.Run
			if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				// TODO(thedadams): Under which circumstances should we clean up old runs? This currently does nothing.
				if err := tx.Model(new(db.Run)).Where("id IS NULL").Order("created_at desc").Find(&runs).Error; err != nil {
					return err
				}
				if len(runs) == 0 {
					return nil
				}

				runIDs := make([]string, 0, len(runs))
				for _, run := range runs {
					runIDs = append(runIDs, run.ID)
				}

				if err := tx.Delete(new(db.RunStep), "run_id IN ?", runIDs).Error; err != nil {
					return err
				}

				return tx.Delete(runs).Error
			}); err != nil {
				slog.Error("Failed to cleanup run completions", "err", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(cleanupInterval):
			}
		}
	}()
}

func (a *agent) run(ctx context.Context) error {
	slog.Debug("Checking for a run")
	// Look for a new run and claim it. Also, query for the other objects we need.
	run, assistant, messages, runSteps, tools := new(db.Run), new(db.Assistant), make([]db.Message, 0), make([]db.RunStep, 0), make([]db.Tool, 0)
	err := a.db.WithContext(ctx).Model(run).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("claimed_by IS NULL").Or("claimed_by = ? AND status = ? AND system_status IS NULL", a.id, openai.RunObjectStatusQueued).Order("created_at desc").First(run).Error; err != nil {
			return err
		}

		thread := new(db.Thread)
		if err := tx.Model(new(db.Thread)).Where("id = ?", run.ThreadID).First(thread).Error; err != nil {
			return err
		}

		// If the thread is locked by another run, then return an error.
		if thread.LockedByRunID != run.ID {
			return fmt.Errorf("thread %s found to be locked by %s while processing run %s", run.ThreadID, thread.LockedByRunID, run.ID)
		}

		if err := tx.Model(assistant).Where("id = ?", run.AssistantID).First(assistant).Error; err != nil {
			return err
		}

		if err := tx.Model(new(db.Tool)).Where("id IN ?", []string(assistant.GPTScriptTools)).Find(&tools).Error; err != nil {
			return err
		}

		if err := tx.Model(new(db.Message)).Where("thread_id = ?", run.ThreadID).Where("created_at <= ?", run.CreatedAt).Order("created_at asc").Find(&messages).Error; err != nil {
			return err
		}

		if err := tx.Model(new(db.RunStep)).Where("run_id = ?", run.ID).Where("type = ?", openai.RunStepObjectTypeToolCalls).Where("created_at >= ?", run.CreatedAt).Order("created_at asc").Find(&runSteps).Error; err != nil {
			return err
		}

		startedAt := run.StartedAt
		if startedAt == nil {
			startedAt = z.Pointer(int(time.Now().Unix()))
		}

		if err := tx.Model(run).Where("id = ?", run.ID).Updates(map[string]interface{}{"claimed_by": a.id, "status": openai.RunObjectStatusInProgress, "started_at": startedAt}).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to get run: %w", err)
		}
		return err
	}

	runID := run.ID
	l := slog.With("type", "run", "id", runID)

	defer func() {
		if err != nil {
			failRun(l, a.db.WithContext(ctx), run, err, openai.RunObjectLastErrorCodeServerError)
		}
	}()

	l.Debug("Found run", "run", run)
	cc, err := prepareChatCompletionRequest(ctx, a.builtInToolDefinitions, run, assistant, tools, messages, runSteps)
	if err != nil {
		l.Error("Failed to prepare chat completion request", "err", err)
		return err
	}

	ccr, err := agents.MakeChatCompletionRequest(ctx, l, a.client, a.url, a.apiKey, cc)
	if err != nil {
		l.Error("Failed to make chat completion request from run", "err", err)
		return err
	}

	l.Debug("Made chat completion request", "status_code", ccr.StatusCode)

	// If the chat completion response asks for a tool to be called, then the run object needs to be put in the appropriate status.
	// If the chat completion response just has a message, then a message should be added to the thread and the run should be put in the appropriate status.
	// In the above two cases, a run step object should be created and the usage of the run updated.
	// If anything errors, then the run should be put in a failed state.
	// If the run reaches a terminal state, then unlock the thread.

	var (
		terminalState   bool
		newPublicStatus openai.RunObjectStatus
		newSystemStatus *string
		newRunSteps     []db.RunStep
		newMessage      *db.Message
	)

	// If the chat completion request failed, then we should put the run in a failed state.
	if ccr.StatusCode >= 400 || len(ccr.Choices) == 0 {
		if ccr.StatusCode == http.StatusTooManyRequests {
			l.Error("Chat completion request had too many requests, failing run", "status_code", ccr.StatusCode)
			failRun(l, a.db.WithContext(ctx), run, fmt.Errorf(z.Dereference(ccr.Error)), openai.RunObjectLastErrorCodeRateLimitExceeded)
		} else {
			l.Error("Chat completion request had unexpected status code, failing run", "status_code", ccr.StatusCode, "choices", len(ccr.Choices))
			failRun(l, a.db.WithContext(ctx), run, fmt.Errorf("unexpected status code: %d", ccr.StatusCode), openai.RunObjectLastErrorCodeServerError)
		}
		return nil
	}

	// Act on the response from the chat completion request.
	if ccr.Choices[0].Message.Data().ToolCalls != nil {
		newPublicStatus, newSystemStatus, newRunSteps, err = objectsForToolStep(run, ccr)
	} else if ccr.Choices[0].Message.Data().Content != nil {
		newPublicStatus = openai.RunObjectStatusCompleted
		terminalState = true
		newRunSteps, newMessage, err = objectsForMessageStep(run, ccr)
	} else {
		err = fmt.Errorf("unexpected response from chat completion request: %+v", ccr)
	}
	// Handle possible errors from above if-else blocks.
	if err != nil {
		l.Error("Failed to create run objects", "err", err)
		return err
	}

	// Create and update the objects in the database.
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var completedAt *int
		if terminalState {
			if err := tx.Model(new(db.Thread)).Where("id = ?", run.ThreadID).Update("locked_by_run_id", nil).Error; err != nil {
				return err
			}

			completedAt = z.Pointer(int(time.Now().Unix()))
		}

		if newMessage != nil {
			if err := db.Create(tx, newMessage); err != nil {
				return err
			}
		}

		for _, r := range newRunSteps {
			if err := db.Create(tx, &r); err != nil {
				return err
			}
		}

		return tx.Model(run).Where("id = ?", run.ID).Updates(map[string]any{
			"status":          newPublicStatus,
			"completed_at":    completedAt,
			"usage":           run.Usage,
			"required_action": run.RequiredAction,
			"system_status":   newSystemStatus,
		}).Error
	}); err != nil {
		l.Error("Failed to update and create objects for run", "err", err)
		return err
	}

	return nil
}

// populateTools returns the function definition used for chat completion from the provided link and subtool.
// The run agent will use these when making chat completion requests for runs.
func populateTools(ctx context.Context) (map[string]*openai.FunctionObject, error) {
	builtInToolDefinitions := make(map[string]*openai.FunctionObject, len(agents.GPTScriptDefinitions()))
	for toolName, toolDef := range agents.GPTScriptDefinitions() {
		if toolDef.Link == "" || toolDef.Link == agents.SkipLoadingTool {
			slog.Info("Skipping tool", "name", toolName)
			continue
		}

		prg, err := loader.Program(ctx, toolDef.Link, toolDef.SubTool)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize program %q: %w", toolName, err)
		}

		builtInToolDefinitions[toolName], err = programToFunction(&prg, toolName)
		if err != nil {
			return nil, err
		}
	}

	return builtInToolDefinitions, nil
}

func programToFunction(prg *types.Program, toolName string) (*openai.FunctionObject, error) {
	b, err := json.Marshal(prg.ToolSet[prg.EntryToolID].Parameters.Arguments)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parameters for tool %q: %w", toolName, err)
	}

	var fp *openai.FunctionParameters
	if err = json.Unmarshal(b, &fp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal parameters for tool %q: %w", toolName, err)
	}

	return &openai.FunctionObject{
		Name:        agents.GPTScriptToolNamePrefix + toolName,
		Description: z.Pointer(prg.ToolSet[prg.EntryToolID].Description),
		Parameters:  fp,
	}, nil
}

func failRun(l *slog.Logger, gdb *gorm.DB, run *db.Run, err error, errorCode openai.RunObjectLastErrorCode) {
	runError := &db.RunLastError{
		Code:    string(errorCode),
		Message: err.Error(),
	}
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(new(db.Run)).Where("id = ?", run.ID).Updates(map[string]interface{}{
			"status":        openai.RunObjectStatusFailed,
			"system_status": nil,
			"failed_at":     z.Pointer(int(time.Now().Unix())),
			"last_error":    datatypes.NewJSONType(runError),
			"usage":         run.Usage,
		}).Error; err != nil {
			return err
		}

		return tx.Model(new(db.Thread)).Where("id = ?", run.ThreadID).Update("locked_by_run_id", nil).Error
	}); err != nil {
		l.Error("Failed to update run", "err", err)
	}
}

func objectsForToolStep(run *db.Run, ccr *db.CreateChatCompletionResponse) (openai.RunObjectStatus, *string, []db.RunStep, error) {
	functionCalls := make([]openai.RunToolCallObject, 0, len(ccr.Choices))
	nonFunctionCalls := make([]openai.RunToolCallObject, 0, len(ccr.Choices))
	for _, choice := range ccr.Choices {
		if choice.Message.Data().ToolCalls != nil {
			for _, tc := range *choice.Message.Data().ToolCalls {
				nonFunctionType := strings.HasPrefix(tc.Function.Name, agents.GPTScriptToolNamePrefix)
				toolType := string(tc.Type)
				if tc.Function.Name == string(openai.AssistantToolsCodeTypeCodeInterpreter) {
					nonFunctionType = true
					toolType = string(openai.AssistantToolsCodeTypeCodeInterpreter)
				} else if tc.Function.Name == string(openai.AssistantToolsRetrievalTypeRetrieval) {
					nonFunctionType = true
					toolType = string(openai.AssistantToolsRetrievalTypeRetrieval)
				}
				if nonFunctionType {
					//nolint:govet
					nonFunctionCalls = append(nonFunctionCalls, openai.RunToolCallObject{
						tc.Function,
						tc.Id,
						openai.RunToolCallObjectType(toolType),
					})
				} else {
					//nolint:govet
					functionCalls = append(functionCalls, openai.RunToolCallObject{
						tc.Function,
						tc.Id,
						openai.RunToolCallObjectType(toolType),
					})
				}
			}
		}
	}

	functionCallDetails, err := db.RunStepDetailsFromRunRequiredActionToolCalls(functionCalls)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to convert step details: %w", err)
	}
	nonFunctionCallDetails, err := db.RunStepDetailsFromRunRequiredActionToolCalls(nonFunctionCalls)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to convert step details: %w", err)
	}

	run.RequiredAction = datatypes.NewJSONType(&db.RunRequiredAction{
		SubmitToolOutputs: functionCalls,
		Type:              openai.RunObjectRequiredActionTypeSubmitToolOutputs,
	})

	if run.Usage.Data() == nil {
		run.Usage = datatypes.NewJSONType(&openai.RunCompletionUsage{})
	}
	run.Usage.Data().TotalTokens += ccr.Usage.Data().TotalTokens
	run.Usage.Data().PromptTokens += ccr.Usage.Data().PromptTokens
	run.Usage.Data().CompletionTokens += ccr.Usage.Data().CompletionTokens

	var (
		runSteps        []db.RunStep
		newSystemStatus *string
		newPublicStatus openai.RunObjectStatus
	)
	if len(nonFunctionCalls) > 0 {
		runSteps = append(runSteps, db.RunStep{
			AssistantID: run.AssistantID,
			RunID:       run.ID,
			StepDetails: datatypes.NewJSONType(*nonFunctionCallDetails),
			Type:        string(openai.RunStepObjectTypeToolCalls),
			Status:      string(openai.RunStepObjectStatusInProgress),
			RunnerType:  z.Pointer(agents.GPTScriptRunnerType),
		})

		newSystemStatus = z.Pointer("requires_action")
		newPublicStatus = openai.RunObjectStatusInProgress
	}

	if len(functionCalls) > 0 {
		runSteps = append(runSteps, db.RunStep{
			AssistantID: run.AssistantID,
			RunID:       run.ID,
			StepDetails: datatypes.NewJSONType(*functionCallDetails),
			Type:        string(openai.RunStepObjectTypeToolCalls),
			Status:      string(openai.RunStepObjectStatusInProgress),
			RunnerType:  z.Pointer(agents.GPTScriptRunnerType),
		})
		newPublicStatus = openai.RunObjectStatusRequiresAction
	}

	return newPublicStatus, newSystemStatus, runSteps, nil
}

func objectsForMessageStep(run *db.Run, ccr *db.CreateChatCompletionResponse) ([]db.RunStep, *db.Message, error) {
	content, err := db.MessageContentFromString(*ccr.Choices[0].Message.Data().Content)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert message content: %w", err)
	}

	newMessage := &db.Message{
		Metadata: db.Metadata{
			Metadata: nil,
		},
		ThreadID:    run.ThreadID,
		Role:        string(openai.Assistant),
		Content:     []openai.MessageObject_Content_Item{*content},
		AssistantID: z.Pointer(run.AssistantID),
		RunID:       z.Pointer(run.ID),
		FileIDs:     nil,
	}

	stepDetails := openai.RunStepDetailsMessageCreationObject{
		//nolint:revive
		MessageCreation: struct {
			MessageId string `json:"message_id"`
		}{
			MessageId: newMessage.ID,
		},
		Type: openai.RunStepDetailsMessageCreationObjectTypeMessageCreation,
	}

	details := new(openai.RunStepObject_StepDetails)
	if err = details.FromRunStepDetailsMessageCreationObject(stepDetails); err != nil {
		return nil, nil, fmt.Errorf("failed to convert run step details: %w", err)
	}

	runStep := db.RunStep{
		AssistantID: run.AssistantID,
		RunID:       run.ID,
		StepDetails: datatypes.NewJSONType(*details),
		Type:        string(openai.RunStepObjectTypeMessageCreation),
		Usage: datatypes.NewJSONType(
			&openai.RunStepCompletionUsage{
				CompletionTokens: ccr.Usage.Data().CompletionTokens,
				TotalTokens:      ccr.Usage.Data().TotalTokens,
				PromptTokens:     ccr.Usage.Data().PromptTokens,
			},
		),
	}

	if run.Usage.Data() == nil {
		run.Usage = datatypes.NewJSONType[*openai.RunCompletionUsage](&openai.RunCompletionUsage{})
	}
	run.Usage.Data().TotalTokens += ccr.Usage.Data().TotalTokens
	run.Usage.Data().PromptTokens += ccr.Usage.Data().PromptTokens
	run.Usage.Data().CompletionTokens += ccr.Usage.Data().CompletionTokens

	return []db.RunStep{runStep}, newMessage, nil
}

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
		toolInfo, err := db.GetOutputForRunStepToolCall(output)
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
