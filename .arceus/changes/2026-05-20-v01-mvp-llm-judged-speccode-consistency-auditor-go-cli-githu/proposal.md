# v0.1 MVP — LLM-judged spec/code consistency auditor (Go CLI + GitHub Action)

## 為什麼 (Why)

使用者目前的開發流程大量仰賴 Arceus plugin 來幫 Claude Code 產出 `.arceus/changes/<id>/{proposal,spec,tasks,decisions}.md`，並由 `arceus:coder` 依 `tasks.md` 實作程式碼。

**痛點**：實作完成後沒有任何自動化機制確認「**LLM 寫出來的程式碼，跟它自己當初承諾的 spec / tasks 是否真的吻合**」。

- `tasks.md` 雖然有 checkbox，但勾選與否完全由 coder agent 自己回報，缺乏第三方驗證。
- `spec.md` 的「驗收條件」清單只是文件，沒有人實際對著 code 逐條檢查。
- 整個 SDD 流程缺一個「**事後稽核者**」這塊。GitHub Spec Kit 的 `/speckit.analyze` 只做規格之間的一致性檢查，並未驗證 spec ↔ code。

**期望效益**：在 `arceus:apply` 完成後（或 PR 階段）由一個**獨立的、不參與實作的第三方工具**，產出可審計的 markdown 報告，明確指出：

1. `tasks.md` 中哪些項目已實作 / 部分實作 / 未實作（不只信任 checkbox）
2. `spec.md` 驗收條件逐條驗證結果
3. 觀察到的 drift：spec 沒有但 code 出現了什麼、spec 要求但 code 沒做什麼
4. 整體 verdict：APPROVE / REQUEST_CHANGES / NEEDS_DISCUSSION

## 範圍 (Scope)

- **In scope**:
  - Go CLI 工具 `check-spec`，吃 `.arceus/changes/<id>/` 資料夾與 git ref 範圍，輸出 markdown 報告
  - LLM 裁判機制（MVP 採 Anthropic Claude，介面抽象化以利後續增 provider）
  - 從 git diff 蒐集「這個 change 影響的程式碼變更」
  - 對應每個 task / 驗收條件的逐條判定（結構化 JSON 中介，再渲染成 markdown）
  - GitHub Action（composite action）封裝，在 PR 上自動跑並貼 sticky comment + 設定 check status
  - Golden-file 測試：對固定的 spec/code/diff 輸入，prompt + LLM 模擬回應產出固定報告
  - Exit codes：`0` 全部通過、`1` 有 drift 或未實作項、`2` 執行錯誤

- **Out of scope (留待 v0.2+)**:
  - 非 Arceus 格式（spec-kit、OpenSpec、Kiro、任意 markdown 路徑）— MVP 硬編碼 `.arceus/changes/<id>/`
  - 多 LLM provider（OpenAI / Gemini / 本地模型）— MVP 只支援 Anthropic
  - 靜態 AST 分析做 cross-check（純 LLM 裁判）
  - Web UI / daemon / 歷史審查瀏覽
  - 自動修補（auto-fix）功能
  - 私有模型微調

## Stakeholders

- **Owner**: 使用者 (`@seaflower`) — 主要使用者與設計者
- **Reviewer**: 使用者本人（單人專案）
- **受影響的人/流程**:
  - 使用 Arceus 的所有專案（DinoPanel 等）— 這個工具會被當外掛接到他們的 PR 流程
  - 未來的 `arceus:apply` workflow — 完成後可以自動串接 `check-spec` 作為下一步
