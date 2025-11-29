package projectfs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CommitResult struct {
	CommitID string `json:"commitId"`
	Branch   string `json:"branch"`
	Remote   string `json:"remote,omitempty"`
	Pushed   bool   `json:"pushed"`
}

func CommitWorkspaceChanges(workspacePath, message string) (*CommitResult, error) {
	if workspacePath == "" {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(workspacePath, ".git")); err != nil {
		return nil, nil
	}

	clean, err := isTreeClean(workspacePath)
	if err != nil || clean {
		return nil, err
	}

	if err := runGit(workspacePath, "add", "-A"); err != nil {
		return nil, fmt.Errorf("git add failed: %w", err)
	}

	commitMsg := sanitizeCommitMessage(message)
	if err := runGit(workspacePath, "commit", "-m", commitMsg); err != nil {
		return nil, fmt.Errorf("git commit failed: %w", err)
	}

	commitID, _ := gitOutput(workspacePath, "rev-parse", "HEAD")
	branch, _ := gitOutput(workspacePath, "rev-parse", "--abbrev-ref", "HEAD")
	remote, _ := gitOutput(workspacePath, "config", "--get", "remote.origin.url")

	result := &CommitResult{
		CommitID: strings.TrimSpace(commitID),
		Branch:   strings.TrimSpace(branch),
		Remote:   strings.TrimSpace(remote),
	}

	if result.Remote != "" && result.Branch != "" {
		if err := runGit(workspacePath, "push", "origin", result.Branch); err == nil {
			result.Pushed = true
		} else {
			return result, fmt.Errorf("git push failed: %w", err)
		}
	}

	return result, nil
}

func isTreeClean(path string) (bool, error) {
	out, err := gitOutput(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

func sanitizeCommitMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "Automated workspace update"
	}
	lines := strings.Split(msg, "\n")
	first := strings.TrimSpace(lines[0])
	const maxLen = 72
	if len([]rune(first)) > maxLen {
		runes := []rune(first)
		first = string(runes[:maxLen-3]) + "..."
	}
	return first
}

func runGit(path string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = path
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func gitOutput(path string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = path
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
