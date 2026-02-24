# 如何优雅并叛逆地在 Golang 实现 DbC 与 AOP (一) — 预编译宏

> *Clarent Blood Arthur*

## 一、问题定义

在 Go 中实现 DbC 或 AOP，传统方案无一例外地做了妥协。

### `go generate`

- 生成的 `*_gen.go` 污染项目目录和 Git 历史。
- 源码改了忘了跑 gen，编译器不会告诉你。静默过期。

### 反射

- 强类型退化为 `interface{}`，编译期检查全部失效。
- 运行时开销高出数量级，IDE 静态分析同步失效。

### 装饰器

- 5 行业务逻辑被 20 行样板代码包裹。本末倒置。

三种方案，三种入侵。

## 二、技术选型：`go build -overlay`

Go 工具链有个后门。

自 Go 1.16 起，`go build` 支持 `-overlay` 参数，接受一个 JSON：

```json
{
  "Replace": {
    "/project/foo.go": "/project/.cache/foo_shadow.go"
  }
}
```

语义：**编译时将 key 路径透明替换为 value 路径的内容**。源文件不动一个字节，编译器吃的是影子文件。

```bash
go build -overlay=overlay.json ./...
```

编译器行为：读取 `overlay.json` → 构建文件映射表 → 对命中的路径，从影子文件读取源码 → 正常编译。未命中的文件不受影响。`go test`、`go vet` 同理。

这就是宏。

|  | `go generate` | `-overlay` |
|--|---------------|------------|
| 生成物 | 项目目录内 | 缓存目录 |
| 源文件 | 需嵌入指令 | 零修改 |
| 生成 | 手动 | 手动（可封装） |
| 映射 | 手动（开发者维护源与生成物的关系） | 自动（overlay.json 声明式映射） |
| 报错定位 | 生成文件 | 原始文件（`//line`） |

### `//line` 指令

影子文件插入了额外代码，行号会错位。编译器内部指令：

```go
//line /path/to/original.go:42
```

从此行起，编译器假装代码来自 `original.go` 第 42 行。`cgo` 和 yacc 的生成代码中随处可见。

展开效果：

```go
// 源文件 math.go:5 → // #assert: x > 0

// 影子文件
//line math.go:5
if !(x > 0) {
    panic("assertion failed: x > 0 (at math.go:5)")
}
//line math.go:6
```

panic 堆栈指向 `math.go:5`，不是 `_shadow.go:17`。

## 三、实现路径

### 3.1 源码解析与 AST 遍历

用 `go/parser` 解析 AST，遍历 `*ast.File` 的注释节点：

```go
fset := token.NewFileSet()
f, _ := parser.ParseFile(fset, path, nil, parser.ParseComments)

directives := map[int]*Directive{} // 行号 → 指令
for _, cg := range f.Comments {
    for _, c := range cg.List {
        if d := ParseDirective(c.Text); d != nil {
            line := fset.Position(c.Pos()).Line
            directives[line] = d
        }
    }
}
```

AST 还能区分 standalone（整行注释）和 inline（代码行尾部注释）——这决定了影子文件中守卫代码的插入位置。

### 3.2 Shadow 文件生成

逐行构建输出：

1. **standalone 指令行** → 保留注释，紧接插入 `//line` + `if` 守卫块
2. **inline 指令行** → 保留原始代码行，之后插入守卫块
3. **普通行** → 直接输出

生成完毕后，扫描指令中的包引用（`fmt.Errorf`、`filepath.SkipDir` 等），对源文件缺失的 import 用 `astutil.AddImport` 自动注入。

## 四、最小实现

注释写 `// #assert: <expr>`，展开为 `if !(<expr>) { panic(...) }`。

```go
// main.go
package main

import "fmt"

func Divide(a, b int) int {
    // #assert: b != 0
    return a / b
}

func main() {
    fmt.Println(Divide(10, 2))
    fmt.Println(Divide(10, 0))
}
```

引擎核心（省略 import 和 error handling）：

```go
var re = regexp.MustCompile(`//\s*#assert:\s*(.+)`)

func generate(path string) (shadow string, ok bool) {
    data, _ := os.ReadFile(path)
    var out []string
    for i, line := range strings.Split(string(data), "\n") {
        out = append(out, line)
        if m := re.FindStringSubmatch(line); m != nil {
            ln := i + 1
            out = append(out,
                fmt.Sprintf("//line %s:%d", path, ln),
                fmt.Sprintf("if !(%s) {", m[1]),
                fmt.Sprintf(`    panic("assert: %s (%s:%d)")`, m[1], path, ln),
                "}",
                fmt.Sprintf("//line %s:%d", path, ln+1))
            ok = true
        }
    }
    if !ok { return "", false }
    p := filepath.Join(".macro_cache", filepath.Base(path))
    os.WriteFile(p, []byte(strings.Join(out, "\n")), 0o644)
    abs, _ := filepath.Abs(p)
    return abs, true
}
```

生成 `overlay.json`，然后：

```bash
go build -overlay=.macro_cache/overlay.json -o myapp .
./myapp
# 5
# panic: assert: b != 0 (main.go:6)
```

## 五、零入侵验证

回头审视这套机制的入侵性——答案是零。

- **`go.mod`**：不需要修改。overlay 是工具链 flag，不是依赖。
- **`import`**：源文件不需要额外 import。影子文件的 import 由引擎自动注入。
- **工具链兼容**：`go build -overlay` 是标准 flag，不依赖任何第三方补丁。`go test -overlay`、`go vet -overlay` 同样适用。
- **IDE 兼容**：源文件是合法的 Go 代码（注释不影响语法），LSP、gopls 正常工作。
- **可逆性**：删掉缓存目录，一切回到原点。没有任何生成物残留在项目中。

不改文件，不改依赖，不改工具链。只在编译那一瞬间，影子文件短暂存在。

`go build -overlay` + `//line` = 编译期代码注入基础设施。

Go 没有宏？它一直有。藏在一个 flag 里。

---

*下一篇：代码视觉重心 —— 基于信任链模型的守卫语法设计。*
