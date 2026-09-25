# BookForge 设计文档

当前设计和行为契约见 [`new_design.md`](new_design.md)。程序将 outline 和 hooks 视为不透明文本，只负责生成序号、协议分隔、提交状态和 Quarto 调用。

依赖方向：`cmd/bookforge` → `internal/commands` → `internal/orchestrator`；编排器依赖 `llm.Client` 与存储接口，具体 OpenAI、文件存储和 Quarto 实现位于基础设施包。
