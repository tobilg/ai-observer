package importer

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"mvdan.cc/sh/v3/syntax"
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
//  5. A unique candidate when no cwd is available

var (
	githubURLRe = regexp.MustCompile(`(?i)(?:^|://|@)(?:[^/@]+@)?github\.com[:/]+([^/]+/[^/?#]+)`)
	schemeRe    = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
	userAtRe    = regexp.MustCompile(`^[^@/]+@`)

	// Match standalone GitHub references, excluding lookalike hosts and dynamic paths.
	repoURLRe  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_.@/-])(?:https?://|ssh://git@|git@)?github\.com[:/]([a-z0-9_.-]+/[a-z0-9_.-]+)`)
	repoNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)
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
// entries and text-mined references - deduplicated by repository identity.
//
// The working directory says which repo we're in; candidates supply the owner/
// prefix the cwd alone can't. A candidate whose repo-part matches the cwd name
// or a path segment can supply an owner when Git metadata is absent. A candidate that doesn't
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

	// Resolve only an unambiguous candidate. An exact cwd-name match outranks
	// ancestor matches; competing owners of the same name fall back to cwd.
	if len(candidates) > 0 && path != "" {
		segments := make(map[string]bool)
		for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
			segments[strings.ToLower(seg)] = true
		}
		for _, exact := range []bool{true, false} {
			var matches []string
			for _, repo := range candidates {
				name := strings.ToLower(repo[strings.LastIndex(repo, "/")+1:])
				if (exact && name == strings.ToLower(cwdName)) || (!exact && segments[name]) {
					matches = append(matches, repo)
				}
			}
			if len(matches) > 0 {
				if repo := uniqueRepository(matches); repo != "" {
					return repo
				}
				return cwdName
			}
		}
	}
	if cwdName != "" {
		return cwdName
	}
	return uniqueRepository(candidates)
}

func uniqueRepository(candidates []string) string {
	var result string
	for _, repo := range candidates {
		if result != "" && !strings.EqualFold(result, repo) {
			return ""
		}
		result = repo
	}
	return result
}

// isPlaceholder returns true if repo is a documentation/example placeholder.
func isPlaceholder(repo string) bool {
	return placeholderRepos[strings.ToLower(repo)] ||
		strings.Contains(repo, "<") || strings.Contains(repo, ">")
}

func normalizeRepoFlag(val string) string {
	if gh := githubRepoFromURL(val); gh != "" {
		val = gh
	}
	val = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(val), "/"), ".git")
	if !repoNameRe.MatchString(val) || isPlaceholder(val) {
		return ""
	}
	return val
}

// extractRepositoryRefs mines attribution candidates only. References never
// create activity metrics, and commands/expansions are parsed, never executed.
func extractRepositoryRefs(texts ...string) []string {
	var repos []string
	for _, text := range texts {
		repos = append(repos, extractGitHubRefs(text)...)
		// Parse literal gh pr commands, including quoted --repo/-R values.
		// Markdown/prose that is not valid shell still contributes URL references.
		if !strings.Contains(text, "gh") {
			continue
		}
		file, err := syntax.NewParser().Parse(strings.NewReader(text), "")
		if err != nil {
			continue
		}
		syntax.Walk(file, func(node syntax.Node) bool {
			if _, ok := node.(*syntax.FuncDecl); ok {
				return false
			}
			call, ok := node.(*syntax.CallExpr)
			if !ok {
				return true
			}
			args := make([]string, len(call.Args))
			for i, arg := range call.Args {
				args[i], _ = literalShellWord(arg.Parts)
			}
			repos = append(repos, repositoryFromGHArgs(args)...)
			return true
		})
	}
	return repos
}

func repositoryFromGHArgs(args []string) []string {
	if len(args) < 3 || args[0] != "gh" || args[1] != "pr" {
		return nil
	}
	var repos []string
	for i := 2; i < len(args); i++ {
		arg := args[i]
		var value string
		switch {
		case arg == "--":
			return repos
		case arg == "--repo" || arg == "-R":
			i++
			if i < len(args) {
				value = args[i]
			}
		case strings.HasPrefix(arg, "--repo="):
			value = strings.TrimPrefix(arg, "--repo=")
		case strings.HasPrefix(arg, "-R"):
			value = strings.TrimPrefix(arg, "-R")
		}
		if repo := normalizeRepoFlag(value); repo != "" {
			repos = append(repos, repo)
		}
	}
	return repos
}

func extractGitHubRefs(text string) []string {
	var repos []string
	for _, match := range repoURLRe.FindAllStringSubmatchIndex(text, -1) {
		// Check the terminator without consuming it: adjacent references can
		// share a separator. Reject partial matches before shell expansions.
		if end := match[3]; end < len(text) && !strings.ContainsRune("/ \t\r\n?#)\"',;`>]}", rune(text[end])) {
			continue
		}
		if repo := normalizeRepoFlag(strings.TrimRight(text[match[2]:match[3]], ".")); repo != "" {
			repos = append(repos, repo)
		}
	}
	return repos
}
