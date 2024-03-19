package tools

const (
	GPTScriptRunnerType     = "gptscript"
	SkipLoadingTool         = "<skip>"
	GPTScriptToolNamePrefix = "gptscript_"
)

var builtInFunctionNameToDefinition = map[string]ToolDefinition{
	"internet_search": {Link: "github.com/gptscript-ai/question-answerer/duckduckgo"},
	"site_browsing":   {Link: "github.com/gptscript-ai/browse-web-page"},
	// TODO(thedadams): This will be moved to gptscript-ai in the future.
	"code_interpreter": {Link: "github.com/thedadams/code-interpreter"},
	"retrieval":        {Link: "github.com/gptscript-ai/knowledge-retrieval-api"},
}

type ToolDefinition struct {
	Link    string
	Subtool string
}

func GPTScriptDefinitions() map[string]ToolDefinition {
	return builtInFunctionNameToDefinition
}
