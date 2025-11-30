package projectfs

import (
	"fmt"
	"os/exec"
	"sync"
)

var (
	gitCheckOnce sync.Once
	gitCheckErr  error
)

func ensureGitBinary() error {
	gitCheckOnce.Do(func() {
		if _, err := exec.LookPath("git"); err != nil {
			gitCheckErr = fmt.Errorf("git executable not found in PATH: %w", err)
		}
	})
	return gitCheckErr
}
