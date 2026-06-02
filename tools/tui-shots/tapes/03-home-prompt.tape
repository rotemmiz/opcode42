# 03-home-prompt.tape — entry + prompt surfaces.
# Launches forge-tui against the opencode daemon (seeded fixture session).
# The theme is pinned via --theme flag so capture is deterministic.
# Scene numbering matches opencode's reference harness.
#
# Scenes captured:
#   03-home-empty   — splash: wordmark + empty composer + footer + sidebar
#   04-prompt-text  — composer with typed text
#   15-slash-commands — slash command menu
Output out/_03.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 1000
Set Padding 8

Hide
Type "source ./env.sh && ./bin/forge-tui --url http://127.0.0.1:4199 --dir ./fixture --theme $FORGE_THEME"
Enter
Sleep 5s
Show
Sleep 1s

# home screen: wordmark + empty composer + chrome
Screenshot out/03-home-empty.png

# typed prompt text
Type "Refactor the auth module and add an expiry test"
Sleep 600ms
Screenshot out/04-prompt-text.png

# slash command menu (/ in composer)
Ctrl+A
Type "/"
Sleep 900ms
Screenshot out/15-slash-commands.png
Escape
Sleep 300ms
