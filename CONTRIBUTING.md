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

You need Go 1.24 or newer. The library has no dependencies. The CLI in `cmd/rustyon` is a separate Go module with its own `go.mod` (it needs YAML and TOML libraries), tied to the library by the committed `go.work` file. Inside the workspace the CLI always builds against your local copy of the library, so a change to the library and the CLI can go in one commit.

```sh
git clone https://github.com/peltonapp/rustyon-go.git
cd rustyon-go
make test
```

Because `cmd/rustyon` is its own module, `go test ./...` at the repository root only covers the library. Run it in `cmd/rustyon` as well, or use the `make` targets, which cover both. `go.work` is ignored by `go install pkg@version` and by consumers of the library, so the CLI may only use library API that exists in the version required by `cmd/rustyon/go.mod` when it is released.

Useful targets:

| Command          | What it does                                            |
| ---------------- | ------------------------------------------------------- |
| `make test`      | run all tests with the race detector, library and CLI   |
| `make cover`     | write `coverage.html` (library) and the CLI coverage profile |
| `make lint`      | run `gofmt`, `go vet` and `golangci-lint` on both modules |
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
cmd/rustyon/         the CLI, a separate Go module (go.mod, go.sum)
testdata/            golden files for the formatter
go.work              workspace joining the library and the CLI
```

## Pull requests

- Add tests for new behavior and for every bug fix. Formatter changes need a golden file in `testdata/format` (`go test -run TestFormatGolden -update` regenerates them, check the diff).
- Keep the public API small. New exported identifiers need doc comments.
- Run `make check` before pushing.
- Commit messages: one line, imperative mood, for example `Fix decoding of raw identifiers`.
- No em dashes or en dashes in code, comments, docs or commit messages; plain hyphens are fine.

## Releasing (maintainers)

The repository holds two Go modules with separate tags:

| Module                                          | Tag                    | Result                                  |
| ----------------------------------------------- | ---------------------- | --------------------------------------- |
| `github.com/peltonapp/rustyon-go` (library)     | `vX.Y.Z`               | GitHub release without binaries         |
| `github.com/peltonapp/rustyon-go/cmd/rustyon`   | `cmd/rustyon/vX.Y.Z`   | GitHub release with CLI binaries        |

The release workflow reacts to both. GoReleaser OSS has no monorepo tag support, so for CLI tags the workflow passes the plain version in `GORELEASER_CURRENT_TAG`, lets GoReleaser build and archive (`--skip=publish,validate`), and uploads the archives with `gh release create`. CLI releases are not marked as the latest release, so the repository's latest release stays the library.

To release the CLI:

1. Release the library first if the CLI needs anything newer than the version in `cmd/rustyon/go.mod`: tag `vX.Y.Z` on main and push it.
2. Point the CLI at that version. The pinned library version must contain `cmd/rustyon/go.mod`, otherwise Go finds `cmd/rustyon` in two modules ("ambiguous import"):

   ```sh
   cd cmd/rustyon
   GOWORK=off go get github.com/peltonapp/rustyon-go@vX.Y.Z
   GOWORK=off go mod tidy
   GOWORK=off go test ./...
   ```

   Commit the result and merge it.
3. Tag the merge commit and push the tag: `git tag cmd/rustyon/vA.B.C && git push origin cmd/rustyon/vA.B.C`.
4. After the workflow finishes, `go install github.com/peltonapp/rustyon-go/cmd/rustyon@latest` installs the new version.

## Compatibility with ron-rs

rustyon aims to read and write the same documents as [ron-rs](https://github.com/ron-rs/ron) ([crate docs](https://docs.rs/ron)). If you find a document that ron-rs accepts and rustyon rejects, or the other way around, please open an issue with the input and the ron-rs version.
