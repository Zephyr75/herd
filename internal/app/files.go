package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"herd/internal/settings"
	"herd/internal/store"
)

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
		fmt.Fprintf(&b, "issue: %s\nrepo: %s\n", t.Issue, p.Repo)
	}
	fmt.Fprintf(&b, "project: %s\nbranch: %s\nworktree: %s\nbase: %s\n", p.Name, branchName(t), worktreePath(p, t), p.BaseBranch)
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", t.Title)
	if t.Issue != "" {
		fmt.Fprintf(&b, "GitHub issue #%s in %s. Plan reads it with `gh issue view %s --repo %s`; later steps rely on PLAN.md.\n\n", t.Issue, p.Repo, t.Issue, p.Repo)
	} else {
		b.WriteString("LOCAL task: there is no GitHub issue. Work from the description only and make no GitHub calls.\n\n")
	}
	if t.Description != "" {
		b.WriteString("## Description\n\n")
		b.WriteString(strings.TrimSpace(t.Description))
		b.WriteString("\n")
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

// writePlugin installs the herd Claude Code plugin in ~/.herd/plugin: the
// shipped files plus one command per agent, built from its instructions.
func (a *App) writePlugin() error {
	dir := a.pluginDir()
	if err := os.RemoveAll(filepath.Join(dir, "commands")); err != nil {
		return err
	}
	if a.PluginSrc != nil {
		err := fs.WalkDir(a.PluginSrc, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			dst := filepath.Join(dir, p)
			if d.IsDir() {
				return os.MkdirAll(dst, 0o755)
			}
			b, err := fs.ReadFile(a.PluginSrc, p)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, b, 0o644)
		})
		if err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
			return err
		}
		manifest := `{"name": "herd", "version": "1.0.0", "description": "Agents run by herd"}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o755); err != nil {
		return err
	}
	for _, ag := range a.settings.Agents() {
		name := commandName(ag.Command)
		if name == "" {
			continue
		}
		key, values := ag.DoneRule()
		var b strings.Builder
		fmt.Fprintf(&b, "---\ndescription: herd %s agent\nargument-hint: \"<task> [-- guidance]\"\n---\n\n", ag.Name)
		fmt.Fprintf(&b, "You are the herd **%s** agent. Arguments: $ARGUMENTS\n\n", ag.Name)
		b.WriteString("Text after `--` in the arguments is guidance from the user for this step; follow it.\n")
		b.WriteString("The *herd step* block in your system prompt gives the task, the step number `{n}`, `{sha}`, `{issue}` and the exact artifact path.\n\n")
		b.WriteString("## Protocol\n\n")
		b.WriteString("1. Read `TASK.md` and then `CONTEXT.md` in the run directory (`$HERD_RUN_DIR`) before anything else.\n")
		b.WriteString("2. Work in the current directory: it is the task's own git worktree.\n")
		b.WriteString("3. Never commit or push: herd does it.\n")
		fmt.Fprintf(&b, "4. Write your artifact (`%s`) in the run directory. It starts with YAML front matter; when the step is finished it contains `%s: %s`.\n", ag.Writes, key, strings.Join(values, " | "))
		b.WriteString("   Only add that line when the step is truly finished. If you need the user, ask in the terminal and stop.\n\n")
		b.WriteString("## Instructions\n\n")
		b.WriteString(strings.TrimSpace(ag.Instructions))
		b.WriteString("\n")
		if err := os.WriteFile(filepath.Join(dir, "commands", name+".md"), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}
