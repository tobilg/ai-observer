package importer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRepositoryReferenceMining(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       []string
	}{
		{"https", "See https://github.com/acme/project.git", []string{"acme/project"}},
		{"pull_url", "(https://github.com/acme/project/pull/42)", []string{"acme/project"}},
		{"query", "https://github.com/acme/project?tab=readme", []string{"acme/project"}},
		{"ssh", "git@github.com:acme/project.git", []string{"acme/project"}},
		{"ssh_scheme", "ssh://git@github.com/acme/project.git", []string{"acme/project"}},
		{"bare_url", "github.com/acme/project", []string{"acme/project"}},
		{"repo_flag", "gh pr view 42 --repo acme/project", []string{"acme/project"}},
		{"quoted_flag", "gh pr view --repo 'acme/project'", []string{"acme/project"}},
		{"equals_flag", "gh pr list --repo=acme/project", []string{"acme/project"}},
		{"short_flag", "gh pr list -Racme/project", []string{"acme/project"}},
		{"short_separate", "gh pr list -R acme/project", []string{"acme/project"}},
		{"deduplicate", "https://github.com/acme/project https://github.com/ACME/PROJECT/pull/2", []string{"acme/project"}},
		{"adjacent_references", "https://github.com/acme/project https://github.com/other/project", []string{"acme/project", "other/project"}},
		{"other_command", "other --repo acme/project", nil},
		{"echoed_command", "echo 'gh pr list --repo acme/project'", nil},
		{"dynamic_flag", "gh pr list --repo \"$ORG/project\"", nil},
		{"dynamic_url", "https://github.com/acme/project-$SUFFIX", nil},
		{"placeholder", "https://github.com/owner/repo gh pr list --repo '<org>/<repo>'", nil},
		{"lookalike_host", "https://notgithub.com/acme/project https://github.com.evil.test/acme/project", nil},
		{"url_in_other_path", "https://example.test/github.com/acme/project", nil},
		{"flag_after_separator", "gh pr view -- --repo acme/project", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var meta RepositoryMetadata
			meta.observeText(tc.text)
			if !reflect.DeepEqual(meta.CandidateRepos, tc.want) {
				t.Fatalf("candidates = %q, want %q", meta.CandidateRepos, tc.want)
			}
		})
	}
}

func TestRepositoryResolutionPrecedence(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "project")
	for _, tc := range []struct {
		name string
		meta RepositoryMetadata
		want string
	}{
		{"git_metadata", RepositoryMetadata{GitRepository: "acme/actual", Cwd: missing, CandidateRepos: []string{"other/project"}}, "acme/actual"},
		{"matching_reference", RepositoryMetadata{Cwd: missing, CandidateRepos: []string{"acme/project"}}, "acme/project"},
		{"unrelated_reference", RepositoryMetadata{Cwd: missing, CandidateRepos: []string{"other/dependency"}}, "project"},
		{"ambiguous_owner", RepositoryMetadata{Cwd: missing, CandidateRepos: []string{"acme/project", "other/project"}}, "project"},
		{"unique_without_cwd", RepositoryMetadata{CandidateRepos: []string{"acme/project"}}, "acme/project"},
		{"ambiguous_without_cwd", RepositoryMetadata{CandidateRepos: []string{"acme/project", "other/dependency"}}, ""},
		{"cwd_before_ancestor", RepositoryMetadata{Cwd: filepath.Join(missing, "child"), CandidateRepos: []string{"acme/project", "other/child"}}, "other/child"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.meta.Repository(); got != tc.want {
				t.Fatalf("repository = %q, want %q", got, tc.want)
			}
		})
	}
}

func repositoryGit(t *testing.T, args ...string) {
	t.Helper()
	if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, output, err)
	}
}

func TestRepositoryRemoteAndWorktree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "different-local-name")
	repositoryGit(t, "init", dir)
	repositoryGit(t, "-C", dir, "remote", "add", "origin", "https://github.com/acme/project.git")
	meta := RepositoryMetadata{Cwd: dir, CandidateRepos: []string{"other/dependency"}}
	if got := meta.Repository(); got != "acme/project" {
		t.Fatalf("remote repository = %q", got)
	}
	repositoryGit(t, "-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "fixture")
	worktree := filepath.Join(t.TempDir(), "feature-worktree")
	repositoryGit(t, "-C", dir, "worktree", "add", "--detach", worktree)
	meta.Cwd = worktree
	if got := meta.Repository(); got != "acme/project" {
		t.Fatalf("worktree repository = %q", got)
	}
	meta.GitRepository = "recorded/original"
	if got := meta.Repository(); got != "recorded/original" {
		t.Fatalf("recorded remote did not win: %q", got)
	}
}

func TestRepositoryWithoutGit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	meta := RepositoryMetadata{Cwd: dir, CandidateRepos: []string{"acme/project"}}
	if got := meta.Repository(); got != "acme/project" {
		t.Fatalf("repository without git = %q", got)
	}
}

func TestRepositoryRetainsRemoteAfterRestart(t *testing.T) {
	dir := t.TempDir()
	repositoryGit(t, "init", dir)
	repositoryGit(t, "-C", dir, "remote", "add", "origin", "https://secret@github.com/acme/project.git")
	meta := RepositoryMetadata{Cwd: dir}
	meta.Enrich(nil, nil)
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	gitRemoteCache.Delete(dir)
	var restored RepositoryMetadata
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.LocalRepository != "acme/project" || restored.Repository() != "acme/project" {
		t.Fatalf("lost previously discovered remote: %+v", restored)
	}
}

func TestRepositoryMetadataScope(t *testing.T) {
	for _, tc := range []struct {
		name, entry string
		want        string
	}{
		{"claude_command", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"gh pr view -R acme/project"}}]}}`, "acme/project"},
		{"claude_result", `{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"https://github.com/acme/project/pull/1"}]}]}}`, "acme/project"},
		{"claude_write_excluded", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"content":"https://github.com/acme/project"}}]}}`, ""},
		{"codex_command", `{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"gh pr view --repo acme/project\"}"}}`, "acme/project"},
		{"codex_argv", `{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"gh\",\"pr\",\"view\",\"--repo\",\"acme/project\"]}"}}`, "acme/project"},
		{"codex_shell_argv", `{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"gh pr view --repo acme/project\"]}"}}`, "acme/project"},
		{"codex_echo_argv", `{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"echo\",\"gh pr view --repo acme/project\"]}"}}`, ""},
		{"codex_result", `{"type":"response_item","payload":{"type":"function_call_output","output":"https://github.com/acme/project/pull/1"}}`, "acme/project"},
		{"codex_event", `{"type":"event_msg","payload":{"type":"agent_message","message":"https://github.com/acme/project"}}`, "acme/project"},
		{"codex_system_excluded", `{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"https://github.com/acme/project"}]}}`, ""},
		{"codex_reasoning_excluded", `{"type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"https://github.com/acme/project"}]}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var meta RepositoryMetadata
			if tc.name[:6] == "claude" {
				var entry ClaudeJSONLEntry
				if err := json.Unmarshal([]byte(tc.entry), &entry); err != nil {
					t.Fatal(err)
				}
				meta.ObserveClaude(entry)
			} else {
				var entry CodexJSONLEntry
				if err := json.Unmarshal([]byte(tc.entry), &entry); err != nil {
					t.Fatal(err)
				}
				meta.ObserveCodex(entry)
			}
			if got := meta.Repository(); got != tc.want {
				t.Fatalf("repository = %q, want %q", got, tc.want)
			}
		})
	}
}
