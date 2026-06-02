# 01-conversation.tape — conversation surfaces.
# Launches forge-tui attached to the opencode daemon with the fixture session
# pre-loaded (ses_00000000000000000000000000). Scrolls through the transcript
# and screenshots each content region.
#
# Scenes captured (same numbering as opencode reference):
#   06-markdown-reasoning  — top of transcript: reasoning + markdown intro
#   07-tools-diff          — edit diff region
#   08-summary-table       — bottom: summary markdown + table
#   09-write-bash-todos    — write/code, bash output, todo list
Output out/_01.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 1000
Set Padding 8

Hide
Type "source ./env.sh && ./bin/forge-tui --url http://127.0.0.1:4199 --dir ./fixture --session ses_00000000000000000000000000 --theme $FORGE_THEME"
Enter
Sleep 7s
Show
Sleep 1s

# Bottom of transcript (default scroll position is newest): summary + table + todos
Screenshot out/08-summary-table.png

# Jump to top: user msg + reasoning + markdown intro
Ctrl+G
Sleep 1500ms
Screenshot out/06-markdown-reasoning.png

# Page down into tool region: grep / glob / edit diff
PageDown
Sleep 1200ms
Screenshot out/07-tools-diff.png

# Next page: write (syntax-highlighted code) + bash output + todos
PageDown
Sleep 1200ms
Screenshot out/09-write-bash-todos.png
