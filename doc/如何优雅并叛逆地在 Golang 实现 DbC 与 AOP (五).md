# 如何优雅并叛逆地在 Golang 实现 DbC 与 AOP (五) — 心路历程

> 引擎仓库：[github.com/imnive-design/inco](https://github.com/imnive-design/inco)

## 随笔

### 3.5 天

从第一个 commit 到自举完成，3.5 天。

```
02-18  3 commits   项目初始化，type checking，开始思考范式
02-19  5 commits   指令系统重写，release 机制
02-20  17 commits  引擎核心重写，inline 指令，审计系统，auto-import，copilot-instructions
02-21  12 commits  增量构建，并行处理，.incoignore，dry-run
02-22  6 commits   收尾润色
```

43 个 commit。1708 行源码，1812 行测试。

02-20 是密度最高的一天——17 个 commit，基本上把引擎从里到外翻了一遍。指令解析重写、standalone/inline 分类、审计系统的 if 计数、auto-import 的包名映射、copilot-instructions.md 的编写，全在这一天。

### 范式转移

最有意思的是 02-19 的一个 commit message：

```
paradigm shift to If治事 Inco治物
```

这是整个项目的转折点。之前我试过 `@ensure`（后置条件）、`@must`（强制检查），反复改关键字，想做完整的 DbC——precondition + postcondition + invariant。

然后我意识到：Go 的 `(result, error)` 返回模式**已经是** postcondition。调用方 `if err != nil` 就是在验证后置条件。再加一层 `@ensure` 是重复的。

砍掉 postcondition，只留 precondition。把 `@must` 改成 `@inco:`。从"完整的 DbC 系统"退到"只做守卫"。这个减法比之前所有的加法都重要。

### 偷鸡

AI 骂我偷鸡。不止一次。

它说 Go 社区不喜欢"把代码藏起来"。Go 的哲学是显式的——你看到什么就是什么，没有隐式行为，没有魔法。overlay 把守卫"藏"在影子文件里，编辑器里看到的是注释，编译器吃的是 if。这违反了 Go 的显式原则。

它说得对。所以我做了三件事：

1. **IDE 插件**——Hover 预览展开后的 if 块，CodeLens 显示守卫密度。看不到不行，那就让你看到。
2. **`inco release`**——坍缩后所有守卫变成标准 if。发布出去的是普通 Go 代码，没有任何隐藏。
3. **`copilot-instructions.md`**——告诉 AI 这些注释的含义。AI 参与编写，也就接受了这套"偷鸡"的规则。

偷鸡归偷鸡，但是偷完之后把鸡还回去了。

### 本质上很简单

Gemini 的总结精准得让人没什么好补充的：

> 做了个底层引擎 + CLI 工具 + IDE 插件 + 审计系统的全栈式项目。

全栈——但每一层都不复杂。引擎核心就是 AST 遍历 + 字符串拼接。CLI 是一个 switch-case。审计是数 if 和数 @inco:。Release 是文件复制 + 重命名。

整套系统的本质：**在 Go 里通过 overlay 和 AST 插入 if**。

Go 社区拒绝 macro。所以我用 Go 已有的机制组合出了一个 macro 系统，然后用这个 macro 系统插入 Go 社区最喜欢的东西——if。

一个极其简洁的循环。

### 甚至不需要怎么维护

引擎的输入是注释，输出是 if 语句。

注释的语法不会变——Go 的注释格式从 1.0 到现在没变过。if 的语法不会变——这是语言的基本控制结构。overlay 不会变——它是工具链的标准 flag。AST 不会变——`go/parser` 和 `go/token` 是标准库。

四个不变的东西组合出来的系统，维护成本趋近于零。

除非 Go 哪天真的加了 macro——那我可以退休了。

### 自举

Inco 的全部源码都是 `.inco.go`。引擎解析自己的 `@inco:` 指令，生成自己的影子文件，通过 overlay 编译自己。

```
inco build ./...
```

这一行编译的对象包括 inco 自身。如果引擎有 bug——比如误解析了一条指令，或者生成了非法的 if 语句——编译直接失败。没有"引擎能编译别人的代码但编译不了自己"的尴尬场景。

自举是最严格的集成测试。但光靠自举不够——1812 行单元测试覆盖了指令解析、引擎生成、审计统计、ignore 规则等每个模块。自举证明"整体能跑"，单元测试证明"每个零件都对"。

### *Clarent Blood Arthur*

Inco 不是 Go 语言的正统扩展。它不改语言，不改编译器，不改工具链。它用规则本身去突破规则——在 Go 允许的范围内，做了 Go 设计者大概没预想过的事情。

优雅并叛逆。

---

完。
