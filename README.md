# catalyst-werewolf

一局 9 人 AI 狼人杀，主持人是一个 Dapr Workflow。

演示的看点：游戏进行到一半把主持人进程杀掉，再启动一个，游戏从断点继续。已经说过的话不会重新生成，LLM 不会被重复调用。

## 规则

标准 9 人局：3 狼人、预言家、女巫、猎人、3 村民。

- 夜里：狼人先密谈，再由一只狼定下"狼队计划"：今晚刀谁、谁悍跳、白天把票引向谁。计划写进每只狼的秘密信息，白天照着执行；投票时狼人不能投同伴。女巫知道今晚谁被刀，决定救不救、毒不毒（两瓶药各用一次，同一晚最多用一瓶，不能自救）；预言家查验一个人。
- 天亮：只公布谁死了，不公布死因。猎人出局时可以开枪带走一人，被毒死的除外。
- 白天：按轮换的顺序每人发言一次，然后是回应环节，每人再说一次，可以回应怀疑或者质疑别人。每次发言都带着公开的表态，记成"……【声明预言家，第1夜查验小红是好人，怀疑阿杰】"，后面的人可以直接拿它来质询。投票前主持人汇总每个人的最新表态，然后投票，平票无人出局。被放逐的人有遗言。
- 胜负按屠边规则：狼人全部出局则好人胜利；村民全部出局，或者神职（预言家、女巫、猎人）全部出局，则狼人胜利。

## 每个玩家知道什么

主持人的 workflow 持有完整的 `game.State`，所有真相都在里面。每次调用 LLM 前，它从这份状态现场生成这个玩家的视图（`brain.Turn`）。视图只包含这个玩家有权知道的东西，过滤在代码里完成，不靠提示词让模型"自觉不看"。别人的身份从来不会传进去：座位信息的类型 `brain.Seat` 本身就没有 role 字段。

视图分成六块：

- 【座位和状态】：每个人是否存活。死了的人只写公开的死法，比如"第2夜死亡""被投票放逐"。
- 【你的秘密信息】：自己的身份、狼同伴、夜聊和狼队计划、查验结果、女巫知道的刀口和药。这是唯一确定的事实。
- 【你的笔记】：玩家自己维护的结构化记忆，包括对每个人的判断（`suspicions`）和接下来的打算（`strategy`）。每次行动都会交回一份完整的新笔记。
- 【你最近的私下想法】：最近 3 次行动的 `thinking`。更早的判断应该已经写进笔记里。
- 【场上的身份声明】：从公开发言里摘出来的声明，每条都标着"未验证"。美玲声称查验小红是好人，进入别人上下文的只是"美玲声称：预言家，小红：好人（未验证）"，小红不会被当成好人。狼人的欺骗空间就留在这里。
- 【公开记录】：所有人都看到的内容，也是唯一允许引用的历史。

发言时还会告诉它：这一轮谁已经说过、谁还没说。最后附一份回答前的自检清单。

每次行动都返回 JSON：`thinking` 是私下想法，`speech` 是说出口的话，`target` 是选中的人，`notes` 是新的笔记。白天发言还要给出 `suspect`（现在最怀疑谁，或者"无"）和 `claim`，格式是 `{"role", "target", "result"}`，不声明身份就写 null，狼人可以说假的。

主持人会校验回答。不合格时把所有问题一起告诉它让它重答，最多重答 2 次。校验这些：

- `target` 和 `suspect` 必须在名单里，比如不能投给自己。
- `claim` 只能声称预言家、女巫、猎人或村民，提到的人必须在桌上，预言家的结果只能是好人或狼人。
- 笔记里不能出现桌上没有的名字。
- 任何发言（包括夜聊和狼队计划）都不能引用从没公开说过话的人，比如"说过""提到""的发言""的观点"。白天发言时，也不能说今天还没轮到的人"刚才"如何。提示词里也会直接列出还没公开发过言的人；第 1 夜会明说"还没有任何人公开发过言"。

重答以后还不合格，选择算弃权，无效的声明和笔记条目会被丢掉。发言内容的问题只记一条日志，发言照常算数。

## 结构

| 目录 | 内容 |
|---|---|
| `game/` | 规则：发身份、谁能看到什么、计票、胜负。纯函数，不依赖 Dapr |
| `brain/` | 玩家怎么想：`random`（随机，免费）和 `llm`（Dapr Conversation API） |
| `werewolf/` | 主持人 workflow。每轮一次执行，用 continue-as-new 进入下一轮 |
| `internal/engine/` | 本地 workflow 引擎（durabletask + SQLite），只在本地玩和跑测试时用；在 Catalyst 上玩不需要它 |
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

在 Catalyst 上玩不需要本地引擎，也不用运行 `werewolf engine`：Catalyst 托管的 workflow 就是引擎，只需要跑 `gm` 这一个进程。

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

`gpt-4o-mini` 便宜，但 9 个人、几十条记录的上下文里推理明显吃力，发言容易空泛。想要更像样的对局，换个更强的模型。`component update` 会整体替换 metadata，所以 key 要一起带上：

```bash
diagrid component update llm --project <项目> --metadata key=<你的 key> --metadata model=gpt-4.1
```

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
