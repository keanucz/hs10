package projectfs

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const baseDir = "data/projects"

type Settings struct {
	WorkspacePath string `json:"workspacePath"`
	RepoType      string `json:"repoType,omitempty"`
	RepoURL       string `json:"repoUrl,omitempty"`
}

func WorkspacePath(projectID string) string {
	return filepath.Join(baseDir, projectID)
}

func EnsureWorkspace(path string) error {
	return os.MkdirAll(path, 0o755)
}

func SetupProjectWorkspace(projectID, repoOption, repoURL string) (Settings, error) {
	option := strings.ToLower(strings.TrimSpace(repoOption))
	if option == "" {
		option = "init"
	}

	workspacePath := WorkspacePath(projectID)
	parentDir := filepath.Dir(workspacePath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return Settings{}, fmt.Errorf("failed to create parent workspace dir: %w", err)
	}

	switch option {
	case "clone":
		if repoURL == "" {
			return Settings{}, errors.New("repo_url is required when repo_option is 'clone'")
		}
		if err := cloneRepo(repoURL, workspacePath); err != nil {
			return Settings{}, err
		}
	default:
		if err := EnsureWorkspace(workspacePath); err != nil {
			return Settings{}, fmt.Errorf("failed to create workspace: %w", err)
		}
		if err := initRepo(workspacePath); err != nil {
			return Settings{}, err
		}
	}

	return Settings{
		WorkspacePath: workspacePath,
		RepoType:      option,
		RepoURL:       repoURL,
	}, nil
}

func SaveSettings(db *sql.DB, projectID string, settings Settings) error {
	buf, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	_, err = db.Exec(`UPDATE projects SET settings = ? WHERE id = ?`, string(buf), projectID)
	if err != nil {
		return fmt.Errorf("failed to persist settings: %w", err)
	}
	return nil
}

func LoadSettings(db *sql.DB, projectID string) (Settings, error) {
	var raw sql.NullString
	err := db.QueryRow(`SELECT settings FROM projects WHERE id = ?`, projectID).Scan(&raw)
	if err != nil {
		return Settings{}, err
	}

	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return Settings{}, nil
	}

	var settings Settings
	if err := json.Unmarshal([]byte(raw.String), &settings); err != nil {
		return Settings{}, fmt.Errorf("invalid project settings: %w", err)
	}
	return settings, nil
}

func initRepo(path string) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git init failed: %v (%s)", err, stderr.String())
	}
	return nil
}

func cloneRepo(repoURL, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("workspace %s already exists", path)
	}

	cmd := exec.Command("git", "clone", repoURL, path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %v (%s)", err, stderr.String())
	}
	return nil
}
