# Spec — v0.1 MVP — LLM-judged spec/code consistency auditor

## 需求描述

### 使用情境

```bash
# 開發者本機
check-spec verify --change 2026-05-20-some-change --base origin/main --head HEAD

# CI（PR）
- uses: seaflower/check-spec-action@v0
  with:
    change-id: 2026-05-20-some-change
    anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

### CLI 介面

二進位名稱：`check-spec`

子命令：

| 命令 | 說明 |
|---|---|
| `check-spec verify` | 主命令：對某個 change 跑審查並輸出報告 |
| `check-spec list` | 列出目前 repo 的 `.arceus/changes/` 內容（debug 用） |
| `check-spec version` | 印出版本 |

`verify` flags：

| Flag | 必填 | 預設 | 說明 |
|---|---|---|---|
| `--change <id>` | 是 | — | `.arceus/changes/<id>/` 資料夾名稱 |
| `--base <ref>` | 否 | `origin/main` | git 比對基準 |
| `--head <ref>` | 否 | `HEAD` | git 比對目標 |
| `--repo <path>` | 否 | `.` | repo 根目錄 |
| `--output <path>` | 否 | stdout | 報告輸出路徑 |
| `--format <fmt>` | 否 | `markdown` | `markdown` 或 `json`（json 用於程式介接） |
| `--model <name>` | 否 | `claude-opus-4-7` | LLM 模型 ID |
| `--max-diff-bytes <n>` | 否 | `200000` | diff 上限，超過則截斷並警告 |

環境變數：
- `ANTHROPIC_API_KEY` — 必要

### 報告格式（markdown）

```
# Spec/Code Consistency Audit — <change-id>

**Verdict**: APPROVE | REQUEST_CHANGES | NEEDS_DISCUSSION
**Model**: claude-opus-4-7
**Base → Head**: origin/main (abc123) → HEAD (def456)
**Files analyzed**: N

## Task implementation (from tasks.md)

| # | Task | Reported | Actual | Evidence |
|---|------|----------|--------|----------|
| 1 | … | [x] | done | path/to/file.go:42 |
| 2 | … | [x] | partial | … |
| 3 | … | [ ] | done | (was marked unchecked but actually implemented) |

## Acceptance criteria (from spec.md)

- PASS: CRITERION_TEXT — verified at path/to/file.go:L
- FAIL: CRITERION_TEXT — not found in diff
- PARTIAL: CRITERION_TEXT — missing edge case (...)

## Drift findings

- **Undocumented additions**: file X adds API Y not mentioned in spec
- **Missing from implementation**: spec requires Z, no matching code

## Open questions for human reviewer
- …
```

### 內部架構（資料流）

```
.arceus/changes/<id>/*.md  ─┐
                            ├─►  Prompt builder  ─►  Anthropic API  ─►  Structured JSON  ─►  Renderer  ─►  markdown
git diff base..head         ─┘                                              │
                                                                            └─►  --format json
```

### GitHub Action

- 形式：composite action（不是 Docker action，避免冷啟動）
- 步驟：
  1. checkout 已由上層 workflow 處理
  2. 安裝預編譯 binary（從 release artifact 抓對應 OS/arch）
  3. 推測 change-id（如未指定）：找 PR 內最新修改的 `.arceus/changes/<id>/` 資料夾
  4. 執行 `check-spec verify` → markdown 報告
  5. 用 `marocchino/sticky-pull-request-comment` 或同等機制貼 comment（key 固定，避免洗版）
  6. verdict ≠ APPROVE 時設定非零 exit

## 驗收條件

- [x] `check-spec verify --change <id>` 在本機可跑通，產出 markdown 到 stdout — `TestVerify_E2E_PassExit0` + smoke test
- [x] `--format json` 輸出符合 `internal/report/schema.json` 定義 — `TestVerify_E2E_JSONFormat`
- [x] 缺少 `ANTHROPIC_API_KEY` 時回傳 exit 2 並印出明確錯誤訊息 — `defaultJudgeFactory` 檢查 env，`TestVerify_JudgeFactoryError` 覆蓋
- [x] 找不到 `.arceus/changes/<id>/` 時回傳 exit 2 並列出可用 id — `TestVerify_E2E_ChangeNotFound`
- [x] 對 sample-change-pass 跑（mocked LLM）產出含 verdict APPROVE、無 drift — `TestVerify_E2E_PassExit0`（用 sanitizeReport 處理 SHA 浮動）
- [x] 對 sample-change-drift 跑（mocked LLM）產生 verdict=`REQUEST_CHANGES` 並列出 drift — `TestVerify_E2E_DriftExit1`
- [x] `go test ./...` 全綠 — 6 packages、44+ 個測試通過（含 -race）
- [x] `golangci-lint run` 無錯誤 — 0 issues（v2.12.2，啟用 errcheck/govet/staticcheck/gosec/revive/ineffassign/unused + gofmt/goimports）
- [x] GitHub Action 的 `action.yml` 由 `raven-actions/actionlint@v2` 在 CI 驗證
- [x] README 含安裝、最小可用範例、CI 範例、報告結構、隱私聲明、CLI reference、架構說明
- [x] 在 `check_spec` 自己的 repo 上 dogfood：commit `36071d0` 對本 change 跑 verify，結果存於 `docs/audits/v0.1-self-audit.md`（Opus 4.7、未截斷 50 files、verdict REQUEST_CHANGES 因 dogfood 與 status flip 兩項仍 pending；本 commit 修正之）

## 技術假設

- **語言/版本**：Go 1.23+（穩定的 `slog`、`log/slog`、generic 已成熟）
- **CLI 框架**：`github.com/spf13/cobra` — 業界事實標準，比 stdlib `flag` 適合多子命令
- **LLM SDK**：`github.com/anthropics/anthropic-sdk-go` 官方套件（v1.x）
- **Git 操作**：直接 shell-out 到 `git` 二進位（避免拉 `go-git` 增加二進位體積；CI/本機都已備 git）
- **設定載入**：MVP 不需要 config 檔，全靠 CLI flag + env
- **LLM 呼叫**：單次 messages.create，啟用 prompt caching 對 spec.md（靜態長文）以降本
- **輸出長度**：要求模型回 JSON，主程式做 markdown 渲染（不讓模型直接吐 markdown，避免不穩定）
- **二進位發佈**：GoReleaser → GitHub Releases，linux/macos/windows × amd64/arm64
- **版本控管**：semver，v0.x 期不保證向下相容
- **重試/錯誤處理**：Anthropic 5xx 重試 3 次（指數退避）；其他錯誤直接失敗
- **隱私**：spec.md + diff 會送上 Anthropic API，README 必須明確警示，CI 範例對 secret repo 標註注意事項
