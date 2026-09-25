# BookForge 架构决策：结束标记驱动的长文生成

## 设计边界

`outline.md` 和 `initial_hooks.md` 是不透明文本。程序原样读取，不解析标题、章节、钩子结构或语义。每轮由 LLM 返回章节正文和一个结束标记：普通轮输出下一轮钩子文本，最后一轮输出全书完成标记。程序仅负责协议边界、章节序号文件名、状态提交和恢复。

## 提示词结构

每次请求包括三个 system sections 和一个 user message：

```text
system:
  prompts/system.md
  prompts/long_book_gen.md
  固定输出协议

user:
  以下是一份大纲：
  [outline.md 原文]

  以下是前次输出章节的钩子：
  [当前钩子；首次使用 initial_hooks.md]

  按要求输出并只输出一章新章节。
```

章节序号不进入 prompt。`prompts/long_book_gen.md` 是可配置的写作规则文件，作为普通 system 指令文本传给模型。大纲前置于动态 user prompt，以维持稳定前缀；缓存是否命中由模型服务决定。

## 响应协议

模型每次必须且只能返回以下之一：

```text
[章节正文]
<<<BOOKFORGE_HOOKS>>>
[下一轮完整钩子文本]
```

```text
[最后一章正文]
<<<END_OF_BOOK>>>
```

解析器只检查两个标记恰好出现其一、正文非空；钩子模式还要求钩子文本非空，结束模式要求结束标记后无多余文本。两个输出块的 Markdown 结构和语义不由程序解析。新钩子块替换当前状态，不由程序累积历史内容。

## 文件与状态

```text
my-book/
├── bookforge.yaml
├── outline.md
├── initial_hooks.md
├── prompts/
│   ├── system.md
│   └── long_book_gen.md
├── chapters/001.qmd
└── .bookforge/
    ├── manifest.json
    ├── snapshots/state_000.json
    └── audit/run_001.json
```

`ProjectState` 只持久化 `CurrentHooks` 和 `Finished`。Manifest 保存输入文件哈希，不保存生成计数。生成序号由 `chapters/NNN.qmd`、`state_NNN.json` 和 `run_NNN.json` 的连续完整前缀推导，只用于文件名、审计关联和恢复，不用于 prompt，也不决定结束时间。

提交时依次写章节和快照，最后原子写审计文件。三类产物都存在且序号连续才视为已提交；未完成或超前的文件在恢复时归档。最终状态的 `Finished` 标志来自本轮 `<<<END_OF_BOOK>>>`。

每轮审计包含输入哈希、生成序号、模型参数、prompt/response 哈希、前后钩子文本哈希、章节路径和是否全书结束。`store_prompt_and_response` 控制是否保存完整 prompt 和成功响应。格式无效的原始响应无论该设置为何值都会独立保存到 `.bookforge/invalid-responses/`。API 密钥不写入任何状态或审计。

`llm.parameters` 是用户自定义的通用键值映射，按原样合并进 OpenAI-compatible API 请求，BookForge 不解释参数。若服务端拒绝参数，记录并显示服务端错误。

`llm.headers` 是额外 HTTP 请求头的字符串映射，按原样发送；同名自定义头覆盖默认头。

输出格式错误时根据 `generation.invalid_output_retries` 自动重试；数值是首次失败后额外重试次数，默认 3。耗尽后不提交章节或状态，保留并显示全部无效响应。

操作日志位于 `.bookforge/log.jsonl`，记录生成、审核、重试、重写、清空和渲染时间、结果、章节序号及错误。`bookforge log` 显示日志；`bookforge status` 显示识别的章节文件映射、提交状态和结束状态。

交互审核支持批准当前章、`A` 批准并自动接受本次运行的其余章节、查看、编辑后确认、重新生成和退出。`A` 不修改项目配置。重写前先展示目标文件及级联失效范围并要求确认。`bookforge clear` 展示待删除文件并要求确认，只清理 BookForge 管理的产物；用户输入、提示词、配置、Quarto 项目和未登记文件保留。清理旧日志后会写入一条新的 clear 完成记录。

## 续跑与重写

```sh
bookforge generate
bookforge generate --auto
bookforge rewrite 5
```

`generate` 连续生成，直到已提交结束标记。若发生中断，再次调用时扫描连续已提交产物并从下一序号恢复；若最新状态已完成，则提示并退出。`rewrite N` 先展示解析出的目标章节文件并要求确认，再归档 N 及之后的章节、快照、审计和结束标志，从 N−1 状态继续，直到模型再次返回结束标记。

工具不接受生成次数参数，也不尝试推断 outline 章节数。输入文件哈希变化时拒绝普通续跑；显式 `rewrite 1` 重置 state 0 和输入哈希。

## CLI 与 Quarto

核心生成命令为 `generate [--auto]`、`rewrite <序号> [--auto]` 和 `render`。审核交互支持 `a` 批准、`A` 批准并自动批准本次运行余下章节、`v` 查看、`e` 编辑后确认、`r` 重生成和 `q` 退出。`A` 只影响本次运行。辅助命令 `init`、`validate`、`plan`、`status`、`log`、`hooks show` 和 `clear` 提供项目管理、状态查询和受限清理。

`clear` 先列出 BookForge 管理的生成文件并要求确认，只删除编号章节、快照、审计、无效响应、归档和运行日志；不会递归删除项目，也不会删除用户源文件、提示词、Quarto 文件或未登记的文件。

BookForge 只生成编号 `.qmd` 并调用用户配置的 Quarto 命令，不修改 Quarto 清单、模板或 PDF 排版。Quarto 项目必须明确引用生成章节；渲染前检查该引用和状态一致性。

## 验证边界

核心验证通过 `go test ./...`、`go test -race ./...` 和 `go vet ./...` 完成。真实 OpenAI 调用和 PDF 渲染依赖用户服务凭据及 Quarto 环境。提示缓存效果由服务端决定，程序不保证命中。
