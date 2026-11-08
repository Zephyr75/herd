package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"herd/internal/settings"
	"herd/internal/store"
)

// attachDir holds the files attached at task creation, in the run dir.
const attachDir = "attachments"

// copyAttachments copies files to dir/attachments, renaming duplicates.
func copyAttachments(dir string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	dst := filepath.Join(dir, attachDir)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		ext := filepath.Ext(f)
		base := strings.TrimSuffix(filepath.Base(f), ext)
		name := base + ext
		for i := 2; ; i++ {
			if _, err := os.Stat(filepath.Join(dst, name)); os.IsNotExist(err) {
				break
			}
			name = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		if err := os.WriteFile(filepath.Join(dst, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// attachments lists the attached files' names, sorted.
func attachments(dir string) []string {
	es, _ := os.ReadDir(filepath.Join(dir, attachDir))
	var names []string
	for _, e := range es {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	return names
}

// writeTaskFile writes TASK.md to the run dir: the task source every agent
// reads first.
func (a *App) writeTaskFile(p settings.Project, t store.Task) error {
	dir := a.runDir(p, t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	source := "local"
	switch {
	case t.Issue != "" && t.Description != "":
		source = "issue+description"
	case t.Issue != "":
		source = "issue"
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "source: %s\n", source)
	if t.Issue != "" {
		fmt.Fprintf(&b, "issue: %s\nrepo: %s\n", t.Issue, a.issueRepo(t))
	}
	fmt.Fprintf(&b, "project: %s\nbranch: %s\nworktree: %s\nbase: %s\n", p.Name, branchName(t), worktreePath(p, t), p.BaseBranch)
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", t.Title)
	if t.Issue != "" {
		if _, err := os.Stat(filepath.Join(dir, issueFile)); err == nil {
			fmt.Fprintf(&b, "GitHub issue #%s in %s: herd copied it, comments included, to %s in this folder.\n\n", t.Issue, a.issueRepo(t), issueFile)
		} else {
			fmt.Fprintf(&b, "GitHub issue #%s in %s: herd could not copy it, so read it with `gh issue view %s --repo %s --comments`.\n\n", t.Issue, a.issueRepo(t), t.Issue, a.issueRepo(t))
		}
	} else {
		b.WriteString("LOCAL task: there is no GitHub issue. Work from the description only and make no GitHub calls.\n\n")
	}
	if t.Description != "" {
		b.WriteString("## Description\n\n")
		b.WriteString(strings.TrimSpace(t.Description))
		b.WriteString("\n")
	}
	if names := attachments(dir); len(names) > 0 {
		b.WriteString("\n## Attachments\n\nFiles the user attached to the task: read each one (images too) before planning.\n\n")
		for _, n := range names {
			fmt.Fprintf(&b, "- %s\n", filepath.Join(dir, attachDir, n))
		}
	}
	return os.WriteFile(filepath.Join(dir, "TASK.md"), []byte(b.String()), 0o644)
}

// writeContextFile writes CONTEXT.md: the general context, then the
// project's, then the knowledge base folders.
func (a *App) writeContextFile(p settings.Project, t store.Task) error {
	var b strings.Builder
	b.WriteString("<!-- Written by herd before each step. Edit it in Settings > Context. -->\n\n")
	if c, ok := a.settings.Context("general"); ok && strings.TrimSpace(c.Text) != "" {
		b.WriteString(strings.TrimSpace(c.Text))
		b.WriteString("\n\n")
	}
	if c, ok := a.settings.Context(p.ID); ok {
		if strings.TrimSpace(c.Text) != "" {
			b.WriteString(strings.TrimSpace(c.Text))
			b.WriteString("\n\n")
		}
		if c.ClaudeMd {
			b.WriteString("The repository's CLAUDE.md applies to this task; follow it.\n")
		}
	}
	if dirs := a.knowledgeDirs(p); len(dirs) > 0 {
		b.WriteString("\n## Knowledge base\n\nReference folders you can read (never edit them). Search them for background on the domain, conventions and past decisions:\n\n")
		for _, d := range dirs {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	}
	return os.WriteFile(filepath.Join(a.runDir(p, t), "CONTEXT.md"), []byte(b.String()), 0o644)
}

var cmdName = regexp.MustCompile(`^/herd:([A-Za-z0-9_-]+)`)

// commandName returns "implement" for "/herd:implement {task} -- {guidance}".
func commandName(command string) string {
	m := cmdName.FindStringSubmatch(strings.TrimSpace(command))
	if m == nil {
		return ""
	}
	return m[1]
}

// writePlugin rebuilds the herd Claude Code plugin in ~/.herd/plugin: its
// manifest and one command per agent, built from the agent's instructions.
func (a *App) writePlugin() error {
	dir := a.pluginDir()
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		return err
	}
	manifest := `{"name": "herd", "version": "1.0.0", "description": "Agents run by herd: one command per agent in Settings > Agents"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o755); err != nil {
		return err
	}
	for _, ag := range a.settings.Agents() {
		name := commandName(ag.Command)
		if name == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "commands", name+".md"), []byte(a.CommandText(ag)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// CommandText is the command herd writes for an agent (/herd:<name>): its
// instructions between the prompts of Settings > Context > Prompts.
func (a *App) CommandText(ag settings.Agent) string {
	stem := strings.TrimSuffix(ag.Writes, filepath.Ext(ag.Writes))
	vars := map[string]string{"artifact": ag.Writes, "stem": stem}
	var b strings.Builder
	fmt.Fprintf(&b, "---\ndescription: herd %s agent\nargument-hint: \"<task> [-- guidance]\"\n---\n\n", ag.Name)
	fmt.Fprintf(&b, "You are the herd **%s** agent. Arguments: $ARGUMENTS\n\n", ag.Name)
	b.WriteString(strings.TrimSpace(settings.Fill(a.settings.Prompt(settings.PromptProtocol), vars)) + "\n\n")
	b.WriteString("## Instructions\n\n")
	b.WriteString(strings.TrimSpace(ag.Instructions) + "\n")
	if d := strings.TrimSpace(settings.Fill(a.settings.Prompt(settings.PromptDiagrams), vars)); d != "" {
		b.WriteString("\n" + d + "\n")
	}
	if ag.Sisters {
		b.WriteString("\n" + strings.TrimSpace(settings.Fill(a.settings.Prompt(settings.PromptSisters), vars)) + "\n")
	}
	return b.String()
}
