package tools

const (
	GPTScriptRunnerType     = "gptscript"
	SkipLoadingTool         = "<skip>"
	GPTScriptToolNamePrefix = "gptscript_"
)

var builtInFunctionNameToDefinition = map[string]ToolDefinition{
	"internet_search":  {Link: "github.com/gptscript-ai/question-answerer/duckduckgo", Commit: "091e15cf90c34062ec189abefd84b92150c6725f"},
	"site_browsing":    {Link: "github.com/gptscript-ai/browse-web-page", Commit: "9656ac56b96c94fef24e30dc39482a24e0af0cb7"},
	"code_interpreter": {Link: "github.com/gptscript-ai/code-interpreter", Commit: "b784f26c82e1ea55fe8ead4f2d38b5ff2651bca7"},
	"retrieval":        {Link: "github.com/gptscript-ai/knowledge-retrieval-api", Commit: "30235686c44f046ebf5aa50a12d921878bb6503d"},
}

type ToolDefinition struct {
	Link, Commit, SubTool string
}

func GPTScriptDefinitions() map[string]ToolDefinition {
	return builtInFunctionNameToDefinition
}
