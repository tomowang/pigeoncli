# pigeon

pigeon is a local email client for the terminal, built on
[bubbletea](https://github.com/charmbracelet/bubbletea). It syncs IMAP
mailboxes into a local SQLite cache so browsing works offline, and sends
mail over SMTP. A CLI covers account setup, sync, signatures, reading,
searching, composing, and replying; a TUI covers day-to-day reading and
composing. An HTTP API is planned as a third frontend.

## Status

Early scaffold (Phase 0). Expect missing features and rough edges — see
[AGENTS.md](./AGENTS.md) for the architecture and phased build plan.

## Install

With [Homebrew](https://brew.sh) (macOS and Linux):

```
brew install tomowang/tap/pigeon
```

With Go 1.27+:

```
go install github.com/tomowang/pigeoncli/cmd/pigeon@latest
```

Or build from source:

```
git clone https://github.com/tomowang/pigeoncli
cd pigeoncli
make build   # -> bin/pigeon
```

## Quick start

```
pigeon account add work \
  --email you@example.com \
  --imap-host imap.example.com \
  --smtp-host smtp.example.com
# prompts for the account password, stored in the OS keyring

pigeon account test work   # verify IMAP/SMTP login
pigeon sync work           # pull folders and headers into the local cache
pigeon                     # launch the TUI
```

Passwords are never written to disk in plaintext — they're stored via the
OS keyring (`internal/auth`). Connection settings (host, port, TLS,
username) live in a TOML config file.

### Gmail

Gmail accounts sign in with Google OAuth2 instead of a password:

```
pigeon account add personal --email you@gmail.com --google
# opens your browser to sign in with Google; IMAP/SMTP default to Gmail's servers

pigeon account test personal
pigeon sync personal
```

The OAuth2 token (not a password) is stored in the OS keyring; pigeon
refreshes it automatically. If Google revokes access (e.g. after months of
inactivity), re-run sign-in with `pigeon account edit personal
--reauth-google`. In the TUI, pick "Google" under Sign in with on the add-
account form instead of entering a password.

## CLI

```
pigeon                          # launch the TUI (default, no subcommand)
pigeon version                  # print build info

pigeon account add <slug>       # add an account (prompts for password)
pigeon account add <slug> --google           # add a Gmail account via Google OAuth2 sign-in
pigeon account edit <slug>      # edit an account's settings
pigeon account edit <slug> --reauth-google   # re-run Google sign-in for a google-auth account
pigeon account list             # list configured accounts
pigeon account remove <slug>    # remove an account
pigeon account test <slug>      # test IMAP/SMTP connectivity

pigeon sync [slug]               # sync one account, or all if omitted
pigeon sync [slug] --full        # ignore the initial sync window, sync full history

pigeon signature add <name>      # add a signature (--account, --body, --default)
pigeon signature list            # list signatures (--account to filter)
pigeon signature edit <id>       # edit a signature's name/body
pigeon signature remove <id>     # remove a signature
pigeon signature set-default <id> # make a signature the default in its scope

pigeon message list <slug> [folder]        # list cached messages (default folder: INBOX)
pigeon message show <slug> <uid>           # show a message's headers and body (--folder)
pigeon message search <slug> <query>       # full-text search a synced account's headers
pigeon message spam <slug> <uid>           # report a message as spam (moves to Junk)
pigeon message unspam <slug> <uid>         # undo a spam report (moves back to Inbox)
pigeon message attachment save <slug> <uid> <index> <dest-path>  # save an attachment to disk

pigeon folder list <slug>        # list an account's synced folders

pigeon compose send <slug> --to <addr>     # compose and send a new message (--cc, --subject, --body)
pigeon compose reply <slug> <uid>          # reply to a cached message (--all, --folder, --subject, --body)
```

Global flags (any subcommand): `--config`, `--db`, `--blobs`, `--log-level`,
`--log-file`. Each defaults to the XDG config/cache directories when unset.

## TUI keybindings

| Key | Action |
|---|---|
| `tab` | switch pane (accounts / folders / messages) |
| `↑/↓`, `j/k` | move selection, scroll |
| `enter` | open selection |
| `a` | add a new account |
| `c` | compose a new message |
| `s` | sync the selected account (also runs automatically in the background) |
| `/` | search subject/from/to/cc for the selected account |
| `?` | toggle help |
| `L` | toggle the status log |
| `q` / `ctrl+c` | quit (asks to confirm); press `ctrl+c` twice to quit immediately |

In the message viewer: `r` reply, `R` reply-all, `t` toggle raw/rendered,
`g` go to related messages, `a` save an attachment, `esc` back to list.

Press `?` inside the TUI for the full, context-aware list.

## Development

```
make build   # build ./cmd/pigeon -> bin/pigeon
make run     # go run ./cmd/pigeon
make test    # go test ./...
make vet     # go vet ./...
make lint    # golangci-lint run
make tidy    # go mod tidy
```

The TUI requires a real TTY; it won't run under a piped or backgrounded
shell.

See [AGENTS.md](./AGENTS.md) for the full architecture, package layout,
and commit conventions.

## License

[MIT](./LICENSE)
