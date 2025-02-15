package main

// String vars that are free to be changed at compile time
var (
	version = "v0.6.0"
	defaultConfDir = "" // ~ will be substituted for home, path for user's default config
	defaultHistDir = ""
	commonRequirePaths = "';./libs/?/init.lua;./?/init.lua;./?/?.lua'"

	prompt string // Prompt will always get changed anyway
	multilinePrompt = "> "
)

// Flags
var (
	running bool // Is a command currently running
	interactive bool
	login bool // Are we the login shell?
	noexecute bool // Should we run Lua or only report syntax errors
)

