package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"
)

//go:embed init.tmpl
var initTemplate string

func cmdInit(args []string) int {
	if len(args) != 1 {
		return die(2, "usage: mundane init <task.db>")
	}
	dbPath := args[0]
	abs, err := absoluteOrPath(dbPath)
	if err != nil {
		return die(1, "%v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		// fall back to argv[0] so tests can still run uninstalled
		exe = "mundane"
	}

	t, err := template.New("init").Parse(initTemplate)
	if err != nil {
		return die(1, "parse init template: %v", err)
	}
	data := map[string]string{
		"DB":      shellQuote(abs),
		"DBRaw":   abs,
		"Exe":     shellQuote(exe),
		"LockFD":  "9",
	}
	if err := t.Execute(os.Stdout, data); err != nil {
		return die(1, "render init template: %v", err)
	}
	return 0
}

func absoluteOrPath(p string) (string, error) {
	if strings.HasPrefix(p, "/") {
		return p, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getcwd: %w", err)
	}
	return cwd + "/" + p, nil
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
