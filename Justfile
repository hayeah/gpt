wire:
    go run github.com/google/wire/cmd/wire .

run: wire
    CONFIG_FILE=cfg.toml go run ./cmd/cli

dev:
    go run github.com/cortesi/modd/cmd/modd@latest
