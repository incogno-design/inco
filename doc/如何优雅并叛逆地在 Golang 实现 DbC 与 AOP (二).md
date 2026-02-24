# 如何优雅并叛逆地在 Golang 实现 DbC 与 AOP (二) — 代码视觉重心

> 引擎仓库：[github.com/imnive-design/inco](https://github.com/imnive-design/inco)

## 一、守卫 vs 逻辑

上一篇我们拿到了一套编译期代码注入基础设施。现在问题变成了：**注入什么？**

先看一段常见的 Go 代码：

```go
func Transfer(from *Account, to *Account, amount int) error {
    if from == nil || to == nil {
        return fmt.Errorf("account must not be nil")
    }
    if from == to {
        return fmt.Errorf("cannot transfer to self")
    }
    if amount <= 0 {
        return fmt.Errorf("amount must be positive")
    }
    if from.Balance < amount {
        return fmt.Errorf("insufficient funds: have %d, need %d", from.Balance, amount)
    }

    from.Balance -= amount
    to.Balance += amount
    return nil
}
```

现实中多数人会这样写——合并条件、统一 `return error`。比起每个参数一个 `if` + `panic`，这已经算克制了。

但问题不在于写法是否合理。问题在于：这 4 个 `if` 和下面的业务逻辑**长得一样**。相同的关键字、相同的缩进、相同的三行结构。读代码时必须逐个扫描才能判断"这是守卫还是逻辑"。

这些 `if` 不是业务分支——它们是**前置条件**。它们的存在不是为了"做什么"，而是为了"确保可以做"。

**守卫和逻辑在语义上是两个层次的东西，但在视觉上不可区分。**

### 比 DbC 和 AOP 更进一步

传统 DbC 需要语言级支持（Eiffel 的 `require`/`ensure`），传统 AOP 需要运行时代理或编译器插件（AspectJ）。两者都引入了额外的基础设施。

Inco 不需要。它用 Go 已有的东西——注释、overlay、AST——在不修改语言的前提下，同时实现了前置条件声明（DbC）和横切关注点分离（AOP）。守卫是契约，也是切面。一套语法，两个范式。

## 二、语法设计

### 正向表达

`// @inco: <expr>` 的语义是"expr 必须为 true"。写你**期望成立**的条件：

```go
// @inco: err == nil       // 期望无错误
// @inco: n > 0            // 期望 n 为正
// @inco: !skip            // 期望不跳过
```

传统 `if` 写的是**违约条件**（`if err != nil`），思维方向是反的。正向表达让注释读起来像契约声明，而不是错误处理。

### 违约动作

默认动作是 panic。需要其他行为时用后缀指定：

```go
// @inco: x != nil                              // → panic（自动消息）
// @inco: x != nil, -panic("x is nil")          // → panic（自定义）
// @inco: err == nil, -return(nil, err)          // → return
// @inco: n > 0, -continue                      // → continue
// @inco: n != target, -break                    // → break
// @inco: x > 0, -log("unexpected:", x)          // → log.Println
```

每条指令一行。守卫的密度从 3 行/个压缩到 1 行/个。

### Standalone vs Inline

**Standalone**——整行是注释，用于函数入口的参数校验：

```go
func Transfer(from *Account, to *Account, amount int) error {
    // @inco: from != nil
    // @inco: to != nil
    // @inco: from != to, -panic("cannot transfer to self")
    // @inco: amount > 0, -panic("amount must be positive")
    // @inco: from.Balance >= amount, -return(fmt.Errorf("insufficient funds: have %d, need %d", from.Balance, amount))

    from.Balance -= amount
    to.Balance += amount
    return nil
}
```

同样的函数，5 行守卫 + 3 行逻辑。守卫是注释色，逻辑是代码色。眼睛可以直接跳过注释区，落在业务逻辑上。

**Inline**——代码行尾部追加指令，用于变量只在指令中使用的场景：

```go
user, err := db.Query("SELECT * FROM users WHERE id = ?")
_ = err // @inco: err == nil, -return(nil, err)
```

`_ = err` 消除 unused variable 报错，指令跟在同一行。不额外占行。

### 展开对照

写的是：

```go
// @inco: from != nil
```

编译器看到的是：

```go
//line transfer.inco.go:14
if !(from != nil) {
    panic("inco violation: from != nil (at example/transfer.inco.go:14)")
}
//line transfer.inco.go:15
```

1 行注释 → 3 行 `if` 块。视觉压缩比 3:1。

## 三、信任链模型

### 函数入口即信任边界

传统 DbC 有 precondition（前置条件）和 postcondition（后置条件）。完整的契约系统需要在函数入口和出口都做检查。

Inco 只做前置条件——**单向守卫**。

原因很简单：Go 的错误处理模式本身就是单向的。函数签名 `(result, error)` 已经是一种 postcondition 协议。调用方通过检查 `err` 来验证后置条件。再加一层 postcondition 断言是重复的。

信任链的流向：

```
调用方 → @inco: 检查参数 → 函数体执行 → return (result, err) → 调用方检查 err
```

每个函数入口是一个信任边界。`@inco:` 在边界上设卡，通过即可信任。函数内部不需要反复验证已经检查过的条件。

### 守卫不是逻辑

区分的标准：

| | 守卫 | 逻辑 |
|--|------|------|
| 目的 | 确保可以执行 | 决定如何执行 |
| 违约 | 异常（panic/error） | 正常分支 |
| else | 没有 | 通常有 |
| body | 单一动作 | 可能多行 |
| 示例 | `if x == nil { panic }` | `if x < lo { return lo }` |

`@inco:` 只替代守卫。有 else 的、body 多行的、属于业务分支的 `if`——不动。

## 四、视觉量化

### 守卫密度

`inco audit` 报告两个指标：

**inco/(if+inco)**——守卫指令占所有条件检查的比例。

```
@inco::           12
Native if stmts:  8
inco/(if+inco):   60.0%
```

目标 > 50%。意味着大部分 `if` 是真正的业务逻辑，守卫已被指令吸收。

**函数覆盖率**——有至少一个 `@inco:` 的函数比例。

```
With @inco::     15 / 20  (75.0%)
Without @inco::   5 / 20  (25.0%)
```

剩余的函数要么确实不需要守卫，要么是审计遗漏。

### 审计与代码质量

这两个指标不是 KPI，是**诊断工具**。

一个函数里 `if` 的总量是固定的——守卫 + 逻辑。当守卫被 `@inco:` 吸收后，剩下的 `if` 就是纯业务逻辑。`inco/(if+inco)` 越高，说明代码中残留的 `if` 越"干净"。

反过来看：如果一个文件 `inco/(if+inco)` 很低，意味着大量 `if` 仍然混杂着守卫和逻辑——读这个文件时的认知负荷更高。

这实际上量化了一个一直难以度量的东西：**逻辑密度**。

传统代码质量工具衡量的是圈复杂度（cyclomatic complexity）——每个 `if` 加 1，不区分守卫还是逻辑。一个有 10 个 nil 检查 + 2 个业务分支的函数，圈复杂度是 12，和一个有 12 个业务分支的函数一样。但两者的实际复杂度天差地别。

Inco 的审计把守卫从 `if` 计数中剥离。剩余的 `if` 数量更接近函数的**真实逻辑复杂度**。审计报告不是在说"你该加更多守卫"，而是在说"你的代码有多少比例是在做真正的决策"。

### 缩进层级

传统守卫 `if` 引入一层缩进。5 个守卫 = 原始代码被推到至少第 2 层缩进才开始。

`@inco:` 是注释，不引入缩进。函数体的业务逻辑始终从第 1 层缩进开始。

### 扫描路径

读一个函数时的眼动路径：

**传统**：`if` → body → `if` → body → `if` → body → ...终于到业务代码。逐行判断每个 `if` 是守卫还是逻辑。

**@inco:**：注释块（灰色，一扫而过）→ 空行 → 业务代码。守卫和逻辑的视觉边界是**颜色**，不是结构。

---

*下一篇：影子坍缩与生态兼容 —— 方言到通用语的翻译机制。*
