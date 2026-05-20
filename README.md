# check-spec

> 監督阿爾宙斯的計畫 — a third-party auditor for the Arceus spec-driven workflow.

`check-spec` is a Go CLI (and accompanying GitHub Action) that audits whether
code committed under an [Arceus](https://github.com/mikeqoo1/arceus) change
proposal actually matches the `spec.md` and `tasks.md` that were promised.

It reads `.arceus/changes/<id>/`, collects the git diff between two refs, and
hands both to Claude as an independent judge — the judge then returns a
structured verdict you can audit, diff, and gate PRs on.

## Why

The Arceus workflow runs `propose → apply → review` end-to-end with AI
agents. The `coder` agent self-reports task completion by ticking the
checkbox in `tasks.md`, but nothing actually verifies that the code matches
the spec. `check-spec` fills that gap.

|                                          | Spec Kit | OpenSpec | check-spec |
| ---                                      | :---:    | :---:    | :---:      |
| Generates code from spec                 | ✓        | ✓        |            |
| Cross-document spec consistency          | ✓        |          |            |
| **Verifies spec ↔ actual code**          |          |          | ✓          |
| Third-party (does not also implement)    |          |          | ✓          |
| Self-hostable Go CLI                     |          |          | ✓          |

## Install

```bash
# from source (Go 1.23+)
go install github.com/mikeqoo1/check-spec/cmd/check-spec@latest

# or grab a pre-built binary from the Releases page
# https://github.com/mikeqoo1/check-spec/releases
```

## Quick start

```bash
export ANTHROPIC_API_KEY=sk-ant-...

# Inside a repo that has .arceus/changes/<id>/
check-spec verify \
  --change 2026-05-20-my-change \
  --base origin/main \
  --head HEAD
```

Output is a markdown audit report on stdout. Exit codes:

| Code | Meaning                                                           |
| ---: | ---                                                               |
| 0    | Verdict `APPROVE` with no findings — implementation matches spec. |
| 1    | Verdict not `APPROVE`, or implementation has drift / partial tasks. |
| 2    | Execution error (missing API key, ref not found, etc.).           |

## Report structure

```
# Spec/Code Consistency Audit — <change-id>

- **Verdict**: APPROVE | REQUEST_CHANGES | NEEDS_DISCUSSION
- **Model**:  claude-opus-4-7
- **Base → Head**: origin/main (abc123) → HEAD (def456)
- **Files analyzed**: N

## Summary
...

## Task implementation (from tasks.md)
| # | Phase | Task | Reported | Actual | Evidence |
...

## Acceptance criteria (from spec.md)
- PASS: criterion 1 — ...
- FAIL: criterion 2 — ...

## Drift findings
- Undocumented additions: ...
- Missing from implementation: ...

## Open questions for human reviewer
- ...
```

Need machine-readable output for tooling? Use `--format json`. The JSON
schema lives at [`internal/report/schema.json`](internal/report/schema.json).

## GitHub Action

For PR-time auditing, add this to `.github/workflows/check-spec.yml`:

```yaml
name: check-spec audit
on:
  pull_request:
    paths: ['.arceus/changes/**', '**/*.go']
permissions:
  contents: read
  pull-requests: write
jobs:
  audit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: mikeqoo1/check-spec@v0
        with:
          anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

By default the action auto-detects the change-id from the PR diff, posts a
sticky comment with the report, and fails the check when findings are
present. See [`action.yml`](action.yml) for all inputs.

## CLI reference

```
check-spec verify [flags]

Flags:
  --change          string   change id under .arceus/changes/ (required)
  --base            string   base git ref (default "origin/main")
  --head            string   head git ref (default "HEAD")
  --repo            string   repo root (default ".")
  --output          string   write report to file (default stdout)
  --format          string   markdown | json (default "markdown")
  --model           string   LLM model id (default "claude-opus-4-7")
  --max-diff-bytes  int      soft cap on diff payload (default 200000)
  -v, --verbose              enable debug logging
```

```
check-spec list      # list change ids under .arceus/changes/
check-spec version   # print version, commit, build date
```

## How it works

1. **Load** the change proposal: parse `proposal.md`, `spec.md`, `tasks.md`,
   `decisions.md`, plus the `meta.json` status block.
2. **Collect** the diff: shell-out to `git diff <base>..<head>`,
   split per file, exclude binaries, soft-cap by size (vendor / generated
   files are deprioritized first).
3. **Prompt**: build a two-segment user prompt — proposal + spec
   (cacheable across calls within Anthropic's 5-minute TTL) and
   tasks + decisions + diff (dynamic).
4. **Judge**: send to the Anthropic Messages API. The model is instructed
   to return a single JSON object covering per-task and per-criterion
   verdicts plus drift findings.
5. **Render**: hydrate the verdict against the original task / criterion
   text (so the report has full context), then output markdown or JSON.

## Privacy note

`check-spec` sends `proposal.md`, `spec.md`, `tasks.md`, `decisions.md`,
**and the git diff** to the Anthropic API on every run. Do not point it at
private code unless you are comfortable with that data leaving your
network and being subject to Anthropic's data handling policy.

The CLI never writes anywhere except `--output` (when set) and stderr
(for logs).

## Development

```bash
make verify        # lint + test (race) + build
make test          # tests only
make lint          # golangci-lint
make build         # produces ./check-spec
```

Architecture:

```
cmd/check-spec/        thin main, calls cli.Execute
internal/cli/          cobra wiring + E2E tests using FakeJudge
internal/arceus/       reads .arceus/changes/<id>/
internal/gitdiff/      shells out to git diff
internal/prompt/       builds system + user prompt segments
internal/llm/          Judge interface + AnthropicJudge + FakeJudge
internal/report/       Report struct + markdown/JSON renderers + schema.json
internal/version/      ldflags-populated version constants
```

## License

[MIT](LICENSE)
