# 参考与第三方来源

2026-11-01增加可选本机[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)运行时（官方仓库MIT）。程序只检测并调用用户已有的Harness，不捆绑其二进制/源码或提取用户凭证；兼容性根据已安装SDK协议核对。未新增模型SDK依赖。

当前平台适配为Go精简实现，由项目原有TypeScript业务迁移而来；未复制或捆绑完整DSH或B站SDK。2026-10-30核查并沿用以下来源：

- [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk)：官方Go MCP SDK，锁定v1.7.0；支持stdio及2025-11-25等协议。该版本LICENSE说明新代码Apache-2.0、未获重许可同意的旧贡献仍MIT；实际许可证和NOTICE随依赖分发保留，不以本项目许可替代。
- [skip2/go-qrcode](https://github.com/skip2/go-qrcode)：通用二维码PNG生成，锁定v0.0.0-20200617195104-da1b6568686e，MIT。仅用于二维码编码，不涉及平台业务。
- [Alterssss/dsh-bili-whale](https://github.com/Alterssss/dsh-bili-whale)：扫码、草稿、评论root/parent、回执及保护流程参考，仓库声明MIT；未复制其宿主、命令或UI插件。
- [Scorp1o117/dsh-soul-md](https://github.com/Scorp1o117/dsh-soul-md)：分层记忆及按需读取设计参考，仓库声明MIT；未使用宿主注入实现。
- [bilibili-api-collect](https://github.com/bilibili-plugins/bilibili-api-collect)：平台接口参数资料，不代表平台授权。

运行依赖和传递依赖的版本／校验和以go.mod、go.sum为准，许可证原文见对应模块LICENSE／NOTICE；正式发布二进制前须随包提供适用的第三方声明。本项目采用 MIT 许可证；依赖仍适用各自许可证。

历史 TypeScript 源码归档不包含在此次发布中。B站使用非官方Web接口，可能变更或限制；不绕过验证码、访问控制或风控。

本地Codex用量采集（2026-10-31）按用户要求复用其GPT-Niang/gpt-niang-usage/runtime/thread-speed.mjs中的只读线程索引、token_count累计差值、增量游标与重置检测思路，在本项目用Go实现；未捆绑该项目运行时或增加Node运行依赖。采集后另行校验机器人MCP操作ID及纯业务轮次，不导入小人界面的全部聊天统计或正文。

2026-10-31私信推送：新增github.com/gorilla/websocket v1.5.3（BSD-2-Clause）及google.golang.org/protobuf v1.36.11（BSD-3-Clause），版本/校验和见go.mod/go.sum，分发遵循其LICENSE。协议资料参考[laplace-live/comet](https://github.com/laplace-live/comet)公布的网页广播路径、字段号及认证/订阅流程；该仓库AGPL-3.0，本项目未复制、移植或捆绑其客户端代码，以标准Protobuf库独立实现Go协议适配。B站广播仍是非官方Web接口，协议可变，不代表平台公开SDK承诺。

2026-11-02记忆存储采用 modernc.org/sqlite v1.38.2（BSD-3-Clause，已核对模块 LICENSE），将 SQLite 编译入 Go 程序，无需用户安装数据库服务。传递依赖及校验值见 go.mod/go.sum；分发时保留对应 LICENSE/NOTICE。


## 公共表情资源（2026-11-03）

`assets/images/stickers/` 为独立许可的公开表情资料，不属于本项目 MIT 代码授权。新增36张原始GIF来自 [kiriyuki.com](https://kiriyuki.com)，作者 [可以叫我云大人/雾雪](https://space.bilibili.com/103739)，仅按上游保留署名与网址的非商业分享条件提供。相关语义数据由 [Loping151/dsh-nekowhale-stickers](https://github.com/Loping151/dsh-nekowhale-stickers) 的 CC BY-NC 4.0 数据整理；未引入其运行时代码。角色设计来源与旧收藏逐张出处见[图库说明](assets/images/stickers/README.md)及 sources.json，上游声明原文随图库保留。

### 蓝色大肥鱼图库：新增龙娘原图

10张原图来自 https://github.com/lmy414/ai-girl-stickers ，具体详情页、投稿署名、来源和上游许可备注保存在 `assets/images/stickers/providers/codex/sources.json`。图片权利归原作者；来源站代码及文字的MIT许可不覆盖图片。本项目保留原字节，不主张原创或统一再许可。

### CrossPet 龙娘表情

20张现成表情来自 https://github.com/lokicorvus/crosspet （固定提交6e98970f6ff9b44e8ac85215241cabcd78f67d92）。署名CrossPet / lokicorvus，GPT龙娘角色设计ZipZipPipe。CC BY-NC-SA 4.0，非商业、署名、相同方式共享；完整原文见 `assets/images/stickers/CROSSPET-LICENSE.md`。本项目仅将WebP转为PNG，保留原文件、哈希和逐张来源；没有生成新图。图片不适用项目MIT许可。
