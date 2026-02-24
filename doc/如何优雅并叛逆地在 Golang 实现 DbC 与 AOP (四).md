# 如何优雅并叛逆地在 Golang 实现 DbC 与 AOP (四) — IDE 支持与 AI 共生

> 引擎仓库：[github.com/imnive-design/inco](https://github.com/imnive-design/inco)
>
> 插件仓库：[github.com/imnive-design/inco-extension](https://github.com/imnive-design/inco-extension)

## 一、可见性危机

上一篇解决了方言的分发问题——坍缩后的代码是标准 Go，接收方零成本。但开发态的问题还在：方言的代价不只是"需要翻译"，还有一个更隐蔽的问题：**工具链看不见它**。

`gopls` 不知道 `// @inco: x != nil` 是一条守卫指令。对它来说，这就是一条普通注释——不参与类型检查，不影响控制流分析，不产生诊断信息。

具体表现：

- **无语法高亮**：`@inco:` 和 `// TODO:` 视觉上没有区别。守卫指令淹没在注释里。
- **无错误检查**：写了 `// @inco: x != nill`（拼错了），LSP 不会报错。直到 `inco gen` 或编译时才爆出来。
- **无悬停预览**：鼠标悬停在指令上，看不到展开后的 `if` 块。开发者必须在脑中做展开。
- **无密度感知**：一个函数有多少守卫、覆盖率多少，IDE 不告诉你。

gopls 的职责是服务 Go 语言规范。`@inco:` 不在规范里，所以 gopls 的行为完全正确。

问题在方言这一侧：**你发明了一种语法，就得为它提供可见性。**

## 二、VSCode 插件

### 语法高亮

最基本的需求：让 `@inco:` 指令和普通注释在视觉上可区分。

TextMate grammar 注入 Go 语言的注释作用域，匹配 `@inco:` 及其后续的表达式和动作后缀。效果：

- `@inco:` 关键字 → 独立颜色（如蓝色），与灰色注释区分。
- 条件表达式 → 代码色。
- 动作后缀（`-panic`、`-return`）→ 关键字色。

一眼就能识别哪些注释是守卫指令，哪些是普通注释。

### 实时诊断

插件在文件保存时解析 `@inco:` 指令，检查：

- 语法正确性（表达式为空、动作格式错误）。
- 已知的常见错误模式（如拼写错误的动作名）。

诊断结果通过 LSP 的 Diagnostic 接口推送，编辑器中直接显示波浪线和错误信息。不需要等到 `inco gen` 才发现问题。

### Hover 预览

鼠标悬停在 `@inco:` 指令上时，弹出展开后的 `if` 守卫块：

```
// @inco: from != nil
↓ 展开为：
if !(from != nil) {
    panic("inco violation: from != nil (at example/transfer.inco.go:14)")
}
```

消除"脑中展开"的认知负荷。

### CodeLens 密度统计

在每个函数声明上方显示 CodeLens：

```
@inco: 5 guards | if: 2 stmts | density: 71.4%
func Transfer(from *Account, to *Account, amount int) error {
```

函数级的守卫密度一目了然。没有守卫的函数不显示 CodeLens——"无标注 = 未覆盖"的视觉暗示。

### 命令集成

- **保存时自动 gen**：文件保存触发 `inco gen`，影子文件实时更新。搭配 `-overlay` 的构建流程，保存即编译。
- **命令面板**：`Inco: Build`、`Inco: Test`、`Inco: Audit`、`Inco: Release` 一键触发。
- **审计报告面板**：`Inco: Audit` 的结果以结构化视图呈现，点击可跳转到未覆盖的函数。

### 自动补全的缺位

插件能解决高亮、诊断、预览，但有一个能力它给不了：**自动补全**。

gopls 的补全基于 Go 语法和类型系统——它能补全变量名、函数签名、结构体字段。但 `// @inco:` 是注释，gopls 不会在注释中提供表达式补全，也不会建议动作后缀。

这意味着写 `@inco:` 指令时，没有 Tab 补全，没有参数提示，没有类型检查反馈。开发者需要记住语法，手动输入。

这个缺口由 AI 填补。Copilot 读取 `copilot-instructions.md` 后，能在注释中补全完整的 `@inco:` 指令——包括条件表达式和动作后缀。它不是 LSP 级的精确补全，但覆盖了 gopls 完全无法触及的区域。

插件做可见性，AI 做生产力。两者互补。

## 三、AI 共生协议

### `copilot-instructions.md` 作为方言字典

GitHub Copilot 读 `.github/copilot-instructions.md`。这个文件是仓库级的 AI 指令。

Inco 的 `copilot-instructions.md` 包含完整的方言规范：

```markdown
# Inco — Copilot 使用手册

## 核心规则
### 1. `@inco:` 是守卫，不是逻辑
**用 `@inco:`**：nil 检查、错误检查、范围验证、前置条件
**用 `if`**：业务分支、条件选择、流程控制

### 2. 两种指令形式
**Standalone**（整行是注释）：
// @inco: x != nil

**Inline**（代码行尾部追加指令）：
_ = err // @inco: err == nil, -panic(err)

### 3. 可用动作
| 动作 | 语法 | 含义 |
| panic | `// @inco: <expr>` | 自动生成 panic 消息 |
| return | `// @inco: <expr>, -return(vals...)` | 返回指定值 |
| continue | `// @inco: <expr>, -continue` | continue 循环 |
...

### 4. 指令语义
写你期望成立的条件（正向表达）。
```

效果：Copilot 在 `.inco.go` 文件中补全代码时，会优先使用 `@inco:` 指令而不是原始 `if` 来写守卫。它知道动作后缀的语法，知道 standalone 和 inline 的使用场景，知道哪些 `if` 不该转换。

### 从字典到协议

`copilot-instructions.md` 的本质是**人机之间的方言协议**。

传统的 AI 辅助编程是单向的：AI 学习语言规范，按规范生成代码。但方言不在规范里。如果不告诉 AI "这个项目有一套额外的语法约定"，它会按标准 Go 来写——所有守卫都是 `if`，你得手动转换。

`copilot-instructions.md` 把方言的规则注入到 AI 的上下文中。AI 不再是"被动地使用工具链"，而是"主动地说这种方言"。

这是一种共生：

- **开发者**定义方言规则，写入 `copilot-instructions.md`。
- **AI** 读取规则，按方言生成代码。
- **引擎**处理 AI 生成的方言代码，产出守卫。
- **审计**检测覆盖率，反馈给开发者和 AI。

闭环。

### DbC 与 AOP 在 AI 生态下的意义

回到标题——为什么是 DbC 和 AOP？

**DbC（Design by Contract）**是函数入口的前置条件声明。传统 DbC 要求显式标注 precondition / postcondition / invariant，由编译器或运行时验证。Eiffel 有语言级支持，Java 有 JSR 380（Bean Validation），但大多数语言——包括 Go——没有。

**AOP（Aspect-Oriented Programming）**是横切关注点的分离。守卫逻辑（nil 检查、范围验证、错误检查）散布在每个函数入口，和业务逻辑正交，但在传统写法中与业务代码混为一体。AspectJ 通过编译器插件实现切面织入，Spring AOP 通过运行时代理。Go 都没有。

Inco 的 `@inco:` 指令同时实现了两者：它是 precondition 的编译期声明（DbC），也把守卫关注点从代码结构中抽离到注释层（AOP）。

但这不是重点。重点是：**DbC 和 AOP 的形式化特征让它们天然适合 AI 协作。**

DbC 的契约是声明式的——"这个条件必须成立"。没有分支，没有副作用，没有上下文依赖。AI 生成一条 `// @inco: x != nil` 的正确率远高于生成一段复杂业务逻辑。因为守卫的正确性可以从函数签名和参数类型直接推导。

AOP 的横切关注点是模式化的——nil 检查、错误检查、范围验证，模式高度重复。AI 最擅长的就是识别和复制模式。告诉它规则（`copilot-instructions.md`），它能在整个项目中一致地应用。

传统编程中，DbC 和 AOP 的主要阻力是**样板代码太多**——写 10 个 precondition 的工作量让人放弃。AI 消除了这个阻力。守卫的声明成本趋近于零，剩下的唯一成本是审计和验证。而 `inco audit` 恰好提供了验证。

DbC + AOP + AI = 契约覆盖率不再是奢侈品。

### Vibe Coding 之后的 Code Review

AI 生成代码的速度越来越快。Vibe coding——让 AI 一口气写出大量代码——正在成为常态。但生成容易，review 难。

一个 AI 生成的函数，可能有 15 行守卫 + 5 行逻辑。Review 时你需要逐行确认每个 `if` 的正确性，同时还要理解业务逻辑。守卫和逻辑混在一起，视觉压力成倍放大。

Inco 把这个压力拆成两步：

1. **扫一眼注释块**——`@inco:` 指令是声明式的，一行一个条件，每条独立。确认守卫是否合理，几秒钟的事。
2. **专注业务逻辑**——注释区之后的代码就是纯逻辑。不需要在守卫和逻辑之间反复切换。

视觉分离降低了 review 的认知负荷。你不再需要问"这个 `if` 是守卫还是逻辑"——注释色的是守卫，代码色的是逻辑。颜色本身就是答案。

AI 生成的代码量越大，这个优势越明显。

### if → @inco: 转换模板

`copilot-instructions.md` 中最实用的部分是转换模板：

```go
// 转换前：
if err != nil { return nil, err }
// 转换后：
_ = err // @inco: err == nil, -return(nil, err)

// 转换前：
if x == nil { panic("x is nil") }
// 转换后：
// @inco: x != nil, -panic("x is nil")
```

AI 在看到现有的 `if` 守卫后，可以建议转换为 `@inco:` 形式。不是替你写新代码——而是帮你把旧代码迁移到方言。

### 不要转换的 if

同样重要的是告诉 AI **什么不能转换**：

- 有 else 的条件
- body 多行的条件块
- 业务逻辑分支
- 含副作用的条件

AI 知道边界在哪里，就不会过度转换。

## 四、反馈循环

### audit → 开发者 → AI → audit

`inco audit` 的输出不只是给人看的。

```
Functions without @inco: (5):
  internal/inco/engine.inco.go:310  commitResults
  internal/inco/engine.inco.go:445  addMissingImports
  ...
```

这份列表告诉开发者哪些函数缺少守卫。开发者可以直接去补，也可以让 AI 在这些函数中生成 `@inco:` 指令——AI 已经知道语法规则，只需要告诉它"这个函数需要守卫"。

补完后再跑一次 `inco audit`，密度上升，未覆盖列表缩短。

这是一个正向反馈循环：

```
audit 报告覆盖率不足
→ 开发者/AI 补充 @inco: 指令
→ 守卫密度上升
→ 再次 audit 验证
→ 交付
```

守卫覆盖率不是目标，是信号。它告诉你"还有多少防御性检查没有被显式声明"。目标是让剩余的每一个 `if` 都是真正的业务逻辑。

---

*下一篇：设计开发心路历程 —— 工程实践背后的约束突破与设计权衡。*
