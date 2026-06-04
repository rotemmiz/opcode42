package lsp

import "path/filepath"

// languageExtensions maps a file extension to the LSP languageId sent in
// textDocument/didOpen. The values are ported verbatim from opencode's
// LANGUAGE_EXTENSIONS (lsp/language.ts); extensions absent there (e.g. .h, .pyi)
// fall back to "plaintext" exactly as opencode does (lsp/client.ts:601).
var languageExtensions = map[string]string{
	".go":      "go",
	".ts":      "typescript",
	".tsx":     "typescriptreact",
	".mts":     "typescript",
	".cts":     "typescript",
	".js":      "javascript",
	".jsx":     "javascriptreact",
	".mjs":     "javascript",
	".cjs":     "javascript",
	".py":      "python",
	".rb":      "ruby",
	".rake":    "ruby",
	".gemspec": "ruby",
	".ru":      "ruby",
	".rs":      "rust",
	".c":       "c",
	".cpp":     "cpp",
	".cc":      "cpp",
	".cxx":     "cpp",
	".c++":     "cpp",
	".dart":    "dart",
	".php":     "php",
	".ml":      "ocaml",
	".mli":     "ocaml",
	".sh":      "shellscript",
	".bash":    "shellscript",
	".zsh":     "shellscript",
	".ksh":     "shellscript",
	".tf":      "terraform",
	".tfvars":  "terraform-vars",
	".gleam":   "gleam",
	".clj":     "clojure",
	".cljs":    "clojure",
	".cljc":    "clojure",
	".edn":     "clojure",
	".nix":     "nix",
}

// languageIDFor returns the LSP languageId for a file path, or "plaintext".
func languageIDFor(file string) string {
	if id, ok := languageExtensions[filepath.Ext(file)]; ok {
		return id
	}
	return "plaintext"
}
