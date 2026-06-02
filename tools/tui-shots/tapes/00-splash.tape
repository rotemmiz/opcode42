# 00-splash.tape — true splash/home screen.
# Spins up a second, empty opencode daemon on port 4200 (no sessions imported)
# so forge-tui has nothing to auto-open and displays the splash/wordmark.
#
# Scenes captured:
#   00-splash   — forge wordmark + empty composer (Tier 0 bg proof: fully painted)
Output out/_00.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 1000
Set Padding 8

Hide
Type "source ./env.sh"
Enter
Sleep 200ms
# Start an empty daemon (separate state dir → no sessions) on port 4200
Type "XDG_DATA_HOME=$HARNESS_DIR/state/empty-data XDG_CONFIG_HOME=$HARNESS_DIR/state/empty-config XDG_CACHE_HOME=$HARNESS_DIR/state/empty-cache XDG_STATE_HOME=$HARNESS_DIR/state/empty-xstate opencode serve --port 4200 >/dev/null 2>&1 &"
Enter
Sleep 5s
Type "./bin/forge-tui --url http://127.0.0.1:4200 --dir ./fixture-empty --theme $FORGE_THEME"
Enter
Sleep 4s
Show
Sleep 1s

# True splash: forge wordmark + empty composer + fully painted background
Screenshot out/00-splash.png
