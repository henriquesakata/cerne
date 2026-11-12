# Source of `docs/assets/demo.gif`

The README demo is rendered, not screen-recorded: `demo.html` draws a terminal and replays a script of commands with their real output (cerne v0.5.0, trimmed where marked with `…`); `frames.mjs` screenshots each frame through headless Chrome; ffmpeg assembles the GIF.

When the CLI output changes, re-run the commands, update the `scenes` array in `demo.html`, and regenerate:

```bash
chrome --headless=new --remote-debugging-port=9333 about:blank &
mkdir -p /tmp/frames && node frames.mjs "$PWD/demo.html" /tmp/frames
cd /tmp/frames && ffmpeg -y -f concat -safe 0 -i list.txt \
  -vf "split[a][b];[a]palettegen=max_colors=64[p];[b][p]paletteuse=dither=none" \
  -fps_mode vfr demo.gif
```

Needs Node 22+ (built-in `WebSocket`), Chrome and ffmpeg.
