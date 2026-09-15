package importer

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"mvdan.cc/sh/v3/syntax"
)

// Tool results may contain either a string or a list of content blocks. Keep
// their text available to both transcript rendering and activity reconstruction.
func (c *ClaudeContent) UnmarshalJSON(data []byte) error {
	type plain ClaudeContent
	var raw struct {
		*plain
		Content json.RawMessage `json:"content"`
	}
	*c = ClaudeContent{}
	raw.plain = (*plain)(c)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Content) == 0 || string(raw.Content) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw.Content, &c.Content); err == nil {
		return nil
	}
	var blocks []struct{ Type, Text string }
	if err := json.Unmarshal(raw.Content, &blocks); err != nil {
		return err
	}
	var texts []string
	for _, block := range blocks {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	c.Content = strings.Join(texts, "\n")
	return nil
}

type claudeToolCall struct {
	content ClaudeContent
	model   string
}

type claudeToolResult struct {
	Type            string  `json:"type"`
	FilePath        string  `json:"filePath"`
	OriginalFile    *string `json:"originalFile"`
	StructuredPatch []struct {
		Lines []string `json:"lines"`
	} `json:"structuredPatch"`
	Stdout      string `json:"stdout"`
	Interrupted bool   `json:"interrupted"`
	ExitCode    *int   `json:"exitCode"`
}

type claudeActivity struct {
	pending   map[string]claudeToolCall
	completed map[string]bool
}

func newClaudeActivity() *claudeActivity {
	return &claudeActivity{pending: make(map[string]claudeToolCall), completed: make(map[string]bool)}
}

// Only completed, successful calls produce activity. Tool IDs are independent
// of API request IDs, since one response can contain several tool calls.
func (a *claudeActivity) process(entry ClaudeJSONLEntry, ts time.Time, sessionID string, meta claudeSessionMeta, repository string) []api.MetricDataPoint {
	if entry.Message == nil {
		return nil
	}
	resultCount := 0
	for _, content := range entry.Message.Content {
		if content.Type == "tool_result" {
			resultCount++
		}
	}
	var metrics []api.MetricDataPoint
	for _, content := range entry.Message.Content {
		if entry.Type == "assistant" && content.Type == "tool_use" && content.ID != "" {
			key := sessionID + ":" + content.ID
			if _, exists := a.pending[key]; !exists && !a.completed[key] {
				a.pending[key] = claudeToolCall{content, entry.Message.Model}
			}
			continue
		}
		if entry.Type != "user" || content.Type != "tool_result" || content.ToolUseID == "" {
			continue
		}
		key := sessionID + ":" + content.ToolUseID
		call, exists := a.pending[key]
		if !exists || a.completed[key] {
			continue
		}
		delete(a.pending, key)
		a.completed[key] = true
		if content.IsError {
			continue
		}
		var result claudeToolResult
		// Top-level result metadata is only unambiguous for a single result block.
		if resultCount == 1 {
			_ = json.Unmarshal(entry.ToolUseResult, &result)
		}
		if result.Interrupted || (result.ExitCode != nil && *result.ExitCode != 0) {
			continue
		}
		if call.content.Name == "Bash" {
			var input struct {
				Command string `json:"command"`
			}
			data, err := json.Marshal(call.content.Input)
			if err != nil || json.Unmarshal(data, &input) != nil {
				continue
			}
			if !hasGitCommit(input.Command) {
				continue
			}
			output := content.Content
			if result.Stdout != "" {
				output = result.Stdout
			}
			// Git's completion headers distinguish created commits from dry runs,
			// failed commands and shell commands which merely mention git commit.
			seen := make(map[string]bool)
			for _, match := range gitCommitOutput.FindAllStringSubmatch(output, -1) {
				if seen[match[1]] {
					continue
				}
				seen[match[1]] = true
				metric := createCommitMetric(ts, meta, repository)
				metric.Attributes["reconstruction"] = "successful_tool_result"
				metrics = append(metrics, metric)
			}
			continue
		}
		path, added, removed, ok := completedEditLines(call.content, result, content.Content)
		if !ok {
			continue
		}
		for _, delta := range []struct {
			kind  string
			value int
		}{{"added", added}, {"removed", removed}} {
			if delta.value == 0 {
				continue
			}
			metric := createLOCMetric(ts, delta.kind, relativeFilePath(path, meta.Cwd), filepath.Ext(path), float64(delta.value), meta, repository)
			metric.Attributes["model"] = call.model
			metric.Attributes["reconstruction"] = "successful_tool_result"
			metrics = append(metrics, metric)
		}
	}
	return metrics
}

func completedEditLines(call ClaudeContent, result claudeToolResult, output string) (path string, added, removed int, ok bool) {
	if call.Name != "Edit" && call.Name != "Write" && call.Name != "MultiEdit" {
		return
	}
	data, err := json.Marshal(call.Input)
	if err != nil {
		return
	}
	var file struct {
		Path string `json:"file_path"`
	}
	if json.Unmarshal(data, &file) != nil || file.Path == "" {
		return
	}
	path = file.Path
	if result.FilePath != "" && result.FilePath != path {
		return
	}
	// A recorded patch includes unchanged context and all replace_all matches.
	if result.StructuredPatch != nil {
		for _, hunk := range result.StructuredPatch {
			for _, line := range hunk.Lines {
				switch {
				case strings.HasPrefix(line, "+"):
					added++
				case strings.HasPrefix(line, "-"):
					removed++
				case strings.HasPrefix(line, " "), strings.HasPrefix(line, "\\"):
				default:
					return path, 0, 0, false
				}
			}
		}
		return path, added, removed, true
	}
	switch call.Name {
	case "Edit":
		var input claudeEditInput
		if json.Unmarshal(data, &input) != nil || input.ReplaceAll {
			return
		}
		added, removed, ok = changedLines(input.OldString, input.NewString)
	case "Write":
		var input claudeWriteInput
		if json.Unmarshal(data, &input) != nil {
			return
		}
		if result.Type == "create" || strings.HasPrefix(output, "File created successfully") {
			return path, countLines(input.Content), 0, true
		}
		if result.OriginalFile != nil {
			added, removed, ok = changedLines(*result.OriginalFile, input.Content)
		}
		// Overwrites without a recorded original or patch cannot be reconstructed.
	case "MultiEdit":
		var input claudeMultiEditInput
		if json.Unmarshal(data, &input) != nil {
			return
		}
		for _, edit := range input.Edits {
			if edit.ReplaceAll {
				return path, 0, 0, false
			}
			a, r, valid := changedLines(edit.OldString, edit.NewString)
			if !valid {
				return path, 0, 0, false
			}
			added += a
			removed += r
		}
		ok = true
	}
	return
}

// changedLines uses the longest common subsequence after trimming shared edges.
// Bound work for unusually large replacements; omit uncertain metrics instead
// of blocking an import or substituting gross replacement sizes.
func changedLines(old, new string) (added, removed int, ok bool) {
	lines := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	a, b := lines(old), lines(new)
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	if int64(len(a))*int64(len(b)) > 4_000_000 {
		return 0, 0, false
	}
	row := make([]int, len(b)+1)
	for _, left := range a {
		previous := 0
		for j, right := range b {
			saved := row[j+1]
			if left == right {
				row[j+1] = previous + 1
			} else {
				row[j+1] = max(row[j], row[j+1])
			}
			previous = saved
		}
	}
	common := row[len(b)]
	return len(b) - common, len(a) - common, true
}

var gitCommitOutput = regexp.MustCompile(`(?m)^\[[^\]\r\n]+ ([0-9a-f]{7,64})\](?: |$)`)

// Parse shell syntax only; no session command or expansion is ever executed.
func hasGitCommit(command string) bool {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return false
	}
	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if found {
			return false
		}
		if _, ok := node.(*syntax.FuncDecl); ok {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		args := make([]string, len(call.Args))
		for i, arg := range call.Args {
			args[i], _ = literalShellWord(arg.Parts)
		}
		if args[0] != "git" && args[0] != "/usr/bin/git" && args[0] != "/bin/git" {
			return true
		}
		for i := 1; i < len(args); i++ {
			arg := args[i]
			switch {
			case arg == "-C" || arg == "-c" || arg == "--git-dir" || arg == "--work-tree" || arg == "--namespace":
				i++
			case arg == "--no-pager" || arg == "--no-optional-locks" || arg == "--bare":
			case strings.HasPrefix(arg, "--git-dir=") || strings.HasPrefix(arg, "--work-tree=") || strings.HasPrefix(arg, "--namespace=") || strings.HasPrefix(arg, "--config-env="):
			case strings.HasPrefix(arg, "-C") || strings.HasPrefix(arg, "-c"):
			default:
				found = arg == "commit"
				return !found
			}
		}
		return true
	})
	return found
}

func literalShellWord(parts []syntax.WordPart) (string, bool) {
	var result strings.Builder
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.Lit:
			result.WriteString(p.Value)
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", false
			}
			result.WriteString(p.Value)
		case *syntax.DblQuoted:
			s, ok := literalShellWord(p.Parts)
			if !ok {
				return "", false
			}
			result.WriteString(s)
		default:
			return "", false
		}
	}
	return result.String(), true
}
