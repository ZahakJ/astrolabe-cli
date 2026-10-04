package term

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// tmuxQueryTimeout bounds the tmux query; tmux usually answers in a few
// milliseconds.
const tmuxQueryTimeout = 150 * time.Millisecond

// tmuxClientTerm asks the tmux server named by tmuxEnv (the value of
// $TMUX) for the TERM of the client attached to the current session, the
// terminal astrolabe is actually drawn on. It returns "" when tmux is missing,
// slow or fails.
func tmuxClientTerm(tmuxEnv string) string {
	if tmuxEnv == "" {
		return ""
	}
	path, err := exec.LookPath("tmux")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxQueryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "display-message", "-p", "#{client_termname}")
	// Talk to the server astrolabe runs under, as named by the environment
	// astrolabe was given, and to no other.
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "TMUX="+tmuxEnv)
	cmd.Stdin, cmd.Stderr = nil, nil
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
