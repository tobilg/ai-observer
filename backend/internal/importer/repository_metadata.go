package importer

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// RepositoryMetadata is shared by full imports and incremental parsers. Watchers
// persist it alongside their offsets, so later batches retain session context.
// GitRepository stores the normalized identifier, never URL credentials.
type RepositoryMetadata struct {
	Cwd             string   `json:"cwd,omitempty"`
	OriginalCwd     string   `json:"originalCwd,omitempty"`
	GitBranch       string   `json:"gitBranch,omitempty"`
	GitRepository   string   `json:"gitRepository,omitempty"`
	LocalRepository string   `json:"localRepository,omitempty"`
	LocalPath       string   `json:"localPath,omitempty"`
	CandidateRepos  []string `json:"candidateRepos,omitempty"`
}

func (m *RepositoryMetadata) addCandidates(repos ...string) {
	for _, repo := range repos {
		if repo == "" {
			continue
		}
		seen := false
		for _, existing := range m.CandidateRepos {
			if strings.EqualFold(existing, repo) {
				seen = true
				break
			}
		}
		if !seen {
			m.CandidateRepos = append(m.CandidateRepos, repo)
		}
	}
}

func (m *RepositoryMetadata) observeText(texts ...string) {
	m.addCandidates(extractRepositoryRefs(texts...)...)
}

// ObserveClaude collects attribution evidence without emitting any telemetry.
func (m *RepositoryMetadata) ObserveClaude(entry ClaudeJSONLEntry) {
	if m.Cwd == "" {
		m.Cwd = entry.Cwd
	}
	if m.GitBranch == "" {
		m.GitBranch = entry.GitBranch
	}
	if entry.Type == "worktree-state" && entry.WorktreeSession != nil && m.OriginalCwd == "" {
		m.OriginalCwd = entry.WorktreeSession.OriginalCwd
	}
	if entry.Type == "pr-link" && entry.PRNumber > 0 {
		if _, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			repo := strings.TrimSpace(entry.PRRepository)
			if repo == "" {
				repo = githubRepoFromURL(entry.PRUrl)
			}
			m.addCandidates(repo)
		}
	}
	if entry.Message == nil || (entry.Type != "user" && entry.Type != "assistant") {
		return
	}
	for _, content := range entry.Message.Content {
		switch content.Type {
		case "text":
			m.observeText(content.Text)
		case "tool_result":
			m.observeText(content.Content)
		case "tool_use":
			if content.Name == "Bash" {
				data, err := json.Marshal(content.Input)
				if err == nil {
					m.observeCommandArguments(data)
				}
			}
		}
	}
}

// ObserveCodex accepts metadata, visible messages/results, and shell tool inputs.
// Arbitrary tool arguments and reasoning summaries are not attribution evidence.
func (m *RepositoryMetadata) ObserveCodex(entry CodexJSONLEntry) {
	switch entry.Type {
	case "session_meta":
		var meta CodexSessionMeta
		if json.Unmarshal(entry.Payload, &meta) != nil {
			return
		}
		if m.Cwd == "" {
			m.Cwd = meta.Cwd
		}
		if meta.Git != nil {
			if m.GitRepository == "" {
				m.GitRepository = normalizeGitURL(meta.Git.RepositoryURL)
			}
			if m.GitBranch == "" {
				m.GitBranch = meta.Git.Branch
			}
		}
	case "response_item":
		var item CodexResponseItem
		if json.Unmarshal(entry.Payload, &item) != nil {
			return
		}
		switch item.Type {
		case "message":
			if item.Role != "user" && item.Role != "assistant" {
				return
			}
			for _, content := range item.Content {
				if content.Type == "input_text" || content.Type == "output_text" {
					m.observeText(content.Text)
				}
			}
		case "function_call":
			switch item.Name {
			case "exec_command", "shell_command", "shell":
				m.observeCommandArguments([]byte(item.Arguments))
			}
		case "function_call_output":
			var output string
			if json.Unmarshal(item.Output, &output) == nil {
				m.observeText(output)
			}
		}
	case "event_msg":
		var event struct{ Type, Message string }
		if json.Unmarshal(entry.Payload, &event) == nil && (event.Type == "user_message" || event.Type == "agent_message") {
			m.observeText(event.Message)
		}
	}
}

func (m *RepositoryMetadata) observeCommandArguments(data []byte) {
	var args struct {
		Command json.RawMessage `json:"command"`
		Cmd     string          `json:"cmd"`
	}
	if json.Unmarshal(data, &args) != nil {
		return
	}
	m.observeText(args.Cmd)
	var command string
	if json.Unmarshal(args.Command, &command) == nil {
		m.observeText(command)
		return
	}
	var argv []string
	if json.Unmarshal(args.Command, &argv) == nil {
		for _, arg := range argv {
			m.addCandidates(extractGitHubRefs(arg)...)
		}
		m.addCandidates(repositoryFromGHArgs(argv)...)
		if len(argv) >= 3 {
			switch filepath.Base(argv[0]) {
			case "bash", "sh", "zsh":
				if strings.HasPrefix(argv[1], "-") && !strings.HasPrefix(argv[1], "--") && strings.Contains(argv[1], "c") {
					m.observeText(argv[2])
				}
			}
		}
	}
}

// Repository returns the best identifier supported by the evidence seen so far.
func (m *RepositoryMetadata) Repository() string {
	if m.GitRepository != "" {
		return m.GitRepository
	}
	path := m.OriginalCwd
	if path == "" {
		path = m.Cwd
	}
	if path != m.LocalPath {
		m.LocalPath, m.LocalRepository = path, ""
	}
	if m.LocalRepository == "" {
		m.LocalRepository = cwdGitRemote(path)
	}
	// Persist a discovered remote so a removed worktree or a restart without
	// Git does not discard evidence that was available in an earlier batch.
	return resolveSessionRepository(m.CandidateRepos, m.LocalRepository, m.OriginalCwd, m.Cwd)
}

// Enrich applies session attribution to a batch of session logs and usage
// metrics. Do not use it on activity metrics attributed to a different repo,
// such as structured PR-link events.
func (m *RepositoryMetadata) Enrich(metrics []api.MetricDataPoint, logs []api.LogRecord) {
	repository := m.Repository()
	apply := func(attrs map[string]string) map[string]string {
		if repository == "" && m.GitBranch == "" {
			return attrs
		}
		if attrs == nil {
			attrs = make(map[string]string)
		}
		if repository != "" {
			attrs["repository"] = repository
		}
		if m.GitBranch != "" {
			attrs["git_branch"] = m.GitBranch
		}
		return attrs
	}
	for i := range metrics {
		metrics[i].Attributes = apply(metrics[i].Attributes)
	}
	for i := range logs {
		logs[i].LogAttributes = apply(logs[i].LogAttributes)
	}
}
