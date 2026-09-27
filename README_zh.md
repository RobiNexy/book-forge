# BookForge

[English README](README.md)

BookForge 是一个 Go CLI 工具，使用两个不透明文本文件生成 Quarto 章节：用户提供的大纲和当前钩子块。程序不解析大纲标题、章节数量或钩子结构。模型必须通过 `<<<END_OF_BOOK>>>` 明确表示全书完成。

## 环境与构建

- 从源码构建需要 Go 1.22 或更高版本。
- 生成章节需要 OpenAI API Key。
- 渲染 PDF 时需要安装 Quarto。

```sh
go test ./...
go build -o ./bookforge ./cmd/bookforge
```

## 准备项目

### 初始化项目

```sh
./bookforge init ./my-book
# 或指定字体模板：a / b / c / d
./bookforge init --template c ./classic-book
```

默认 `auto` 按当前系统选择基础字体：Linux 使用 Noto CJK，Windows 使用宋体/微软雅黑，macOS 使用宋体 SC/PingFang。模板选项：

| 选项 | 风格 | 字体要求 |
|---|---|---|
| `a` | 通用基础 | TeX Live 自带 Fandol 和 TeX Gyre，适合跨系统迁移 |
| `b` | 系统基础 | Linux 用 Noto CJK；Windows/macOS 用系统中文字体 |
| `c` | 典藏衬线 | EB Garamond、Source Han、JetBrains Mono、STIX Two；需自行安装 |
| `d` | 现代人文 | IBM Plex、Source Han、JetBrains Mono、STIX Two；需自行安装 |

也可用 `--template auto` 明确选择系统默认。高配模板要求 XeLaTeX/fontconfig 能找到配置中列出的字体；跨机器分享时，建议项目成员统一安装字体或改用 `a`。初始化不会覆盖已有文件，因此更换模板不会改写既有 `_quarto.yml`。

若要生成其他平台的基础配置，也可将 `--template` 设为 `linux`、`windows` 或 `macos`；当前系统对应的 `b` 和 `auto` 会自动选择其中一种。

### 五套编辑设计

除了字体组合，还可以选择一套完整视觉设计。设计会分别处理封面、章首页、正文标题、页眉页码和纸面配色；五套均使用通用 TeX Live 字体，不需要另装商业字体：

| 设计 | 方向 | 视觉语言 |
|---|---|---|
| `atelier` | 文艺编辑 | 暖纸色、陶土侧栏、衬线字母标记 |
| `swiss` | 瑞士网格 | 高对比红色几何块、无衬线字级和硬朗分隔线 |
| `archive` | 典藏书系 | 象牙纸、双细框、居中章题和古典衬线 |
| `nocturne` | 夜航 | 午夜蓝底、金色轨道、反白题字 |
| `jade` | 东方青绿 | 玉色纸面、错位留白、印章红色点题 |

设计名可以直接作为模板，也可以叠加在基础或高配字体上：

```sh
./bookforge init --template atelier ./my-book
./bookforge init --template b --design swiss ./swiss-book
./bookforge init --template c --design archive ./archive-book
```

`--template atelier` 等价于通用基础字体加对应设计；`--design swiss` 则保留 `--template` 选定的字体。设计长标题、章节题字和正文层级都经过含数学、表格、脚注、代码及跨页内容的同一份样稿编译检查。

`init` 只创建缺少的文件，不覆盖已有文件：

```text
my-book/
├── bookforge.yaml
├── _quarto.yml
├── index.qmd
├── outline.md
├── initial_hooks.md
├── prompts/
│   ├── system.md
│   └── long_book_gen.md
├── chapters/
└── .bookforge/
```

### 编写原始文本

`outline.md` 和 `initial_hooks.md` 可以使用任意 Markdown 或纯文本格式。BookForge 原样读取并传给模型，不识别章节边界、标题、列表或钩子含义。

```markdown
<!-- outline.md -->
# 故事设定

主角发现一封预言第二天事件的信。

可能的情节走向：
- 收到来信
- 调查信件来源
- 预言成真
```

```markdown
<!-- initial_hooks.md -->
## 未解问题
- 谁写了这封信？
- 为什么信上的日期是明天？

连续性信息：主角的姐姐三年前失踪。
```

`prompts/system.md` 定义书籍文风；`prompts/long_book_gen.md` 提供长文规划规则，可以单独编辑。发送给模型的 system prompt 依次由文风指令、长文生成规则和固定输出协议组成。

### 配置 Quarto

`init` 会提供内置的 `_quarto.yml` PDF 书籍模板和 `index.qmd` 示例文件；已有文件不会被覆盖。版式包含适合书稿的开本、封面与正文排版，以及中英文、数学、目录和中文交叉引用支持；选择设计后，会套用对应的专属颜色、章首页、题字和页眉页码。模板中的空格保留设置确保 `# 第一章 这是一个标题` 在 PDF 标题中显示为“第一章 这是一个标题”。所有样式均可在 `_quarto.yml` 中继续调整。生成章节后，运行 `forge` 会自动同步 `book.chapters` 清单并调用配置的 Quarto 命令进行渲染：

```yaml
project:
  type: book

book:
  title: 我的书
  chapters:
    - index.qmd
    - chapters/001.qmd
    - chapters/002.qmd
    - chapters/003.qmd

format:
  pdf: default
```

章节文件使用三位数字生成序号命名，不会从大纲标题推导文件名。`forge` 保留 `index.qmd` 和其他非编号章节条目，更新编号章节列表为当前已生成章节，然后一步完成 PDF 编译。

## 生成直到全书结束

默认在当前目录查找配置。可用 `--project DIR` 指定项目，或用 `--config FILE` 指定 manifest；其中相对路径均相对于 manifest 所在目录。

```sh
./bookforge --project ./my-book validate
```

`validate` 检查配置和输入文件是否可读，以及现有状态是否一致；不会推断大纲章节或钩子语义。

将 API Key 设置在 `llm.api_key_env` 指定的环境变量中，默认是 `OPENAI_API_KEY`：

```sh
export OPENAI_API_KEY="你的 API Key"
./bookforge --project ./my-book generate
```

每次模型请求由一个包含三个有序部分的 system message 和一个 user message 组成：`prompts/system.md`、`prompts/long_book_gen.md`、固定输出协议；user message 则包含大纲、当前钩子文本和“只输出一章”的指令。生成序号只用于文件命名，不发送给模型。

模型响应必须严格使用以下两种格式之一：

```text
本章正文……
<<<BOOKFORGE_HOOKS>>>
下一轮使用的完整钩子文本。
```

```text
最后一章正文……
<<<END_OF_BOOK>>>
```

两个标记互斥。缺少、重复或同时出现标记，或正文为空时，响应视为无效。默认自动重试 3 次（总计最多 4 次请求）；可通过 `generation.invalid_output_retries` 修改额外重试次数。格式错误后，后续重试请求会临时附加“请注意格式”提示；章节提交后提示不会带入下一章。每个无效响应都会显示并保存到 `.bookforge/invalid-responses/`；重试耗尽后生成失败，不提交章节或状态。即使关闭完整审计，无效响应仍会保留。钩子块是任意文本，每轮新块整体替换当前状态，不会自动累加历史钩子。

默认逐章人工审核。无效输入（包括空行或空格）会提示并继续等待，不会退出程序：

- `a`：批准并提交；
- `A`：批准当前章节，并自动批准本次运行剩余章节；
- `v`：查看完整正文和钩子文本；
- `e`：使用配置的编辑器编辑正文，保存后再确认；
- `r`：输入可选修改意见，再请求模型重新生成；意见只用于当前章节，提交后不会继续带入后续章节；
- `q`：退出，不提交当前章节。

使用 `--auto` 可跳过人工审核：

```sh
./bookforge --project ./my-book generate --auto
```

控制台会显示每次模型请求进度；章节提交后显示排版后的摘要卡片，包括输出路径、钩子/完成状态，以及正文开头最多 30 行预览。

生成没有次数上限。BookForge 会在每章提交后继续调用模型，直到提交 `<<<END_OF_BOOK>>>`。中断后重新运行 `generate` 会从最近一次完整提交的章节和钩子快照继续；若最终标记已经提交，则提示全书完成并退出。

## 重写、查看和编译

```sh
./bookforge --project ./my-book log
./bookforge --project ./my-book hooks show
./bookforge --project ./my-book hooks show 2
./bookforge --project ./my-book rewrite 5
./bookforge --project ./my-book rewrite 5 --auto
./bookforge --project ./my-book clear
./bookforge --project ./my-book forge
```

`rewrite N` 会先展示解析出的目标章节文件并请求确认，再归档第 N 章及之后的所有章节、快照、审计和结束状态，恢复到 N−1 章后的状态，然后生成直到模型重新输出结束标记。`log` 输出 JSONL 操作历史。`hooks show [N]` 输出第 N 章后的原始钩子文本，不指定序号时显示最新状态。`clear` 会列出待删除的 BookForge 产物并要求确认，只清理工具管理的文件，保留用户输入、提示词、配置、Quarto 文件和无法识别的文件；清空完成后会留下本次清空操作的日志记录。`forge` 是唯一用于同步章节列表并编译 PDF 的命令。

如果 `outline.md`、`initial_hooks.md`、`prompts/system.md` 或 `prompts/long_book_gen.md` 在生成开始后发生变化，续跑会被拒绝。使用 `rewrite 1` 可归档旧链、从更新后的初始钩子重置并重新生成。

## 配置

`bookforge.yaml` 是项目清单，完整字段见 [`configs/config.example.yaml`](configs/config.example.yaml)。`init` 会写入 V1 默认配置。

| 配置项 | 用途 | 默认值 |
|---|---|---|
| `project.outline` | 不透明大纲文本文件 | `outline.md` |
| `project.initial_hooks` | 不透明初始钩子文本文件 | `initial_hooks.md` |
| `project.system_prompt` | 文风和书籍专属指令 | `prompts/system.md` |
| `project.long_book_rules` | 长文生成规则 | `prompts/long_book_gen.md` |
| `project.chapters_dir` | 编号 `.qmd` 输出目录 | `chapters` |
| `project.state_dir` | manifest、快照和审计数据 | `.bookforge` |
| `llm.provider` / `llm.model` | 模型服务和名称 | `openai` / `gpt-4.1` |
| `llm.api_key_env` | API Key 环境变量 | `OPENAI_API_KEY` |
| `llm.base_url` | OpenAI 兼容 endpoint | `https://api.openai.com/v1` |
| `llm.temperature`、`max_output_tokens`、`request_timeout` | 生成参数 | `0.7`、`12000`、`10m` |
| `llm.parameters` | 原样合并进模型 API 请求的任意字段 | `{}` |
| `llm.headers` | 额外 HTTP 请求头 | `{}` |
| `generation.review` | `interactive` 或 `auto` | `interactive` |
| `generation.editor` | 编辑章节的编辑器命令；未配置时编辑操作会显示错误并返回审核菜单 | 空 |
| `generation.store_prompt_and_response` | 审计中保存完整 prompt 和原始 response | `true` |
| `generation.invalid_output_retries` | 输出无效后的额外重试次数 | `3` |
| `quarto.project_dir` / `quarto.command` | Quarto 工作目录和可执行命令（可用 PATH 命令或绝对路径） | `.` / `quarto` |

`--auto`、`--model` 和 `--no-audit-full` 只影响本次运行。`llm.parameters` 的键值会直接合并进 API 请求，BookForge 不解释其含义；若服务端拒绝参数，会展示服务端错误。API Key 不会持久化；JSON 快照和审计由工具管理，用户无需编写 JSON。

例如，可以直接传递模型专属参数：

```yaml
llm:
  parameters:
    reasoning_effort: high
```

请自行确认所用模型 API 支持该参数；BookForge 会原样转发，不会解释或改写。

若服务端要求额外 HTTP 请求头，可使用字符串键值对配置：

```yaml
llm:
  headers:
    X-Api-Client: bookforge
```

请求头会按配置原样发送；若自定义请求头与默认请求头同名，自定义值会覆盖默认值。

## 状态和边界

- `.bookforge/manifest.json` 保存输入文件哈希，不保存生成序号。
- `.bookforge/snapshots/state_000.json` 保存初始钩子；`state_NNN.json` 保存当前钩子文本和全书完成标志。
- `.bookforge/audit/run_NNN.json` 保存每轮元数据，可按配置附带完整 prompt 和原始 response。
- `.bookforge/invalid-responses/` 无论是否开启完整审计都会保留格式错误的原始响应；`.bookforge/log.jsonl` 记录生成、审核、重试、重写、清空和 forge 操作。
- `chapters/NNN.qmd`、快照和审计文件共同识别已提交轮次；下一序号从连续完整产物推导。
- `<<<END_OF_BOOK>>>` 是唯一完成条件，完成状态保存在最终快照。
- 项目锁避免同一项目并发生成或渲染。
- 提示缓存由模型服务端决定；BookForge 将大纲置于 user prompt 前部，但不保证缓存命中。

架构说明见 [`docs/new_design.md`](docs/new_design.md)。开发检查可运行 `go test ./...`、`go test -race ./...` 和 `go vet ./...`。

安装 Quarto 和 XeLaTeX 后，可选运行五套完整 PDF 集成测试；PDF、`.tex` 和编译日志会留在指定目录供预览：

```sh
BOOKFORGE_RENDER_TEST_DIR=/tmp/bookforge-designs \
  go test ./internal/commands -run '^TestDesignerPDFs$' -count=1
```
