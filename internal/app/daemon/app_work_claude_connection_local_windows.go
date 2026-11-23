package daemon

// Native Windows keeps project-local settings in the starting directory.
func claudeConnectionLocalRoot(cwd, home string) string { return "" }
