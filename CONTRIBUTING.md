# Contributing

Issues and pull requests are welcome.

## Before you open a pull request

```sh
gofmt -l .
go vet ./...
go test ./...
```

CI runs the same checks plus `staticcheck` on Linux, macOS and Windows.

## Guidelines

- Keep the module pure Go: no cgo, no external processes, no network access during conversion.
- Add a test for every behaviour change. For PDF layout heuristics, prefer a golden
  test in `internal/pdfnative/render_golden_test.go` and regenerate with
  `go test ./internal/pdfnative/ -update-golden`. Review the golden diff.
- Parsers read untrusted input: bound every loop, allocation and recursion by the
  input size or an explicit cap, and run `go test -run '^$' -fuzz FuzzConvert -fuzztime 2m .`
  after touching one.
- Comments explain why, not what: a doc comment on every exported identifier, and
  inline comments only for heuristics, format quirks and limits.
- Do not commit real-world documents as fixtures. Build test inputs in code from
  invented text, so nothing confidential or copyrighted ends up in the repository.
- New dependencies need a permissive license compatible with Apache-2.0.

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
