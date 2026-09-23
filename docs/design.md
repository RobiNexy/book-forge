# BookForge 架构设计文档

参照《CLI 应用架构之禅》的骨架，把这个"钩子驱动的长篇生成器"拆到位。核心目标只有一个：让 LLM 上下文里永远只有"大纲 + 钩子池 + 当前坐标"，其余复杂度全部由 CLI 内部消化。

---

## 一、设计哲学与第一性约束

### 1.1 边界与职责

BookForge 只做一件事：把一份 Markdown 大纲，经过若干次受控的 LLM 调用，产出一本 Quarto 可渲染的书稿目录。

它不做的事：
- 不管大纲怎么来（用户另有工作流）
- 不管 Quarto 模板和 `quarto.yml`（用户自备）
- 不管 PDF 的最终排版（`quarto render` 全权负责）
- 不管 LLM 的替换（V1 只对接 OpenAI）

这是一个典型的"Unix 组合"取向。BookForge 的输出（`chapters/*.qmd`）就是它给下游 Quarto 的接口，一旦 chapters 生成完毕，你完全可以手动 `quarto render`，工具本身可以从流水线里拿掉。

### 1.2 三条不可动摇的约束

约束 1：上下文只装三样东西。大纲在最前（吃 OpenAI prompt caching），钩子池在中间，当前章节坐标（章节号）在最后。这条约束决定了所有 prompt 相关的代码结构。

约束 2：一次 LLM 调用产出正文与钩子操作。生成、提取、兑现判定合并为一次调用，通过输出协议强约束格式。这既省 token 又让 D_n / rw / ex(c_n) 三段维护区在一次响应里显式呈现（对应命题 4 的"日志忠实"）。

约束 3：状态转移是纯函数。`Apply(H_{n-1}, ops) → H_n` 不做 I/O，不调 LLM。这让重放、回滚、测试都变得极其干净。

---

## 二、领域模型：从数学到代码

### 2.1 核心概念映射

| 数学符号 | Go 类型 | 说明 |
|---|---|---|
| O | `outline.Outline` | 大纲 Markdown 的解析结果 |
| O_n | `int`（章节号）| 章节坐标 |
| h ∈ H | `domain.Hook` | 单个钩子 |
| H_n | `domain.HookPool` | 第 n 章后的钩子池快照 |
| D_n | `[]string`（钩子 ID 列表）| 被兑现的钩子集 |
| rw | `map[string]string` | 钩子改写映射 |
| ex(c_n) | `[]domain.Hook` | 本章新增钩子 |
| δ(σ, H_{n-1}, O_n) | `domain.Transition` | 单章转移记录 |

### 2.2 钩子的稳定 ID 策略

命题 3 要求 rw 保持类型和身份不变。实现上：

- ID 由 BookForge 分配，格式 `h_<src>_<seq>`，例如 `h_003_01` 表示"第 3 章生成的第 1 号钩子"
- 大纲派生的初始钩子 src = 0，ID 形如 `h_000_01`
- LLM 只输出 content 和 type，不输出 ID
- 改写时 LLM 只引用旧 ID，不能创造新 ID，也不能修改 type

这一策略把"稳定身份"从 LLM 的自觉性中剥离，落到工程层保证。

### 2.3 转移的两阶段结构

严格按转移方程拆：

```
阶段 A（LLM 侧）:
   输入: (O, H_{n-1}, n)
   输出: (c_n, discharged_ids, rewritten_map, added_hooks_without_id)

阶段 B（本地纯函数）:
   1. 分配 ID 给 added_hooks
   2. 校验：discharged_ids ⊆ H_{n-1}
   3. 校验：rewritten 的 key 必须在 H_{n-1} 且类型不变
   4. 应用: H_n = (H_{n-1} \ D_n) 上 rw → 合并 added → 附加 src=n
```

阶段 B 完全可测试，也是命题 4 里"机械可判定"的部分。

---

## 三、分层架构

沿用 CLI Zen 第十章的四层结构：

```
┌─────────────────────────────────────────────────────────┐
│  Entry Layer                                            │
│  cmd/bookforge/main.go                                  │
│  信号注册 → 参数解析 → 配置层叠 → 日志 → 子命令路由    │
└──────────────────┬──────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────┐
│  Commands Layer                                         │
│  internal/commands/{init,gen,rewrite,render,status,...} │
│  参数校验 → 环境校验 → 构造请求 → 调用核心 → 输出        │
└──────────────────┬──────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────┐
│  Core Layer (Library)                                   │
│  ┌─────────────┐  ┌──────────┐  ┌─────────────────┐    │
│  │ domain      │  │ outline  │  │ orchestrator    │    │
│  │ (纯类型 +   │  │ (MD 解析)│  │ (单章循环状态机)│    │
│  │  转移函数)  │  │          │  │                 │    │
│  └─────────────┘  └──────────┘  └────────┬────────┘    │
│                                          │              │
│  ┌─────────────┐  ┌──────────┐  ┌────────▼────────┐    │
│  │ prompt      │  │ parser   │  │ review          │    │
│  │ (消息组装)  │  │ (响应解析│  │ (人工审核)      │    │
│  │             │  │  +校验)  │  │                 │    │
│  └─────────────┘  └──────────┘  └─────────────────┘    │
└──────────────────┬──────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────┐
│  Infrastructure Layer                                   │
│  llm (OpenAI 客户端) │ store (JSON 持久化) │            │
│  render (quarto exec) │ config (层叠加载) │ signal      │
└─────────────────────────────────────────────────────────┘

横切关注点: errors │ logger │ tempfile │ context.Context
```

关键分层原则：core 层不 import infra 层的具体实现，只依赖抽象接口。这样 orchestrator 可以在测试里注入 mock LLM 和内存 store。

---

## 四、目录结构

### 4.1 BookForge 代码仓

```
bookforge/
├── cmd/
│   └── bookforge/
│       └── main.go                # 入口，最薄的一层
├── internal/
│   ├── domain/                    # 核心类型 + 纯函数
│   │   ├── hook.go
│   │   ├── pool.go
│   │   ├── transition.go          # Apply(H, ops) → H'
│   │   └── transition_test.go
│   ├── outline/                   # Markdown → 章节列表
│   │   ├── parser.go
│   │   └── parser_test.go
│   ├── prompt/                    # 组消息数组
│   │   ├── builder.go             # 大纲永远第一条 system message
│   │   └── templates.go
│   ├── parser/                    # LLM 响应 → (正文, ops)
│   │   ├── response.go
│   │   └── response_test.go
│   ├── review/                    # CLI 审核交互
│   │   ├── review.go
│   │   └── diff.go
│   ├── orchestrator/              # 主循环
│   │   ├── orchestrator.go
│   │   ├── generate.go            # gen 命令的核心逻辑
│   │   ├── rewrite.go             # rewrite 命令的核心逻辑
│   │   └── resume.go              # 断点续跑
│   ├── llm/                       # LLM 抽象 + OpenAI 实现
│   │   ├── client.go              # interface Client
│   │   └── openai/
│   │       └── openai.go
│   ├── store/                     # 项目目录读写
│   │   ├── layout.go              # 路径约定
│   │   ├── snapshot.go            # H_n 快照
│   │   ├── chapter.go             # qmd 文件
│   │   └── audit.go               # 审计日志
│   ├── config/                    # 配置层叠
│   │   └── config.go
│   ├── render/                    # 调用 quarto
│   │   └── quarto.go
│   ├── commands/                  # CLI 子命令
│   │   ├── init.go
│   │   ├── gen.go
│   │   ├── rewrite.go
│   │   ├── render.go
│   │   └── status.go
│   └── infra/
│       ├── signal/                # SIGINT 优雅退出
│       ├── tempfile/
│       └── log/
├── testdata/
│   └── fixtures/                  # 录制的 LLM 响应
├── configs/
│   └── config.example.yaml
├── go.mod
└── README.md
```

### 4.2 用户项目目录约定

BookForge 只读写这个目录，不侵入用户的其他文件：

```
my-book/
├── outline.md                     # 用户提供
├── initial-hooks.md               # 用户提供的初始钩子清单
├── quarto.yml                     # 用户提供（自备模板）
├── _template/                     # 用户提供
├── .bookforge/                    # 工具管理的所有状态
│   ├── config.yaml                # 项目级配置（模型、参数等）
│   ├── state/
│   │   ├── H_0.json               # 初始钩子池
│   │   ├── H_1.json               # 第 1 章后
│   │   └── H_N.json
│   ├── audit/
│   │   ├── ch_01/
│   │   │   ├── prompt.json        # 完整 messages（可关闭）
│   │   │   ├── response.txt       # 原始响应（可关闭）
│   │   │   └── ops.json           # 结构化的转移操作
│   │   └── ch_02/
│   └── lock                       # 锁文件，防止并发
├── chapters/                      # 生成的正文
│   ├── 01.qmd
│   ├── 02.qmd
│   └── ...
└── _book/                         # quarto 输出
    └── book.pdf
```

`.bookforge/` 目录下的所有状态由工具管理，用户不应手工修改（但可以人肉查看，全是 JSON 和文本）。这遵循了 Git 的"文件系统即数据库"思路：可检查、可诊断、无外部依赖。

---

## 五、CLI 接口设计

### 5.1 子命令总览

```
bookforge <command> [options]

Commands:
  init <dir>              初始化项目目录，生成 .bookforge/ 骨架
  gen                     顺序生成章节（默认从上次中断处继续）
  rewrite <from>          从第 <from> 章开始重写，回滚 H_{≥from-1}
  render                  调用 quarto render 出 PDF
  status                  显示当前进度：已生成章节、钩子池大小
  hooks show [n]          查看第 n 个快照的钩子池（默认最新）
  hooks diff <a> <b>      对比两个快照

Global options:
  --project <dir>         项目目录，默认为当前工作目录
  --config <file>         额外的配置文件
  -v, --verbose           详细日志（可叠加：-vv 输出 debug）
  -q, --quiet             静默模式
  --no-color              禁用彩色输出
```

### 5.2 gen 子命令的完整参数

```
bookforge gen [options]

Options:
  --outline <file>        大纲文件，默认 outline.md
  --initial-hooks <file>  初始钩子文件，默认 initial-hooks.md
  --from <n>              从第 n 章开始（默认从上次中断处）
  --to <n>                只生成到第 n 章为止（默认到大纲末尾）
  --auto                  全自动模式，跳过所有审核
  --dry-run               不落盘，只打印会做什么
  --no-audit-full         审计日志不保存完整 prompt/response
  --model <name>          覆盖配置里的模型（如 gpt-4o）
```

初始钩子作为独立文件的设计动机：命题 5 提到的 init(σ) 必须由人肉输入。让 LLM 从大纲自动提取初始钩子容易漂移，用户直接写更可控。

### 5.3 rewrite 子命令

```
bookforge rewrite <from> [options]

参数:
  <from>                  从这一章开始重写（含）

Options:
  --keep-audit            保留旧的审计记录（默认覆盖）
  --auto                  全自动，不审核
```

行为语义：
- 读取 `H_{from-1}` 作为起点
- 删除 `chapters/{from..}.qmd`
- 删除 `H_{from..N}.json`
- 从第 from 章开始，按 gen 的流程往下走

这遵循 Git 的锁文件模式：先原子地清理再重跑，避免中间状态被误认为有效。

### 5.4 退出码约定

参照 CLI Zen 第三章：

| 退出码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 通用失败 |
| 2 | 用户中止（审核阶段选择 abort，或 SIGINT） |
| 64 | 参数错误（EX_USAGE） |
| 65 | 输入数据错误（大纲解析失败、初始钩子格式错） |
| 69 | 服务不可用（LLM API 调用失败） |
| 70 | 内部错误（EX_SOFTWARE，转移函数校验失败等） |
| 73 | 无法创建输出文件（EX_CANTCREAT） |
| 75 | 临时失败，可重试（EX_TEMPFAIL，如速率限制） |

### 5.5 输出流约定

- stdout：结构化数据（`status` 的表格、`hooks show` 的 JSON）
- stderr：进度、日志、错误、审核交互
- 进度输出只在 `stderr.isatty()` 时开启

这样 `bookforge status --format json | jq '.chapters_done'` 之类的组合是自然可用的。

---

## 六、核心数据类型

```go
// internal/domain/hook.go
package domain

type HookType string

const (
    HookFact     HookType = "fact"      // 事实设定
    HookTension  HookType = "tension"   // 冲突/悬念
    HookCallback HookType = "callback"  // 待呼应的伏笔
)

type Hook struct {
    ID      string   `json:"id"`
    Src     int      `json:"src"`      // 0=大纲, 1..N=章节
    Type    HookType `json:"type"`
    Content string   `json:"content"`
}

// internal/domain/pool.go
type HookPool struct {
    N       int    `json:"n"`         // 生成完第 n 章后的池
    Hooks   []Hook `json:"hooks"`
    Version string `json:"version"`   // schema 版本
}

// internal/domain/transition.go
type ChapterOps struct {
    N          int               `json:"n"`
    Discharged []string          `json:"discharged"`   // 兑现的钩子 ID
    Rewritten  map[string]string `json:"rewritten"`    // id -> new content
    Added      []Hook            `json:"added"`        // ID 已分配好
}

// Apply 是纯函数：不做 I/O，不调 LLM
func Apply(prev HookPool, ops ChapterOps) (HookPool, error) {
    // 1. 校验 Discharged 都存在于 prev
    // 2. 校验 Rewritten 的 key 都存在，type 未变
    // 3. 校验 Added 的 Src == ops.N
    // 4. 应用: (prev \ D) 上 rw 合并 added
    // 5. 返回 HookPool{N: ops.N, Hooks: ...}
}
```

`Apply` 的错误分类：
- `ErrUnknownHookID`：Discharged 或 Rewritten 引用了不存在的 ID → 用户错误（LLM 输出无效）
- `ErrTypeChanged`：改写试图变更类型 → 用户错误
- `ErrBadSrc`：新增钩子的 src 不等于当前章节号 → 内部错误

---

## 七、LLM 输出协议

### 7.1 响应格式

用清晰的定界符切分正文与钩子操作。正文是长文本，塞进 JSON 会因转义和长度问题变脆；钩子操作结构化，用 JSON 最好。

```
<<<CHAPTER>>>
第一节 ……

（本章正文，直接写入 chapters/NN.qmd）

<<<HOOKS>>>
{
  "discharged": [
    {"id": "h_000_03", "note": "在酒馆场景兑现了欠债线索"}
  ],
  "rewritten": [
    {"id": "h_002_01", "new_content": "李四对秘密的怀疑加深，转为主动调查"}
  ],
  "added": [
    {"type": "tension", "content": "王五发现了一封未署名的信"},
    {"type": "callback", "content": "刀鞘上的刻痕将在第 7 章被认出"}
  ]
}
<<<END>>>
```

解析器行为：
- 用 `<<<CHAPTER>>>` 和 `<<<HOOKS>>>` 切正文
- 用 `<<<END>>>` 之前的最后一个 `}` 定位 JSON 结束
- JSON 用严格模式解析（不允许多余字段），出错时把原始响应写入 audit 供排查
- `added` 由 BookForge 分配 ID，`h_<n:03>_<seq:02>` 格式

### 7.2 Prompt 结构（缓存优先）

OpenAI prompt caching 要求前缀稳定。消息数组严格按下面顺序：

```
[
  {role: "system", content: <指令模板 + 输出协议说明>},   // 稳定，命中缓存
  {role: "system", content: <完整大纲 Markdown>},         // 稳定，命中缓存
  {role: "user",   content: <当前钩子池 JSON>},           // 每章都不同
  {role: "user",   content: <生成第 N 章的指令>}          // 每章都不同
]
```

指令模板和大纲永远不变，前缀能吃到缓存。钩子池和章节坐标是变量部分，放在后面。这直接对应你说的"大纲放最前面永远能吃到缓存"。

---

## 八、状态管理

### 8.1 快照策略

每个 `H_n.json` 是完整的钩子池快照（不是增量）。理由：
- 空间成本可忽略（一本书几十章，每个快照几十 KB）
- 恢复和回滚变成简单的"读某个文件"，无需重放
- 审计和 diff 极其直观

### 8.2 审计日志

每章一个目录 `.bookforge/audit/ch_NN/`：

```json
// ops.json（永远保存）
{
  "n": 3,
  "started_at": "2025-01-15T10:23:15Z",
  "finished_at": "2025-01-15T10:24:02Z",
  "model": "gpt-4o-2024-11-20",
  "prompt_hash": "sha256:...",
  "response_hash": "sha256:...",
  "usage": {"prompt_tokens": 12043, "completion_tokens": 3821, "cached_tokens": 11200},
  "discharged": [...],
  "rewritten": {...},
  "added": [...],
  "reviewed_by": "auto" | "human",
  "review_edits": [...]  // 人工审核时对钩子做的微调
}
```

`prompt.json` 和 `response.txt` 可通过 `--no-audit-full` 关闭。默认打开，因为你要求"要存，可开关"。

### 8.3 锁文件与恢复

`.bookforge/lock` 文件包含 pid 和启动时间戳。启动时检查：
- 文件不存在 → 正常启动
- 存在但对应进程已死 → 打印警告，覆盖并继续（可能有半截的 audit，`resume` 会跳过不完整的目录）
- 存在且进程活着 → 拒绝启动

恢复逻辑（`gen` 默认行为）：
- 扫描 `.bookforge/state/`，找到最大的 `H_k`
- 扫描 `chapters/`，验证 `01.qmd..k.qmd` 都存在
- 从 k+1 章继续

### 8.4 状态一致性不变式

任何时刻，下面这些必须同时成立：

```
存在 H_k.json  ⟺  存在 chapters/k.qmd  ⟺  存在 audit/ch_k/
```

每章生成的最后阶段是原子提交：

```
1. 生成响应，解析成功
2. （审核通过）
3. 写 chapters/{N}.qmd.tmp
4. 写 .bookforge/state/H_{N}.json.tmp
5. 写 .bookforge/audit/ch_{N}/*
6. 原子 rename：qmd.tmp → qmd
7. 原子 rename：json.tmp → json
```

如果 6 之后崩溃，7 之前，下次启动检测到 qmd 存在但 H 不存在，认为该章未完成，删掉 qmd 和 audit 目录重跑。校验方向：H 是唯一的"提交标记"。

---

## 九、单章生成的状态机

```
                    ┌─────────────────┐
                    │  Load H_{n-1}   │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │  Build Prompt   │
                    │  (O, H, n)      │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │  Call LLM       │──── 失败 ──→ 重试(最多3次)
                    └────────┬────────┘                │
                             │                          ▼
                    ┌────────▼────────┐         ┌───────────────┐
                    │  Parse Response │         │ Exit code 69  │
                    └────────┬────────┘         └───────────────┘
                             │
                    ┌────────▼────────┐
                    │  Assign IDs +   │
                    │  Validate ops   │────── 校验失败 ──┐
                    └────────┬────────┘                   │
                             │                             ▼
                             │                    ┌────────────────┐
                             │                    │ 打印错误+响应   │
                             │                    │ 提示可 rewrite  │
                             │                    │ Exit code 70    │
                             │                    └────────────────┘
                    ┌────────▼────────┐
              ┌─────┤   Review Mode?  │
              │     └─────────────────┘
         auto │                 │ human
              │                 │
              │        ┌────────▼────────┐
              │        │  Show diff +    │
              │        │  Prompt user    │───── abort ──→ Exit 2
              │        └────────┬────────┘
              │                 │ accept/edit
              │                 │
              └────────┬────────┘
                       │
              ┌────────▼────────┐
              │  Apply(H, ops)  │  纯函数
              │  → H_n          │
              └────────┬────────┘
                       │
              ┌────────▼────────┐
              │ Commit (原子)   │
              │ qmd + H + audit │
              └────────┬────────┘
                       │
              ┌────────▼────────┐
              │  n = n + 1      │
              └─────────────────┘
```

---

## 十、人工审核的 CLI 交互

保持简单，遵循 stdin/stderr。审核界面全部输出到 stderr，避免污染潜在的管道使用。

```
================================================================
Chapter 3 generated. Review before commit.
================================================================

Length: 4231 characters
Discharged hooks (2):
  - h_000_03 [callback] "父亲留下的怀表将在关键时刻出现"
  - h_002_01 [tension]  "李四对张三的怀疑"

Rewritten hooks (1):
  - h_001_02 [fact]
      old: "王五住在城南的老宅"
      new: "王五搬到了城东的新居，老宅由妹妹看守"

New hooks (3):
  + h_003_01 [tension]  "刀鞘上的刻痕似有玄机"
  + h_003_02 [callback] "第 5 节提到的信件尚未拆开"
  + h_003_03 [fact]     "张三对建筑学有隐秘的爱好"

Actions:
  [a] accept and commit
  [v] view full chapter text
  [e] edit hooks (opens $EDITOR on ops.json)
  [r] regenerate (discard and retry)
  [s] skip commit (exit without saving)
  [q] abort session

Choice: _
```

编辑用 `$EDITOR` 打开临时 JSON 文件，用户改完保存后重新校验。校验失败允许重编辑。这是 Git commit message 编辑器的标准模式。

`--auto` 模式跳过整个交互，直接走 accept 路径，只在校验失败时报错退出。

---

## 十一、配置层叠

沿用 CLI Zen 第二章的层叠模型：

```
优先级（高 → 低）:
  1. CLI 参数         --model gpt-4o
  2. 环境变量         BOOKFORGE_MODEL=gpt-4o
  3. 项目配置         <project>/.bookforge/config.yaml
  4. 用户配置         $XDG_CONFIG_HOME/bookforge/config.yaml
  5. 内置默认值
```

`config.yaml` 示例：

```yaml
llm:
  provider: openai
  model: gpt-4o-2024-11-20
  api_key_env: OPENAI_API_KEY        # 从环境变量取，不写入文件
  base_url: https://api.openai.com/v1
  temperature: 0.8
  max_tokens: 8000
  timeout_seconds: 300
  retry:
    max_attempts: 3
    backoff_seconds: [2, 5, 15]

generation:
  auto_mode: false
  audit_full: true                    # 保存完整 prompt/response
  chapter_file_pattern: "{:02d}.qmd"  # 章节文件名格式

paths:
  outline: outline.md
  initial_hooks: initial-hooks.md
  chapters_dir: chapters
```

API key 只走环境变量，不落盘。

---

## 十二、错误处理原则

三类错误对应三种输出策略：

用户错误（stderr + 建议 + exit 64/65）：

```
Error: outline.md not found in project directory.

Hint: run 'bookforge init' first, or specify --outline <path>.
```

运行时错误（stderr + 上下文 + 可能的 exit 69/73/75）：

```
Error: OpenAI API request failed after 3 attempts.
  Model:    gpt-4o-2024-11-20
  Attempt:  3/3
  Last error: rate_limit_exceeded (retry after 45s)

Hint: use --model to try a different model, or wait and re-run.
      Progress is saved; 'bookforge gen' will resume from chapter 4.
```

内部错误（stderr + 诊断信息 + exit 70）：

```
Internal error: hook ID h_002_XX referenced in 'discharged' does not
exist in pool H_2.

This is likely a bug in the response parser. Diagnostic info saved to:
  .bookforge/audit/ch_003/response.txt

Please report at: <repo>/issues
```

---

## 十三、信号处理

`SIGINT` 分阶段响应：

- 在 LLM 调用等待期间收到 → 取消 HTTP 请求，不写任何状态，exit 2
- 在解析/校验期间收到 → 让当前章节走完落盘，再退出
- 在审核交互中收到 → 直接退出（当前章节无 commit）
- 连续两次 Ctrl+C → 立即退出

`SIGPIPE` 默认忽略（stdout 输出到 head 之类的管道时避免崩溃）。

关键：任何 exit 前都要移除 `.bookforge/lock`。用 Go 的 `defer` 加上 signal.Notify 的信号转发。

---

## 十四、测试策略

三层测试金字塔：

单元测试（大头，快）：
- `domain.Apply` 的全部转移规则和校验路径
- `outline.Parse` 对各种 Markdown 结构
- `parser.Parse` 用录制的 LLM 响应做 fixture 测试
- `prompt.Build` 验证消息顺序（大纲永远在指令后，钩子池永远在指令前）

集成测试：
- orchestrator 用 mock LLM（返回预录响应）跑一本 3 章的小书
- rewrite 场景：跑到第 3 章，rewrite from 2，验证状态一致
- resume 场景：模拟第 2 章崩溃，验证第 3 次启动能正确恢复

端到端测试：
- `bookforge init && bookforge gen --auto` 在真实项目目录上
- 用极便宜的模型（如 gpt-4o-mini）跑，或用 `--dry-run` 只走前 3 步

`store` 层用临时目录测，测试后自动清理。所有 fixture 存在 `testdata/`。

---

## 十五、演进路径

V1（当前）：
- 单大纲、顺序生成、单次审核循环
- OpenAI only
- rewrite 支持从某章重写
- resume 支持中断恢复

V1.1 候选：
- `hooks manual-add` / `hooks manual-remove`：不通过 LLM 直接编辑钩子池（应急用）
- `bookforge doctor`：检查项目状态一致性
- `bookforge export`：把整个 audit + state 打包，方便反馈问题

V2 可能扩展点（不要在 V1 设计里预留过多，但架构不排斥）：
- LLM 抽象层加入 Anthropic / 本地模型（`llm.Client` 已经是 interface）
- 多线叙事：`HookPool` 支持"作用域"标记（章节范围 / 视点角色）
- 章节级并发：不同章节间无依赖时理论可并，但会破坏钩子池的单调递推，暂不建议

---

## 十六、关键接口预览

以下是 core 层几个关键接口的样子，先给你一个可视化的印象：

```go
// internal/llm/client.go
type Client interface {
    Complete(ctx context.Context, req Request) (Response, error)
}

type Request struct {
    Model       string
    Messages    []Message   // 严格保序，前缀稳定
    Temperature float64
    MaxTokens   int
}

type Response struct {
    Content      string
    Usage        Usage
    CachedTokens int
}

// internal/orchestrator/orchestrator.go
type Orchestrator struct {
    llm      llm.Client
    store    store.Store
    reviewer review.Reviewer
    prompt   *prompt.Builder
    parser   *parser.Parser
    logger   *slog.Logger
}

func (o *Orchestrator) GenerateChapter(ctx context.Context, n int) error {
    // 状态机在这里
}

// internal/store/store.go
type Store interface {
    LoadOutline() (outline.Outline, error)
    LoadInitialHooks() ([]domain.Hook, error)

    LoadSnapshot(n int) (domain.HookPool, error)
    SaveSnapshot(pool domain.HookPool) error
    LatestSnapshotN() (int, error)

    SaveChapter(n int, content string) error
    HasChapter(n int) bool

    SaveAudit(n int, record AuditRecord) error

    // 用于 rewrite：原子清理 from 之后的所有状态
    TruncateFrom(from int) error
}
```

---

## 可能遗漏的视角

几处我判断不足需要点出：

1. Prompt caching 的实际收益取决于 OpenAI 的缓存策略在你选定模型上的行为，尤其是当大纲修改（哪怕一个字）时会失效。如果你打算频繁调整大纲边跑边改，缓存收益可能低于预期。[基于常见模式推断]

2. LLM 生成的正文长度控制：单章几千字时 max_tokens 够用，但如果你要生成万字长章，需要考虑分段生成或 continuation 机制，这在当前设计里没体现。V1 建议每章目标控制在 4000 tokens 以内。

3. 中文 Quarto 渲染的字体和 LaTeX 引擎问题我按你说的"有模板"跳过了，如果 render 阶段出问题，最先看的应该是 `_book/` 下的 LaTeX 中间产物，不是我们的锅但会被误认为是。

4. 审计日志的隐私考量：完整 prompt 会包含大纲全文，如果这本书是敏感内容，audit 目录需要在 `.gitignore` 里；`init` 命令应该自动生成 `.gitignore`。

---

要不要我先落地 `domain` 包（类型定义 + 转移函数 + 单元测试），把这套架构的核心地基打好？这是纯函数部分，没有外部依赖，可以快速拿出来让你评审接口和语义。
