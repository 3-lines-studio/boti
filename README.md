# boti

The generic [Botdir](https://github.com/3-lines-studio/ax-ecosystem/blob/main/BOTDIR.md) host
loader for the AX ecosystem. Given a bot root, it reads `bot.toml`, installs the
consumer, engine (`ax`), and each declared tool binary, wires the standard
Botdir environment, runs the bot's `[[pre_run]]` hooks, then starts the declared
`consumer`.

```
boti install   # read bot.toml, install any missing tool binaries (build-time)
boti           # wire the bot root, run [[pre_run]] hooks, exec the consumer
```

## Use

A bot's container entrypoint is `boti`. Build the image so the tools are
baked in at build time, then start the consumer:

```dockerfile
COPY bot.toml bot.md /bot/
COPY skills /bot/skills
RUN boti install   # installs ax, slaxi, and the tools in bot.toml
CMD ["boti"]
```

`boti install` installs the missing tools through the shared installer
(`https://ax.3lines.studio/install.sh`). It honors `AX_PREFIX`, `AX_VERSION`
(for `ax`), and `VERSION` (for the other tools) the same way the installer does,
so images can pin versions.

`boti` exports the bot root as the single `BOT_ROOT` anchor; consumers and tools
derive the standard paths (`workspace/`, `skills/`, `state/`, `run/`,
`secrets/`, `bot.md`) from it by convention. It also exports any `[env]` table
declared in `bot.toml` (host env wins), runs each `[[pre_run]]` hook in order,
then replaces itself with the declared `consumer` (default `slaxi`).

## `bot.toml` keys the loader reads

```toml
consumer = "slaxi"      # primary consumer to launch (default slaxi)

[env]                   # exported for the consumer; host env wins
WAX_NO_SANDBOX = "1"

[[pre_run]]             # run before the consumer, in order
command = "refresh-skill --name metrics"
```

- `consumer` — one executable name resolved through `PATH`. The loader execs it.
- `[env]` — literal environment variables exported (host env wins).
- `[[pre_run]]` — shell commands run with the bot environment set. A non-zero
  exit aborts startup unless the command handles it.

## Modes

- `install` — read `Root/bot.toml`; install `ax`, `slaxi`, and any declared tool
  that is not already on `PATH`. Requires `bot.toml`. Used at image build time so
  startup needs no network.
- `run` (default) — wire the bot root, run `[[pre_run]]` hooks, then `exec
  consumer`.

## Development

```sh
cd boti
go test
go build -trimpath -ldflags='-s -w' -o boti .
```

Install it like any other ecosystem CLI once a `3-lines-studio/boti` release
exists:

```sh
curl -fsSL https://ax.3lines.studio/install.sh | sh -s -- boti
```
