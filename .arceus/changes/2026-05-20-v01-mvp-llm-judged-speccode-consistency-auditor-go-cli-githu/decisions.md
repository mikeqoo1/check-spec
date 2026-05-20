# Decisions — v0.1 MVP — LLM-judged spec/code consistency auditor

_記錄技術選擇與替代方案，避免未來重新爭論同樣的問題。_

## Decision 1: 判定機制採「LLM 當裁判」而非靜態規則

- **Context**: spec.md 是自由格式中文/英文 markdown，驗收條件無固定語法；單純用 AST 或 grep 無法理解「驗收條件 X 是否被程式碼滿足」。
- **Options considered**:
  - A. 純靜態：解析 tasks.md checkbox + AST grep file path/function — 只能驗表層
  - B. 純 LLM 裁判 — 彈性高，但結果非確定性
  - C. 混合：靜態先過，LLM 補語意層 — 蓋最廣但 MVP 複雜度高
- **Chosen**: B（純 LLM 裁判）
- **Rationale**: MVP 求最快驗證價值。LLM 對「自然語言驗收條件 → 程式碼是否實作」的 judgement 是這類工具唯一能 scale 的辦法。確定性問題用 golden file 測試 + prompt caching + 結構化 JSON 輸出緩解。靜態檢查留到 v0.2 觀察真實 false positive/negative 比例後再加。

## Decision 2: MVP 只支援 Anthropic Claude

- **Context**: 使用者目前在 Claude Code 生態，且 spec.md 段落長、適合 prompt caching（Anthropic 5 分鐘 TTL + 90% 折扣的 cache 在這個 use case 效益最高）。
- **Options considered**:
  - A. 抽象介面 + 多 provider（OpenAI / Gemini / Anthropic / 本地）一起做
  - B. 抽象介面但 MVP 只實作 Anthropic
  - C. 直接綁死 Anthropic，不抽象
- **Chosen**: B
- **Rationale**: 介面要抽（為 v0.2 預留），但實作只一個避免 MVP 工期翻倍。`Judge` interface 已在 spec.md 載明。

## Decision 3: 仰賴 `git` 二進位而非 `go-git` 函式庫

- **Context**: 需要拿到 base..head 的 diff 與 numstat。
- **Options considered**:
  - A. `github.com/go-git/go-git` 純 Go 函式庫
  - B. shell-out 到系統 `git`
- **Chosen**: B
- **Rationale**: go-git 會讓 binary 體積膨脹（~10MB+），且 corner case（submodule、LFS、稀疏 checkout）不如系統 git 穩。CLI 跟 CI 環境一定都有 git。安全性顧慮（command injection）已在 spec.md 載明要 validate ref。

## Decision 4: GitHub Action 採 composite，不採 Docker

- **Context**: Docker action 冷啟動慢（pull image 約 10-30 秒），且不易讓使用者覆寫版本。
- **Options considered**:
  - A. Docker action（內含 binary）
  - B. Composite action（runtime 抓 GitHub Release binary）
  - C. JavaScript action（要重寫一份 Node 版本）
- **Chosen**: B
- **Rationale**: composite 啟動快（~1 秒）、版本管理直觀（綁 release tag）、本機跟 CI 用同一份 binary 容易 debug。

## Decision 5: 輸出採「LLM 回 JSON → Go 渲染 markdown」而非 LLM 直接吐 markdown

- **Context**: 報告需穩定且可程式介接（`--format json`）。
- **Options considered**:
  - A. LLM 直接吐 markdown 報告
  - B. LLM 吐 JSON，Go 程式渲染 markdown
- **Chosen**: B
- **Rationale**: JSON schema 是合約，渲染邏輯在 Go 端可單測且 golden file 穩定。LLM 直接吐 markdown 容易格式漂移（表格錯位、heading 層級不一致）導致 CI comment 顯示異常。

## Decision 6: 不在 MVP 做自動偵測 change id 之外的智能行為

- **Context**: 使用者可能希望工具更聰明（自動找未完成 change、自動跨多個 change 跑等）。
- **Options considered**:
  - A. 一次只驗一個 change，由使用者/Action 指定
  - B. 自動掃所有 `status=active` change 並全部驗證
- **Chosen**: A
- **Rationale**: 一次一個責任邊界清楚；報告對單一 change 才有 actionable 意義。GitHub Action 端加自動偵測（從 PR diff 推測），但仍是「找出一個」而非「跑全部」。
