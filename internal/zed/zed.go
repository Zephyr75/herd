// Package zed opens folders in Zed.
package zed

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Open runs the configured Zed command (e.g. "zed" or "zeditor --new") on
// path without waiting for it.
func Open(command, path string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return errors.New("no Zed command configured (Settings > Appearance)")
	}
	bin, err := exec.LookPath(fields[0])
	if err != nil {
		return fmt.Errorf("%s is not on PATH (set the Zed command in Settings > Appearance)", fields[0])
	}
	cmd := exec.Command(bin, append(fields[1:], path)...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// OpenFile opens a single file in the existing Zed window (-e) rather than a
// new one, unless the command already chooses (-n, -e, -a).
func OpenFile(command, path string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return Open(command, path)
	}
	for _, f := range fields[1:] {
		switch f {
		case "-n", "--new", "-e", "--existing", "-a", "--add":
			return Open(command, path)
		}
	}
	return Open(command+" -e", path)
}

// Available reports whether the Zed command is on PATH.
func Available(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	_, err := exec.LookPath(fields[0])
	return err == nil
}
