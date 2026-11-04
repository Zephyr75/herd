#!/usr/bin/env bash
# Stands in for claude in tests: posts hooks like Claude Code would, writes
# the step's artifact, then waits for input like an interactive session.
post() {
  [ -r "$HERD_ENDPOINT" ] && . "$HERD_ENDPOINT" # the current herd, as in the real hook
  printf '%s' "$2" | curl -s -m 3 -X POST "$HERD_URL/hook?event=$1" \
    -H "X-Herd-Token: $HERD_TOKEN" -H "X-Herd-Step: $HERD_TASK/$HERD_STEP" --data-binary @- >/dev/null
}
resume=""
for a in "$@"; do [ "$prev" = "--resume" ] && resume="$a"; [ "$a" = "--" ] && prompted=1; prev="$a"; done
if [ -n "$resume" ] && [ -z "$prompted" ]; then
  post SessionStart "{\"session_id\":\"$resume\",\"source\":\"resume\"}"
  echo "resumed $resume"
  read -r line
  exit 0
fi
if [ -n "$CLAUDE_CODE_CHILD_SESSION$CLAUDECODE" ]; then echo "inherited Claude Code session markers" >&2; exit 3; fi
post SessionStart '{"source":"startup"}'
post UserPromptSubmit '{}'
echo "fake claude: ${@: -1}"
case "$HERD_STEP" in
  impl-*) echo "change $HERD_N" >> "file-$HERD_N.txt"
          printf -- '---\nstatus: done\n---\n# Changes\n- edited\n' > "$HERD_ARTIFACT" ;;
  commit-*) printf -- '---\nstatus: done\n---\nfeat: fake change\n\nWhat changed.\n' > "$HERD_ARTIFACT" ;;
  review-*) printf -- "---\nstatus: ${FAKE_REVIEW:-done}\n---\n# Findings\n- one\n" > "$HERD_ARTIFACT" ;;
  ask-*) post Notification '{"notification_type":"permission_prompt","message":"Claude needs your permission to use Bash"}'
         read -r answer; post PostToolUse '{}'
         printf -- '---\nstatus: done\n---\n' > "$HERD_ARTIFACT" ;;
  *) printf -- "---\nstatus: ${FAKE_STATUS:-done}\n---\n# Goal\nx\n" > "$HERD_ARTIFACT"
     [ -n "$FAKE_OPTIONS" ] && printf -- '## Options\n- A\n- B\n' >> "$HERD_ARTIFACT" ;;
esac
post Stop '{}'
read -r line
