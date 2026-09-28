# Jus

**Jus 系列应用（Jusplay、Jusnote）共用的 Go 模块，以及 `jus` 命令。**
The shared Go module of the Jus apps, and the `jus` command.

每个 Jus 应用都用这里的代码做同样的几件事：把自己能做的事登记一次，由此生成命令行、`help -json` 和窗口调用的接口；命令行找到正在运行的窗口并把调用交给它；声明自己写过哪些地方，以便干净卸载；运行文件夹里自带、经用户确认过的工具。

## 包

| 包 | 做什么 |
|---|---|
| `capreg` | 能力登记表：每项能力登记一次，生成命令行、`help -json`、窗口的 `api/cap`；参数、来源（谁在调用）、需要确认的操作、固定的退出码、`-json` 错误格式 |
| `instance` | 单实例：窗口在数据目录里报到，命令行找到它并把调用交给它 |
| `textenc` | 文本解码：UTF-8、UTF-16、Shift-JIS、GB18030、Big5 |
| `apps` | 应用的清单与足迹；找到别的 Jus 应用、调用它们的能力；卸载计划（只删确实属于该应用的地方） |
| `conform` | 一致性测试：命令行、退出码、错误格式、清单与足迹 |
| `skills` | 文件夹里自带的工具：发现、按版本确认、运行 |
| `procstat` | 进程退出时报告自己的峰值内存 |

```text
go get github.com/Tieumi221E/Jus@v0.1.0
```

## `jus` 命令

```text
go build -o jus.exe ./cmd/jus
```

```text
jus apps [-json]                 这台电脑上的 Jus 应用：各自的版本、链接、数据格式、写过的地方
jus open <jus://…>               把链接交给对应的应用（jus://note/… 给 Jusnote，jus://play/… 给 Jusplay）
jus uninstall <应用> [-yes]      卸载：不加 -yes 只列出要删的；你的内容（笔记、历史、观看记录等）只列出、不删
jus conform <exe>                对一个应用跑一致性测试
jus help [-json] | version
```

应用在这些地方里找：`JUS_<APP>_EXE` 环境变量、`jus.exe` 旁边、旁边以应用名命名的文件夹（发布包并排解压）、PATH。不在注册表或别处登记任何东西。

退出码：0 成功，1 失败，2 用法错误（或需要 `-yes`），3 冲突。加 `-json` 时，结果以 JSON 输出，失败时在 stderr 输出 `{"error", "kind", "code"}`。

## 要求

Go 1.27 或更新。Jus 应用目前只有 Windows 版；`procstat` 的内存测量也只在 Windows 上实现。

## 许可证

MIT，见 [LICENSE](LICENSE)。
