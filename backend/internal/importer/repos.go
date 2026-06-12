package importer

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Repository attribution logic.
//
// Ported from ducktrace/claude_analysis/repos.py and prmatch.py.
// Maps a session to a canonical "owner/repo" (or bare repo name) using the
// strongest available signal.
//
// Signal priority (best first):
//  1. Codex session_meta.git.repository_url (authoritative)
//  2. On-disk git remote from cwd (real remote, authoritative)
//  3. Candidate repos whose name matches cwd name or path segment
//  4. Bare cwd directory name
//  5. First candidate repository reference

var (
	githubURLRe = regexp.MustCompile(`(?i)(?:^|://|@)(?:[^/@]+@)?github\.com[:/]+([^/]+/[^/?#]+)`)
	schemeRe    = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
	userAtRe    = regexp.MustCompile(`^[^@/]+@`)

	ghPRRe     = regexp.MustCompile(`(?i)\bgh\s+pr\s+(create|merge|close|reopen|checkout|view|diff|review|list|edit|comment|ready|status)\b`)
	repoFlagRe = regexp.MustCompile(`(?i)--repo[ =]\s*['"]?([^\s'"]+)`)
	pullURLRe  = regexp.MustCompile(`(?i)https?://github\.com/([^/\s]+/[^/\s]+?)/pull/(\d+)`)
	// repoURLRe catches any github.com/owner/repo reference not already matched by pullURLRe.
	// The lazy second component combined with the required terminator stops at path separators.
	repoURLRe = regexp.MustCompile(`(?i)github\.com[:/]+([^/\s]+/[^/\s]+?)(?:\.git)?(?:[/\s)"',` + "`" + `]|$)`)
)

var placeholderRepos = map[string]bool{
	"owner/repo": true, "owner/name": true, "org/repo": true,
	"user/repo": true, "your-org/your-repo": true,
	"username/repo": true, "owner/repository": true,
}

var gitRemoteCache sync.Map

// githubRepoFromURL extracts owner/repo from any GitHub URL (https or ssh),
// stripping embedded credentials and a trailing .git. Returns "" on no match.
func githubRepoFromURL(url string) string {
	if url == "" {
		return ""
	}
	m := githubURLRe.FindStringSubmatch(url)
	if m == nil {
		return ""
	}
	return strings.TrimSuffix(m[1], ".git")
}

// normalizeGitURL normalizes any git remote URL to owner/repo when possible.
// Handles git@host:owner/repo.git, https://host/owner/repo.git, and
// https://<token>@host/owner/repo.git (credentials are dropped). Falls back
// to the last two path segments for non-GitHub hosts.
func normalizeGitURL(url string) string {
	if gh := githubRepoFromURL(url); gh != "" {
		return gh
	}
	if url == "" {
		return ""
	}
	u := schemeRe.ReplaceAllString(strings.TrimSpace(url), "")
	u = userAtRe.ReplaceAllString(u, "")
	u = strings.Replace(u, ":", "/", 1) // ssh host:path -> host/path
	u = strings.TrimSuffix(u, ".git")
	var parts []string
	for _, p := range strings.Split(u, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) >= 3 { // host/owner/repo...
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return u
}

// resolveRepoName returns the repo name for a working dir, resolving git
// worktrees to the main repo's directory name. If the path no longer exists
// we just return its basename.
func resolveRepoName(cwd string) string {
	if cwd == "" {
		return ""
	}
	// A worktree's .git is a file (not a dir) containing "gitdir: <path>".
	content, err := os.ReadFile(filepath.Join(cwd, ".git"))
	if err == nil {
		text := strings.TrimSpace(string(content))
		if strings.HasPrefix(text, "gitdir:") {
			gitdir := strings.TrimSpace(strings.TrimPrefix(text, "gitdir:"))
			parts := strings.Split(filepath.ToSlash(gitdir), "/")
			for i, seg := range parts {
				if seg == ".git" && i > 0 {
					return filepath.Base(filepath.Join(parts[:i]...))
				}
			}
		}
	}
	return filepath.Base(cwd)
}

// cwdGitRemote returns the owner/repo of the cwd's origin remote, cached per
// path. This is authoritative when the directory still exists locally - the
// real remote is correct even when the local dir name differs from the repo name.
func cwdGitRemote(path string) string {
	if path == "" {
		return ""
	}
	if cached, ok := gitRemoteCache.Load(path); ok {
		return cached.(string)
	}
	out, err := exec.Command("git", "-C", path, "config", "--get", "remote.origin.url").Output()
	result := ""
	if err == nil {
		result = normalizeGitURL(strings.TrimSpace(string(out)))
	}
	gitRemoteCache.Store(path, result)
	return result
}

// resolveSessionRepository returns the best repository id for a session.
//
// candidateRepos are owner/repo references seen for the session - from pr-link
// entries and text-mined references - ideally most-frequent first.
//
// The working directory says which repo we're in; candidates supply the owner/
// prefix the cwd alone can't. A candidate whose repo-part matches the cwd name
// or a path segment is the highest-confidence answer. A candidate that doesn't
// match is treated as a stray and used only when there's no cwd at all.
func resolveSessionRepository(candidateRepos []string, gitRepoURL, originalCwd, cwd string) string {
	// Codex carries the cwd's actual git remote - authoritative.
	if gitRepoURL != "" {
		if norm := normalizeGitURL(gitRepoURL); norm != "" {
			return norm
		}
	}

	path := originalCwd
	if path == "" {
		path = cwd
	}

	// On-disk git remote is authoritative (handles dir name != repo name).
	if onDisk := cwdGitRemote(path); onDisk != "" {
		return onDisk
	}

	cwdName := resolveRepoName(path)
	var candidates []string
	for _, r := range candidateRepos {
		if r != "" {
			candidates = append(candidates, r)
		}
	}

	if len(candidates) > 0 && path != "" {
		segments := make(map[string]bool)
		for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
			if seg != "" {
				segments[strings.ToLower(seg)] = true
			}
		}
		for _, r := range candidates {
			parts := strings.Split(r, "/")
			name := strings.ToLower(parts[len(parts)-1])
			if name == strings.ToLower(cwdName) || segments[name] {
				return r
			}
		}
	}

	if cwdName != "" {
		return cwdName
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// isPlaceholder returns true if repo is a documentation/example placeholder.
func isPlaceholder(repo string) bool {
	return placeholderRepos[strings.ToLower(repo)] ||
		strings.Contains(repo, "<") || strings.Contains(repo, ">")
}

func normalizeRepoFlag(val string) string {
	if gh := githubRepoFromURL(val); gh != "" {
		if isPlaceholder(gh) {
			return ""
		}
		return gh
	}
	val = strings.TrimRight(strings.TrimSpace(val), "/")
	var parts []string
	for _, p := range strings.Split(val, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) >= 2 && !strings.Contains(parts[len(parts)-2], ".") {
		repo := strings.TrimSuffix(parts[len(parts)-2]+"/"+parts[len(parts)-1], ".git")
		if isPlaceholder(repo) {
			return ""
		}
		return repo
	}
	return ""
}

// extractedRefs holds PR/repo references mined from free text.
type extractedRefs struct {
	PRActions []string
	PRURLs    []string
	PRNumbers []int
	Repos     []string
}

// extractRefs scans one or more strings for gh pr commands, PR URLs,
// --repo flags, and GitHub repo references.
func extractRefs(texts ...string) extractedRefs {
	result := extractedRefs{}
	seenActions := map[string]bool{}
	seenURLs := map[string]bool{}
	seenNumbers := map[int]bool{}
	seenRepos := map[string]bool{}

	for _, text := range texts {
		if text == "" {
			continue
		}
		for _, m := range ghPRRe.FindAllStringSubmatch(text, -1) {
			action := "gh pr " + strings.ToLower(m[1])
			if !seenActions[action] {
				seenActions[action] = true
				result.PRActions = append(result.PRActions, action)
			}
		}
		for _, m := range pullURLRe.FindAllStringSubmatch(text, -1) {
			repo := strings.TrimSuffix(m[1], ".git")
			if isPlaceholder(repo) {
				continue
			}
			if !seenRepos[repo] {
				seenRepos[repo] = true
				result.Repos = append(result.Repos, repo)
			}
			if num, err := strconv.Atoi(m[2]); err == nil && !seenNumbers[num] {
				seenNumbers[num] = true
				result.PRNumbers = append(result.PRNumbers, num)
			}
			if url := m[0]; !seenURLs[url] {
				seenURLs[url] = true
				result.PRURLs = append(result.PRURLs, url)
			}
		}
		for _, m := range repoFlagRe.FindAllStringSubmatch(text, -1) {
			if r := normalizeRepoFlag(m[1]); r != "" && !seenRepos[r] {
				seenRepos[r] = true
				result.Repos = append(result.Repos, r)
			}
		}
		for _, m := range repoURLRe.FindAllStringSubmatch(text, -1) {
			r := strings.TrimSuffix(m[1], ".git")
			if !isPlaceholder(r) && !seenRepos[r] {
				seenRepos[r] = true
				result.Repos = append(result.Repos, r)
			}
		}
	}
	return result
}
