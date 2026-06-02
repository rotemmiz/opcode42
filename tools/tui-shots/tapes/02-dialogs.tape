# 02-dialogs.tape — overlay/dialog surfaces.
# Leader is Ctrl+X. Opens each dialog, screenshots, Escape.
# forge-tui keybindings (handleLeaderKey in model.go):
#   ctrl+x l  → sessions     (scene 19)
#   ctrl+x m  → models       (scene 17)
#   ctrl+x a  → agents       (scene 20)
#   ctrl+x g  → timeline     (scene 22)
#   ctrl+x s  → status       (scene 23)
#   ctrl+p    → command palette (scene 16)
# Theme list: opened via /themes slash command (scene 18)
Output out/_02.gif
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

# command palette (Ctrl+P)
Ctrl+P
Sleep 800ms
Screenshot out/16-command-palette.png
Escape
Sleep 400ms

# model list (ctrl+x m)
Ctrl+X
Sleep 250ms
Type "m"
Sleep 800ms
Screenshot out/17-model-list.png
Escape
Sleep 400ms

# theme list (via /themes slash command)
Type "/themes"
Sleep 400ms
Enter
Sleep 800ms
Screenshot out/18-theme-list.png
Escape
Sleep 400ms

# session list (ctrl+x l)
Ctrl+X
Sleep 250ms
Type "l"
Sleep 800ms
Screenshot out/19-session-list.png
Escape
Sleep 400ms

# agent list (ctrl+x a)
Ctrl+X
Sleep 250ms
Type "a"
Sleep 800ms
Screenshot out/20-agent-list.png
Escape
Sleep 400ms

# timeline (ctrl+x g)
Ctrl+X
Sleep 250ms
Type "g"
Sleep 800ms
Screenshot out/22-timeline.png
Escape
Sleep 400ms

# status (ctrl+x s)
Ctrl+X
Sleep 250ms
Type "s"
Sleep 800ms
Screenshot out/23-status.png
Escape
Sleep 400ms
