# 公共表情图库

所有机器人共享，私聊仅按实际供应商选择单人图。双人图留给未来 Codex 与 DeepSeek 的群聊互动，当前不启用。

本次新增 36 张原始动态 GIF：18 张鲸鱼娘单人图、18 张龙娘与鲸鱼娘双人图。含原有素材共 46 张：私聊可用 24 张、群聊预留 18 张、原有超限收藏 4 张。相同图片的 `.image` 与 `gallery/` 文件是原字节副本，不按两张计数。

- `providers/codex/`：龙娘单人，2 张可发送，另有 2 张超限收藏。
- `providers/deepseek/`：鲸鱼娘单人，22 张可发送，另有 2 张超限收藏。
- `group/dragon-whale/`：18 张双人互动，`enabled=false`，不进入私聊候选。
- `catalog.json` 保存可用条目和语义标注，`sources.json` 保存来源、原图字节数及许可；点击下面图片即可查看或保存原图。

## 来源与使用条件

新增36张 GIF 作者为 [可以叫我云大人/雾雪](https://space.bilibili.com/103739)，来自 [kiriyuki.com](https://kiriyuki.com)。角色设计来源为 [ZipZipPipe](https://space.bilibili.com/4168597)（GPT娘及鲸鱼娘二创）与 [上善无形](https://space.bilibili.com/4456176)（鲸鱼娘原型）。

上游允许保留作者链接和网站的非商业分享；图片不适用本项目 MIT 代码许可，不可收费提供或用于其他商业用途。语义描述基于 [Loping151/dsh-nekowhale-stickers](https://github.com/Loping151/dsh-nekowhale-stickers) 的 CC BY-NC 4.0 知识库整理，新增本项目中文标签映射、禁用场景和群聊范围。完整上游声明见 [KIRIYUKI-ASSETS.txt](KIRIYUKI-ASSETS.txt)，[CC BY-NC 4.0](https://creativecommons.org/licenses/by-nc/4.0/)。

原有10张网络收藏逐张保留原始来源，权利归各自作者，未核实统一再许可；收录不表示改为 MIT 或获得商业授权。

## Codex · 龙娘单人

| 表情 | 场景与状态 |
|---|---|
| <img src="providers/codex/gallery/01.png" alt="GPT龙娘：我有一个想法" width="130"> | **GPT龙娘：我有一个想法**<br>暂未标注<br>超2MiB，仅收藏 |
| <img src="providers/codex/gallery/02.png" alt="GPT龙娘核爆瘫坐" width="130"> | **GPT龙娘核爆瘫坐**<br>暂未标注<br>超2MiB，仅收藏 |
| <img src="providers/codex/gallery/03.png" alt="你这贪财的白色大肥龙：我不是大肥龙……" width="130"> | **你这贪财的白色大肥龙：我不是大肥龙……**<br>委屈、撒娇<br>私聊可用 |
| <img src="providers/codex/gallery/04.png" alt="100% 正确，未经验证" width="130"> | **100% 正确，未经验证**<br>搞怪<br>私聊可用 |

## DeepSeek · 鲸鱼娘单人

| 表情 | 场景与状态 |
|---|---|
| <img src="providers/deepseek/gallery/01.gif" alt="DeepSeek娘表情包：蓝发蓝瞳的Q版" width="130"> | **DeepSeek娘表情包：蓝发蓝瞳的Q版**<br>暂未标注<br>超2MiB，仅收藏 |
| <img src="providers/deepseek/gallery/02.gif" alt="DeepSeek娘表情包：蓝发蓝眼的Q版" width="130"> | **DeepSeek娘表情包：蓝发蓝眼的Q版**<br>暂未标注<br>超2MiB，仅收藏 |
| <img src="providers/deepseek/gallery/03.jpg" alt="DeepSeek娘表情包：token给妈" width="130"> | **DeepSeek娘表情包：token给妈**<br>搞怪、撒娇<br>私聊可用 |
| <img src="providers/deepseek/gallery/04.jpg" alt="鲸鱼娘疑惑：这用户发的啥" width="130"> | **鲸鱼娘疑惑：这用户发的啥**<br>疑惑、搞怪<br>私聊可用 |
| <img src="providers/deepseek/gallery/05.jpg" alt="鲸鱼娘夸夸：和你聊得来" width="130"> | **鲸鱼娘夸夸：和你聊得来**<br>夸夸、开心、害羞<br>私聊可用 |
| <img src="providers/deepseek/gallery/06.jpg" alt="鲸鱼娘干饭：我去吃饭啦" width="130"> | **鲸鱼娘干饭：我去吃饭啦**<br>吃饭、告别<br>私聊可用 |
| <img src="providers/deepseek/gallery/10N01.gif" alt="鲸鱼娘：猫猫搓手" width="130"> | **鲸鱼娘：猫猫搓手**<br>开心、兴奋、期待、搓手、跃跃欲试、等好消息<br>私聊可用 |
| <img src="providers/deepseek/gallery/11N06.gif" alt="鲸鱼娘：小问号" width="130"> | **鲸鱼娘：小问号**<br>疑惑、满头问号、没听懂、什么意思、困惑、求解释<br>私聊可用 |
| <img src="providers/deepseek/gallery/12N11.gif" alt="鲸鱼娘：害羞" width="130"> | **鲸鱼娘：害羞**<br>害羞、被夸了、心动、不好意思、甜蜜<br>私聊可用 |
| <img src="providers/deepseek/gallery/16N01.gif" alt="鲸鱼娘：超开心" width="130"> | **鲸鱼娘：超开心**<br>开心、超开心、笑开花、好消息、心情大好、幸福<br>私聊可用 |
| <img src="providers/deepseek/gallery/16N07.gif" alt="鲸鱼娘：委屈哭哭" width="130"> | **鲸鱼娘：委屈哭哭**<br>委屈、哭、求安慰、被欺负、想哭、可怜巴巴<br>私聊可用 |
| <img src="providers/deepseek/gallery/EX46.gif" alt="鲸鱼娘：猫猫祟祟" width="130"> | **鲸鱼娘：猫猫祟祟**<br>搞怪、猫猫祟祟、调皮、吐舌头、偷偷卖萌、小动作<br>私聊可用 |
| <img src="providers/deepseek/gallery/N08.gif" alt="鲸鱼娘：猫咪探头" width="130"> | **鲸鱼娘：猫咪探头**<br>开心、打招呼、探头、冒泡、在吗、偷看<br>私聊可用 |
| <img src="providers/deepseek/gallery/10N03.gif" alt="鲸鱼娘：苏醒" width="130"> | **鲸鱼娘：苏醒**<br>告别、疲惫、刚睡醒、起床、脑子没开机、迷糊<br>私聊可用 |
| <img src="providers/deepseek/gallery/10N05.gif" alt="鲸鱼娘：怎么个事" width="130"> | **鲸鱼娘：怎么个事**<br>疑惑、怎么回事、围观、吃瓜、探头、好奇<br>私聊可用 |
| <img src="providers/deepseek/gallery/10N07.gif" alt="鲸鱼娘：杂技猫" width="130"> | **鲸鱼娘：杂技猫**<br>开心、整活、表演、看我厉害、逗你开心、忙得团团转<br>私聊可用 |
| <img src="providers/deepseek/gallery/14N04.gif" alt="鲸鱼娘：好人喵" width="130"> | **鲸鱼娘：好人喵**<br>感谢、你真好、好人、感激、被帮助、星星眼<br>私聊可用 |
| <img src="providers/deepseek/gallery/14N08.gif" alt="鲸鱼娘：理理我嘛" width="130"> | **鲸鱼娘：理理我嘛**<br>撒娇、卖萌、理理我、求关注、陪我聊、戳一戳<br>私聊可用 |
| <img src="providers/deepseek/gallery/16N08.gif" alt="鲸鱼娘：给你心心" width="130"> | **鲸鱼娘：给你心心**<br>撒娇、喜欢、表白、送心、喜欢你、贴贴<br>私聊可用 |
| <img src="providers/deepseek/gallery/4N05.gif" alt="鲸鱼娘：蹭蹭" width="130"> | **鲸鱼娘：蹭蹭**<br>撒娇、喜欢、蹭蹭、贴贴、亲近、抱抱<br>私聊可用 |
| <img src="providers/deepseek/gallery/6N05.gif" alt="鲸鱼娘：求求" width="130"> | **鲸鱼娘：求求**<br>撒娇、卖萌、求求你、拜托、可怜巴巴、想要<br>私聊可用 |
| <img src="providers/deepseek/gallery/6N12.gif" alt="鲸鱼娘：软糯" width="130"> | **鲸鱼娘：软糯**<br>撒娇、卖萌、乖巧、软萌、害羞、等你<br>私聊可用 |
| <img src="providers/deepseek/gallery/EX41.gif" alt="鲸鱼娘：炸虾猫" width="130"> | **鲸鱼娘：炸虾猫**<br>撒娇、卖萌、炸虾、可爱、摇摆、美食<br>私聊可用 |
| <img src="providers/deepseek/gallery/N11.gif" alt="鲸鱼娘：诶嘿" width="130"> | **鲸鱼娘：诶嘿**<br>撒娇、卖萌、诶嘿、装可爱、蒙混过关、小得意<br>私聊可用 |

## 群聊预留 · 双人互动

| 表情 | 场景与状态 |
|---|---|
| <img src="group/dragon-whale/gallery/2TT02.gif" alt="双人互动：情侣杯" width="130"> | **双人互动：情侣杯**<br>撒娇、喜欢、情侣、秀恩爱、一起喝、甜蜜<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/2TT04.gif" alt="双人互动：灵魂吸取" width="130"> | **双人互动：灵魂吸取**<br>搞怪、头部按摩、舒服到失神、宠爱、放松、解压<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/2TT05.gif" alt="双人互动：发布会" width="130"> | **双人互动：发布会**<br>开心、官宣、发布会、重大消息、接受采访、一起亮相<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/2TT06.gif" alt="双人互动：合影" width="130"> | **双人互动：合影**<br>开心、合影、自拍、留念、好朋友、秀恩爱<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/2TT08.gif" alt="双人互动：午休" width="130"> | **双人互动：午休**<br>告别、疲惫、午休、一起睡、休息、陪伴<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/2TT09.gif" alt="双人互动：看好书" width="130"> | **双人互动：看好书**<br>开心、一起看书、分享好书、学习、围观、推荐<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/2TT10.gif" alt="双人互动：替身使者" width="130"> | **双人互动：替身使者**<br>搞怪、得意、召唤替身、有人撑腰、出战、中二<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/3TT02.gif" alt="双人互动：打包" width="130"> | **双人互动：打包**<br>搞怪、打包带走、拐走、恶作剧、抱走、喜欢你<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/3TT05.gif" alt="双人互动：你醒啦" width="130"> | **双人互动：你醒啦**<br>开心、打招呼、你醒啦、起床、探望、醒来<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/3TT08.gif" alt="双人互动：打伞" width="130"> | **双人互动：打伞**<br>撒娇、喜欢、撑伞、陪伴、照顾、心动<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/3TT09.gif" alt="双人互动：鬼脸" width="130"> | **双人互动：鬼脸**<br>搞怪、做鬼脸、恶作剧、逗你、捏脸、打闹<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/3TT10.gif" alt="双人互动：拍按钮" width="130"> | **双人互动：拍按钮**<br>开心、兴奋、抢答、疯狂点击、游戏、一起玩<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/3TT11.gif" alt="双人互动：洗头" width="130"> | **双人互动：洗头**<br>撒娇、喜欢、洗头、照顾、宠爱、舒服<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/3TT12.gif" alt="双人互动：躲猫猫" width="130"> | **双人互动：躲猫猫**<br>搞怪、躲猫猫、找到你了、偷看、惊喜、逗朋友<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/TT02.gif" alt="双人互动：比心" width="130"> | **双人互动：比心**<br>撒娇、喜欢、比心、爱你、表白、甜蜜<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/TT06.gif" alt="双人互动：仙女棒" width="130"> | **双人互动：仙女棒**<br>开心、烟花、庆祝、节日、浪漫、一起玩<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/TT07.gif" alt="双人互动：干杯" width="130"> | **双人互动：干杯**<br>开心、干杯、庆祝、合作愉快、聚会、敬你一杯<br>群聊预留，未启用 |
| <img src="group/dragon-whale/gallery/TT09.gif" alt="双人互动：摸下巴" width="130"> | **双人互动：摸下巴**<br>撒娇、喜欢、摸下巴、宠爱、哄人、亲昵<br>群聊预留，未启用 |
