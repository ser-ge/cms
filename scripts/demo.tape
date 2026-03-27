# cms demo recording
# Placeholders (__FOO__) are substituted by scripts/vhs-record.sh

Output "__OUTPUT__"

Set Shell "bash"
Set Width __WIDTH__
Set Height __HEIGHT__
Set FontSize __FONT_SIZE__
Set Theme "__THEME__"
Set Padding 20
Set TypingSpeed 60ms
Set WindowBar Colorful

# Isolated cms environment
Env CMS_CONFIG_DIR "__CMS_CONFIG_DIR__"
Env CMS_TMUX_SOCKET "__CMS_TMUX_SOCKET__"

# Add built cms to PATH and clear screen (hidden from recording)
Hide
Type "export PATH=__BIN_DIR__:$PATH"
Enter
Sleep 500ms
Type "clear"
Enter
Sleep 300ms
Show

# Launch cms fuzzy finder
Sleep 300ms
Type "cms"
Sleep 500ms
Enter
Sleep 3s

# Scroll through items
Down
Sleep 400ms
Down
Sleep 400ms
Down
Sleep 400ms
Down
Sleep 400ms
Up
Sleep 400ms
Up
Sleep 600ms

# Type a filter query
Type "project"
Sleep 1.5s

# Clear filter, try another
Ctrl+U
Sleep 800ms
Type "feat"
Sleep 1.5s

# Select an item
Down
Sleep 400ms
Enter
Sleep 1s
