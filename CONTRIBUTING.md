# Contributing to rustyon

Thanks for helping out! Bug reports, fixes, docs and new features are all welcome.

## Ground rules

- Be kind. This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).
- For anything bigger than a small fix, open an issue first so we can agree on the approach.
- Report security problems privately, see [SECURITY.md](SECURITY.md).

## Developer Certificate of Origin (DCO)

Every commit must be signed off. By signing off you certify that you wrote the change or otherwise have the right to submit it under the project's license, as described in the [Developer Certificate of Origin](DCO).

Sign off by adding `-s` to `git commit`:

```sh
git commit -s -m "Add support for X"
```

This adds a trailer with your name and email:

```
Signed-off-by: Jane Doe <jane@example.com>
```

The name and email must match the commit author. A CI check rejects pull requests with commits that are not signed off. To fix the last commit, run `git commit --amend -s`; for several commits, `git rebase --signoff main`.

## License and SPDX headers

rustyon is licensed under [BSD-3-Clause](LICENSE). Contributions are accepted under the same license.

Every source file starts with an [SPDX](https://spdx.dev/learn/handling-license-info/) header, followed by a blank line so it does not become the package documentation:

```go
// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon
```

For YAML files and the Makefile use `#` comments. Keep the existing copyright line in files you edit. In new files you may use your own name in `SPDX-FileCopyrightText`. `make license` checks that every file has a header.

## Development setup

You need Go 1.24 or newer. The library has no dependencies.

```sh
git clone https://github.com/peltonapp/rustyon-go.git
cd rustyon-go
make test
```

Useful targets:

| Command          | What it does                                            |
| ---------------- | ------------------------------------------------------- |
| `make test`      | run all tests with the race detector                    |
| `make cover`     | write `coverage.html`                                   |
| `make lint`      | run `gofmt`, `go vet` and `golangci-lint`               |
| `make fuzz`      | run every fuzz target for `FUZZTIME` (default 30s)      |
| `make bench`     | run the benchmarks, including the `encoding/json` comparison |
| `make license`   | check SPDX headers                                      |
| `make build`     | build the `rustyon` CLI into `./bin`                    |
| `make check`     | everything CI runs                                      |

## Project layout

```
.                    public API: Marshal, Unmarshal, Value, Registry, Format
internal/syntax/     lexer, literal decoding, comment preserving formatter
internal/fields/     struct field reflection and caching
ronjson/             RON and JSON conversion
cmd/rustyon/         the CLI
testdata/            golden files for the formatter
```

## Pull requests

- Add tests for new behavior and for every bug fix. Formatter changes need a golden file in `testdata/format` (`go test -run TestFormatGolden -update` regenerates them, check the diff).
- Keep the public API small. New exported identifiers need doc comments.
- Run `make check` before pushing.
- Commit messages: one line, imperative mood, for example `Fix decoding of raw identifiers`.
- No em dashes or en dashes in code, comments, docs or commit messages; plain hyphens are fine.

## Compatibility with ron-rs

rustyon aims to read and write the same documents as [ron-rs](https://github.com/ron-rs/ron) ([crate docs](https://docs.rs/ron)). If you find a document that ron-rs accepts and rustyon rejects, or the other way around, please open an issue with the input and the ron-rs version.
