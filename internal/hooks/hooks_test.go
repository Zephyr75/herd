package hooks

import "testing"

func TestHasShellChild(t *testing.T) {
	ps := `      1 /sbin/init
   4115 /usr/bin/zsh -c source /home/u/.claude/shell-snapshots/snapshot-zsh-1.sh 2>/dev/null || true && eval 'sleep 20'
   4115 sh -c [ -r "$HERD_ENDPOINT" ] && curl …
   4116 /usr/bin/node mcp-server.js`
	if !hasShellChild(ps, 4115) {
		t.Error("the background zsh of claude 4115 was not found")
	}
	if hasShellChild(ps, 4116) || hasShellChild(ps, 1) {
		t.Error("a process without a Bash tool shell was reported")
	}
}
