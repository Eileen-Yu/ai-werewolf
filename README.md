# catalyst-werewolf

一局 6 人 AI 狼人杀，主持人是一个 Dapr Workflow。

演示的看点：游戏进行到一半把主持人进程杀掉，再启动一个，游戏从断点继续。已经说过的话不会重新生成，LLM 不会被重复调用。

## 结构

| 目录 | 内容 |
|---|---|
| `game/` | 规则：发身份、谁能看到什么、计票、胜负。纯函数，不依赖 Dapr |
| `brain/` | 玩家怎么想：`random`（随机，免费）和 `llm`（Dapr Conversation API） |
| `werewolf/` | 主持人 workflow。每轮一次执行，用 continue-as-new 进入下一轮 |
| `internal/engine/` | 本地 workflow 引擎（durabletask + SQLite），不装 Dapr 也能玩 |
| `cmd/werewolf/` | 命令行：`engine`、`gm`、`start` |

信息隔离由主持人负责：每个玩家每次只收到 `State.VisibleTo(自己)` 的那部分记录。狼人的夜聊只有狼人能看到，验人结果只有预言家能看到。

随机数和 LLM 调用都只发生在 activity 里，workflow 本身是确定性的。这是"杀掉还能续上"的前提。

## 本地玩（不需要 Dapr）

开三个终端：

```bash
go run ./cmd/werewolf engine                          # 本地引擎，状态存在 werewolf.db
go run ./cmd/werewolf gm -engine localhost:4001       # 主持人；它的输出就是上帝视角的直播
go run ./cmd/werewolf start -engine localhost:4001    # 开一局，等结果
```

演示杀进程：游戏进行中按 Ctrl+C 停掉 `gm`，过几秒再启动一次。游戏会接着往下走，前面的内容不会重复打印。引擎也可以一起杀掉：状态在 `werewolf.db` 里，重启引擎后再启动 `gm` 照样能续上。

`-delay` 设置阶段之间的停顿（默认 2 秒）。这个停顿是 durable timer，同样能扛住重启。

## 在 Catalyst 上玩（还没验证过）

本地引擎只支持 `random` 大脑。要让 LLM 来玩，需要真正的 Dapr，`gm` 和 `start` 不加 `-engine` 时就会连 Dapr。

1. 在项目里建一个 conversation component，名字叫 `llm`。或者另取一个名字，再通过 `-llm-component` 传给 `gm`：

   ```bash
   diagrid component create llm --type conversation.openai --metadata key=<你的 key> --metadata model=gpt-4o-mini
   ```

2. 用 `diagrid dev run` 跑主持人和开局，它会注入 `DAPR_GRPC_ENDPOINT` 和 `DAPR_API_TOKEN`：

   ```bash
   diagrid dev run --project <项目> --id werewolf -- go run ./cmd/werewolf gm -brain llm
   diagrid dev run --project <项目> --id werewolf -- go run ./cmd/werewolf start
   ```

一局大约 60 次 LLM 调用。

## 测试

```bash
go test ./...
```

`werewolf/workflow_test.go` 在测试进程里起一个内存引擎，完整跑一局。其中一个用例会在中途杀掉主持人 worker，换一个新的接手，检查游戏照样能打完。

## 下一步

- 观战网页，加一个"拔电源"按钮
- 真人玩家：投票改成 `WaitForExternalEvent`，超时视为弃票
- 女巫、猎人
