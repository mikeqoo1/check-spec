# Tasks — v0.1 MVP — LLM-judged spec/code consistency auditor

_實作階段的 checklist。arceus:coder 會依序處理並打勾回報。每個 task 必須是可獨立驗證的。_

## Phase 0 — Repo bootstrap

- [x] 初始化 `go mod init github.com/mikeqoo1/check-spec`
- [x] 建立目錄骨架：`cmd/check-spec/`、`internal/arceus/`、`internal/gitdiff/`、`internal/llm/`、`internal/report/`、`internal/prompt/`、`testdata/`
- [x] 加入 `.golangci.yml`（啟用 `errcheck`、`govet`、`staticcheck`、`gosec`、`revive`）
- [x] 加入 `Makefile`：`make build`、`make test`、`make lint`、`make verify`
- [x] 加入 `.github/workflows/ci.yml`：on push/PR 跑 `make verify`

## Phase 1 — Arceus loader (`internal/arceus`)

- [x] `type Change struct { ID, Title string; Proposal, Spec, Tasks, Decisions string; Meta MetaJSON }`
- [x] `func Load(repoRoot, changeID string) (*Change, error)` — 讀 `.arceus/changes/<id>/{proposal,spec,tasks,decisions}.md` + `meta.json`，缺檔回 error
- [x] `func List(repoRoot string) ([]string, error)` — 列出可用 change id（含 archive 排除）
- [x] 解析 `tasks.md` 為 `[]Task{Index int, Text string, Checked bool, Phase string}`（保留原始 markdown 也保留結構化）
- [x] 解析 `spec.md` 的「驗收條件」段：找 `## 驗收條件` 或 `## Acceptance criteria` heading，收集底下 `- [ ]` / `- [x]` 行
- [x] 單元測試覆蓋：正常、缺檔、空 acceptance section、混合中英文 heading

## Phase 2 — Git diff collector (`internal/gitdiff`)

- [x] `func Collect(repoRoot, base, head string, maxBytes int) (Diff, error)`，回傳 `Diff{Files []FileDiff, Truncated bool}`
- [x] 用 `exec.CommandContext` 跑 `git diff --no-color <base>..<head>` 與 `git diff --name-only`；不可信任使用者輸入 → 預先 validate ref 不含 shell metacharacters
- [x] 排除 binary file（從 `git diff --numstat` 的 `-` 標記識別）
- [x] 超過 `maxBytes` 時：按 file 切，優先保留 spec/code 相關的非 vendor、非生成檔
- [x] 單元測試：使用 `git init` 在 tmp dir 起的 fixture repo，覆蓋一般 diff、binary file 過濾、截斷情況

## Phase 3 — LLM client (`internal/llm`)

- [x] 定義介面：`type Judge interface { Verify(ctx, JudgeInput) (JudgeOutput, error) }`
- [x] Anthropic 實作 `AnthropicJudge`，使用官方 SDK；spec.md / proposal.md 段落用 `cache_control: ephemeral` 啟用 prompt caching
- [x] Mock 實作 `FakeJudge`：固定 Output 回應（測試與 golden file 用）
- [x] `Verify` 要求模型回 JSON；SDK 內建重試機制處理 429/5xx；malformed JSON 由 `ParseOutput` 容錯（去 code fence + 找 `{...}`）
- [x] 單元測試：httptest mock SDK、各種 JSON 包裝/錯誤格式、必填欄位驗證、cache_control marshalling 驗證

## Phase 4 — Prompt builder (`internal/prompt`)

- [x] `func Build(change *arceus.Change, diff *gitdiff.Diff) (system string, segments []llm.Segment)`
- [x] System prompt：角色定義 + JSON schema + verdict 規則
- [x] User prompt：XML-ish 分段（proposal / spec / tasks / decisions / git_diff），indexed tasks & criteria 列表
- [x] 對 proposal+spec 段加入 Cache=true（透過 LLM segment 機制，下游 anthropic.go 翻成 cache_control）
- [x] 單元測試：system prompt 形狀、二段切割、cache flag、索引項渲染、binary/omitted/truncated 標記

## Phase 5 — Report renderer (`internal/report`)

- [x] 定義 `Report` struct（對應 spec.md 「報告格式」段）
- [x] `func RenderMarkdown(r Report) string`
- [x] `func RenderJSON(r Report) ([]byte, error)`，含 `internal/report/schema.json`
- [x] 處理空 case：無 task / 無 acceptance criteria / 無 drift 時仍能渲染
- [x] `From()` 將 LLM Output 與 arceus.Change 依索引 join，未匹配索引保留資料（避免幻覺資料消失）
- [x] `HasFindings()` 給 CLI exit code 用
- [x] 單元測試：成節判定、markdown 內容、pipe 字元 escape、JSON round-trip、empty cases

## Phase 6 — CLI wiring (`cmd/check-spec`)

- [x] cobra root command + 子命令 `verify` / `list` / `version`
- [x] `verify` 串接：arceus.Load → gitdiff.Collect → prompt.Build → llm.Verify → report.Render → output
- [x] Exit codes：0 / 1 (drift) / 2 (error) — 對應 spec.md 定義
- [x] 用 `slog` 結構化 log，預設 INFO，`--verbose` 切 DEBUG
- [x] E2E 測試：FakeJudge 跑 `verify`、pass/drift/not-found/json/output-file/invalid-format/judge-error/list/version 共 10 個 E2E 通過
- [x] App struct 設計讓 JudgeFactory 可注入（測試可注入 FakeJudge，prod 用 AnthropicJudge）

## Phase 7 — GitHub Action

- [x] `action.yml`（composite action）放在 repo 根目錄
- [x] 步驟：download release binary（gh release download）→ run `check-spec verify` → `marocchino/sticky-pull-request-comment` 貼 comment
- [x] 輸入：`change-id`、`base-ref`、`head-ref`、`model`、`max-diff-bytes`、`anthropic-api-key`、`check-spec-version`、`post-comment`、`fail-on-findings`
- [x] 自動偵測邏輯：`git diff --name-only base..head` 過濾 `.arceus/changes/<id>/`
- [x] 範例 workflow `.github/workflows/check-spec-example.yml`，並在 README 引用
- [x] `actionlint` 加入 CI workflow（本機沒裝，靠 CI raven-actions/actionlint@v2 驗證）

## Phase 8 — Release & docs

- [x] GoReleaser config：`.goreleaser.yml`，linux/darwin/windows × amd64/arm64 + checksums
- [x] `.github/workflows/release.yml`：on tag `v*` 跑 goreleaser
- [x] README：安裝、最小可用範例、CI 範例、報告結構、隱私聲明、CLI reference、架構說明
- [ ] 在本 repo dogfood：對本 change 跑一次 verify → `docs/audits/v0.1-self-audit.md`（需 `ANTHROPIC_API_KEY`，由使用者執行）

## Phase 9 — Acceptance gate

- [x] 走查 spec.md「驗收條件」每一項，逐條打勾並貼證據（檔案路徑 / 測試名稱）
- [ ] 將本 change `status` 從 `active` 轉為 `completed`：等使用者確認 dogfood 後執行 `arceus change status <id> completed`
