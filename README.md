# boti

The [Botdir](https://github.com/3-lines-studio/ax-ecosystem/blob/main/BOTDIR.md) host loader for the AX ecosystem. Given a bot root, it reads `bot.toml`, installs the consumer, engine, and every declared tool binary, wires the standard Botdir environment, runs the bot's `[[pre_run]]` hooks, then execs the declared consumer (default `slaxi`).

```sh
boti install   # read bot.toml, install any missing binaries (build-time)
boti           # wire the bot root, run [[pre_run]] hooks, exec the consumer
```

## Use

A bot container entrypoint is `boti`:

```dockerfile
COPY bot.toml bot.md /bot/
COPY skills /bot/skills
RUN boti install
CMD ["boti"]
```

`boti install` installs missing tools through `https://ax.3lines.studio/install.sh`, honoring `AX_PREFIX`, `AX_VERSION` (for `ax`), and `VERSION` (for other tools). `boti` exports `BOT_ROOT` as the single anchor; consumers and tools derive `workspace/`, `skills/`, `state/`, `run/`, `secrets/`, `bot.md` from it by convention.

## `bot.toml`

```toml
consumer = "slaxi"

[env]
WAX_NO_SANDBOX = "1"

[[pre_run]]
command = "refresh-skill --name metrics"
```

- `consumer` — executable resolved through `PATH`; the loader execs it.
- `[env]` — literal environment variables exported; host env wins.
- `[[pre_run]]` — commands run in order before the consumer; a non-zero exit aborts startup.

## Development

```sh
cd boti
go test ./...
go build -trimpath -ldflags='-s -w' -o boti .
```
