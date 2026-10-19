# Go CI action

Run blocking formatting, module tidiness, vet, tests, build, and lint checks.
This action does not check out code, create tags, sign binaries, or publish
releases. Shared workflows compose it with the separate GitVersion and
GoReleaser actions.

```yaml
steps:
  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
    with:
      persist-credentials: false
  - uses: michielvha/go-ci-action@FULL_COMMIT_SHA
```

Replace the example SHA placeholder with a verified commit before use.

## Inputs

| Input | Default | Purpose |
| --- | --- | --- |
| `working-directory` | `.` | Go module directory |
| `go-version-file` | `go.mod` | Version file relative to the module |
| `golangci-lint-config` | `.golangci.yml` | Lint config relative to the module |

The `go-version` output contains the resolved toolchain version. A subsequent
release action can use it rather than selecting a different toolchain.

All checks block on failure. Formatting and module checks do not modify source.
Build output is discarded instead of leaving an executable in the checkout.
The action uses Bash and supports Linux and macOS runners.

## Tests

Run `go test ./...` with Go and golangci-lint available. The tests read the action
manifest using `go.yaml.in/yaml/v3` and execute the actual
commands from the composite against isolated Go fixtures and cover failures as
well as a passing module. The GitHub workflow also executes the full composite,
including tool setup.
