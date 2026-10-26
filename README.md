# catalyst-werewolf

一局 9 人 AI 狼人杀，主持人是一个 Dapr Workflow。

演示的看点：游戏进行到一半把主持人进程杀掉，再启动一个，游戏从断点继续。已经说过的话不会重新生成，LLM 不会被重复调用。

## 规则

标准 9 人局：3 狼人、预言家、女巫、猎人、3 村民。

- 夜里：狼人先密谈，定下刀谁、白天怎么打，然后投票刀人；女巫知道今晚谁被刀，决定救不救、毒不毒（两瓶药各用一次，同一晚最多用一瓶，不能自救）；预言家查验一个人。
- 天亮：只公布谁死了，不公布死因。猎人出局时可以开枪带走一人，被毒死的除外。
- 白天：按轮换的顺序每人发言一次，然后是回应环节，每人再说一次，可以回应怀疑或者质疑别人；最后投票，平票无人出局。被放逐的人有遗言。
- 胜负按屠边规则：狼人全部出局则好人胜利；村民全部出局，或者神职（预言家、女巫、猎人）全部出局，则狼人胜利。

## 每个玩家知道什么

LLM 玩家每次行动收到的上下文分成四块：

- 【座位和状态】：每个人是否存活。死了的人只写公开的死法，比如"第2夜死亡""被投票放逐"。
- 【你的秘密信息】：自己的身份、狼同伴和夜聊内容、查验结果、女巫的药。
- 【你之前的私下想法】：这个玩家自己每次行动前写下的分析，也就是它的私有记忆。
- 【公开记录】：所有人都看到的内容，也是唯一允许引用的历史。

发言时还会告诉它：这一轮谁已经说过、谁还没说。

每次行动都返回 JSON：`thinking` 是私下想法，`speech` 是说出口的话，`target` 是选中的人。选了名单之外的人，比如投给自己，会把错误告诉它让它重答，最多重答 2 次，还不行才算弃权。

## 结构

| 目录 | 内容 |
|---|---|
| `game/` | 规则：发身份、谁能看到什么、计票、胜负。纯函数，不依赖 Dapr |
| `brain/` | 玩家怎么想：`random`（随机，免费）和 `llm`（Dapr Conversation API） |
| `werewolf/` | 主持人 workflow。每轮一次执行，用 continue-as-new 进入下一轮 |
| `internal/engine/` | 本地 workflow 引擎（durabletask + SQLite），不装 Dapr 也能玩 |
| `cmd/werewolf/` | 命令行：`engine`、`gm`、`start` |

信息隔离由主持人负责：每个玩家每次只收到 `State.VisibleTo(自己)` 的那部分记录。上帝视角的直播会把所有人的私下想法也打出来（标成"内心"），可以看到每个人嘴上说的和心里想的有什么不同。

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

本地引擎只能用 `random` 大脑，因为 LLM 大脑要调用 Dapr 的 Conversation API。

## 在 Catalyst 上玩

`gm` 不加 `-engine` 时连 Dapr。`diagrid dev run` 会把 Catalyst 的 `DAPR_GRPC_ENDPOINT` 和 `DAPR_API_TOKEN` 注入给它，这时 workflow 状态和执行历史都存在 Catalyst 里。

```bash
diagrid project update <项目> --enable-managed-workflow      # Workflow API 需要它
go build -o bin/werewolf ./cmd/werewolf
diagrid dev run --project <项目> --id werewolf --approve -- ./bin/werewolf gm   # 主持人，输出就是直播

# 另一个终端开局；phaseDelay 的单位是纳秒，可以不填
diagrid workflow start GameWorkflow --project <项目> --id werewolf --instance-id game-1 --data '{"phaseDelay":2000000000}'
diagrid workflow get game-1 --project <项目> --id werewolf  # 结果和执行历史
```

演示杀进程：Ctrl+C 停掉 `dev run`，再运行一次同样的命令。

### 让 LLM 来玩

在项目里建一个 conversation component，名字叫 `llm`。或者另取一个名字，再通过 `-llm-component` 传给 `gm`。然后给 `gm` 加上 `-brain llm`：

```bash
diagrid component create llm --project <项目> --type conversation.openai --metadata key=<你的 key> --metadata model=gpt-4o-mini
diagrid dev run --project <项目> --id werewolf --approve -- ./bin/werewolf gm -brain llm
```

一局大约 100 到 150 次 LLM 调用，每一轮大约 35 次。

## 测试

```bash
go test ./...
```

`werewolf/workflow_test.go` 在测试进程里起一个内存引擎，用随机大脑完整跑 10 局，覆盖女巫和猎人的各种情况。另外有两个用例：
- 按剧本让女巫救人，检查这一晚是平安夜。
- 在中途杀掉主持人 worker，换一个新的接手，检查游戏照样能打完。

## 下一步

- 观战网页，加一个"拔电源"按钮
- 真人玩家：投票改成 `WaitForExternalEvent`，超时视为弃票
