package lsp

import "path/filepath"

// languageExtensions maps a file extension to the LSP languageId sent in
// textDocument/didOpen. It covers the foundation subset's servers (gopls,
// typescript, pyright); unknown extensions fall back to "plaintext", matching
// opencode's LANGUAGE_EXTENSIONS lookup (lsp/language.ts, lsp/client.ts:601).
var languageExtensions = map[string]string{
	".go":  "go",
	".ts":  "typescript",
	".tsx": "typescriptreact",
	".mts": "typescript",
	".cts": "typescript",
	".js":  "javascript",
	".jsx": "javascriptreact",
	".mjs": "javascript",
	".cjs": "javascript",
	".py":  "python",
}

// languageIDFor returns the LSP languageId for a file path, or "plaintext".
func languageIDFor(file string) string {
	if id, ok := languageExtensions[filepath.Ext(file)]; ok {
		return id
	}
	return "plaintext"
}
