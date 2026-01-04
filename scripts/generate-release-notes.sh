#!/bin/bash
set -e

# Generate release notes using Claude and prepend to GitHub release
# Usage: ./scripts/generate-release-notes.sh [TAG]
# If TAG is not provided, uses the latest tag

TAG="${1:-$(git describe --tags --abbrev=0 2>/dev/null)}"

if [ -z "$TAG" ]; then
    echo "Error: No tag found. Please provide a tag as argument or create one first."
    exit 1
fi

# Check if gh CLI is available early
if ! command -v gh &> /dev/null; then
    echo "Error: GitHub CLI (gh) is not installed."
    echo "Install it with: brew install gh"
    exit 1
fi

echo "Generating release notes for $TAG..."

# Get previous tag
PREV_TAG=$(git describe --tags --abbrev=0 "$TAG^" 2>/dev/null || echo "")

# Get commits since previous tag (or all commits if no previous tag)
if [ -n "$PREV_TAG" ]; then
    echo "Getting commits from $PREV_TAG to $TAG..."
    COMMITS=$(git log "$PREV_TAG".."$TAG" --pretty=format:"- %s" 2>/dev/null || echo "")
else
    echo "Getting all commits up to $TAG..."
    COMMITS=$(git log "$TAG" --pretty=format:"- %s" 2>/dev/null || echo "")
fi

if [ -z "$COMMITS" ]; then
    echo "Error: No commits found."
    exit 1
fi

echo ""
echo "Commits:"
echo "$COMMITS"
echo ""

# Create prompt for Claude
PROMPT="You are generating a release summary for AI Observer $TAG, an OpenTelemetry-compatible observability backend for AI coding tools (Claude Code, Gemini CLI, Codex CLI).

Based on these commits since the last release, generate a concise, user-friendly summary in markdown format:

$COMMITS

Format the output with these sections (only include sections that have content):

## Highlights
(1-2 sentence summary of the most important changes)

## New Features
(list new features, if any)

## Improvements
(list improvements/enhancements, if any)

## Bug Fixes
(list fixes, if any)

Rules:
- Only include sections that have content
- Be concise - one line per item
- Do not include commit hashes
- Group related changes together
- Use clear, user-friendly language
- Skip minor changes like typos, small refactors, CI tweaks
- Output ONLY the markdown, no preamble or explanation"

# Generate release notes using Claude CLI
echo "Calling Claude to generate release summary..."
SUMMARY=$(echo "$PROMPT" | claude -p 2>/dev/null)

if [ -z "$SUMMARY" ]; then
    echo "Error: Failed to generate release notes with Claude."
    echo "Make sure the 'claude' CLI is installed and working."
    exit 1
fi

# Fetch existing release notes from GitHub
echo "Fetching existing release notes from GitHub..."
if gh release view "$TAG" &> /dev/null; then
    EXISTING_NOTES=$(gh release view "$TAG" --json body -q .body 2>/dev/null || echo "")
else
    echo "Warning: Release $TAG does not exist on GitHub yet."
    EXISTING_NOTES=""
fi

# Combine: Claude summary + separator + existing notes
if [ -n "$EXISTING_NOTES" ]; then
    COMBINED_NOTES="$SUMMARY

---

$EXISTING_NOTES"
else
    COMBINED_NOTES="$SUMMARY"
fi

# Save to file
NOTES_FILE="release-notes-$TAG.md"
echo "$COMBINED_NOTES" > "$NOTES_FILE"

echo ""
echo "Generated release summary:"
echo "=========================="
echo "$SUMMARY"
echo "=========================="
echo ""
if [ -n "$EXISTING_NOTES" ]; then
    echo "(Will be prepended to existing GitHub-generated changelog)"
    echo ""
fi
echo "Full notes saved to: $NOTES_FILE"
echo ""

# Ask user if they want to update the GitHub release
read -p "Update GitHub release $TAG with these notes? [y/N] " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    # Check if release exists
    if ! gh release view "$TAG" &> /dev/null; then
        echo "Error: Release $TAG does not exist on GitHub."
        echo "Push the tag first: git push origin $TAG"
        exit 1
    fi

    echo "Updating GitHub release..."
    gh release edit "$TAG" --notes-file "$NOTES_FILE"
    echo "Done! Release updated: $(gh release view "$TAG" --json url -q .url)"

    # Clean up notes file after successful update
    rm -f "$NOTES_FILE"
else
    echo "Skipped. You can manually update the release with:"
    echo "  gh release edit $TAG --notes-file $NOTES_FILE"
fi
