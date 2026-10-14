# bookmarks-to-nav

把浏览器导出的书签转成一份 **全部条目都是私密** 的 [ghome](https://github.com/vrocwang/ghome)
`nav.yaml`。

书签是个人的东西——内网地址、带 token 的入口、随手存的临时链接——所以生成的文档里每个分组
都带 `private: true`，页面只把它交给"能读配置仓"的人。

这是一个独立工具：**不依赖 ghome，也不 import 它**，也不复制它的 schema。它只保证自己写出的
文档是 ghome 要的形状——能解析、每个分组都有链接、每条链接都有 `name` 和 `url`。

输入是浏览器"导出书签"写出的那个 HTML 文件（Netscape 格式），Chrome / Edge / Firefox /
Safari 都一样。

## 安装

```bash
go build -o bookmarks-to-nav .
# 或者
make build          # 输出 ./bookmarks-to-nav
make cross          # linux/amd64、linux/arm64、darwin/arm64、darwin/amd64 → dist/
```

## 用法

```bash
./bookmarks-to-nav ~/Downloads/bookmarks.html > nav.yaml
./bookmarks-to-nav --out nav.yaml --leaf --search ~/Downloads/bookmarks.html
cat bookmarks.html | ./bookmarks-to-nav -           # 读标准输入
```

| flag | 默认 | 说明 |
| --- | --- | --- |
| `--out` | 标准输出 | 写到文件（权限 0600） |
| `--title` | `导航` | `site.title` |
| `--subtitle` | `我的书签` | `site.subtitle` |
| `--leaf` | 关 | 分组只取**最内层**文件夹名，不写完整路径 |
| `--single` | 空 | 全部书签塞进一个分组（忽略层级），参数是分组名 |
| `--max-name` | `80` | 标题超过这么多**字符**就截断（`0` 不截断） |
| `--link-private` | 关 | 把 `private` 写在**每条链接**上，而不是每个分组上 |
| `--search` | 关 | 显式写一个 `search: enabled: true` 块（509 条书签建议开） |
| `--quiet` | 关 | 不打印汇总 |
| `--version` | | 打印版本 |

位置参数必须在最后（`flag` 包遇到第一个非 flag 参数就停止解析）。

## 从浏览器导出

浏览器里找"导出书签"（Firefox 在书签管理 → 导入和备份 → 导出书签到 HTML…；Chrome / Edge 是
`chrome://bookmarks` → ⋮ → 导出书签；Safari 是文件 → 导出 → 书签），拿到一个 `.html` 文件，
直接喂给工具。

## 它做了哪些判断

| 情况 | 处理 | 为什么 |
| --- | --- | --- |
| 内嵌的 base64 图标（Chrome 导出里每个链接一个，单个几 KB） | **丢掉** | ghome 自己解析并缓存站点图标；写进 YAML 只会让配置膨胀几百 KB |
| `javascript:` / `data:` / `vbscript:` / `blob:` 链接 | **报告并跳过** | ghome 会**整份拒绝**带这些协议的配置，一个坏链接就让整页停在上一版 |
| `chrome://` / `file://` / `ssh://` / `mailto:` 等 | 原样保留 | ghome 的黑名单只有上面那四个；本工具只镜像规则，不替你判断哪些链接有用 |
| 只装文件夹、没有链接的目录 | 跳过 | 空卡片只是噪音 |
| 同一个 URL 出现多次 | 只留第一次 | 一条链接一个图标、一个探测目标 |
| 标题过长（YouTube 那种 200 字） | 按 `--max-name` 截断加省略号 | 导航条目显示不下；按**字符**截断，不会切坏中文 |
| **书签没有标题** | 用 URL 的**主机名**当名字 | ghome 要求每个链接有 `name`，空名字会让它**整份拒绝**；汇总里会报"n untitled bookmarks named after their host" |
| 文件夹名里的 `&`（`K8S&amp;&amp;Docker`） | 解实体后按需加引号 | 页面上该显示 `&&` 而不是 `&amp;` |
| 嵌套很深（实测 4 层） | 默认分组名写完整路径 `书签栏 / 学习 / K8S&&Docker / Train` | 扁平化之后 `Docker`、`Learn` 这些同名文件夹就分不出来了；要短名用 `--leaf` |
| 嵌套超过 60 层 | 更深的分层折进最深的第 60 层，链接保留，汇总里报 "folders nested deeper than 60 were flattened" | 真实导出不到 10 层；不设限的话，深嵌套会让分组名（整条路径）把输出放大到输入的几百倍，并在递归遍历时爆栈 |
| 标题里有控制字符 | 转义成 `\x07` 这种写法 | 直接丢掉会让名字变空，而 ghome 会为一个空名字拒收**整份文档** |
| HTML 实体 | 只解码一次（`&amp;` → `&`） | 解码两次会把用户真实保存的 `A&amp;B` 改写成 `A&B` |

**生成后会先按 ghome 的规则校验一遍**：解析不了、有结构性错误、或者没有任何私密条目，就直接
失败，不写出一个页面会拒收的文件——那种文件比没有更糟，页面会继续服务上一版，你还得去读
`/healthz` 的 `lastError` 才知道为什么。

## 生成之后

1. **必须放在私有仓，并且用 `--source=gitea` 或 `forgejo`。** 解锁判据是"这个令牌能读配置仓"，
   所以配置仓公开 = 谁都能读 = 谁都能解锁。文件模式下私密条目对**所有人**隐藏（安全的方向，
   但页面看起来是空的，日志里会有一条 WARNING 说明原因）。
2. 解锁：打开页面 → 右上角锁 → 粘贴能读配置仓的令牌（存 `sessionStorage`，关标签页即失效）。
3. 509 条书签 = 58 个分组，页面很长。想瘦身：
   - `--single 全部书签` 只留一个分组，靠搜索框找（配 `--search`）
   - `--max-name 40` 让长标题更短
   - 生成后手工删掉不想要的分组——它就是个 YAML 文件，改完 push 即生效

## 实测

一份 509 条书签、58 个文件夹（最深 4 层）、485 个内嵌图标的 Chrome 导出：

```console
$ time ./bookmarks-to-nav --search --out nav.yaml bookmarks_2026_10_14.html
wrote 509 links in 58 groups (from 509 bookmarks, 58 folders); 41 titles truncated
real  0m1.0s
```

产出 694 行 / 78 KB，喂给 `ghome --source=file` 是 `status: ok`、零 warning，匿名视角只剩
`private: {total: 509, locked: true}`。

## 结构

```text
main.go                  CLI（flag 解析、汇总输出、写文件）
internal/bookmarks/      Netscape 书签 HTML 的解析 + nav.yaml 渲染 + 输出不变式检查
```

`internal/bookmarks` 在返回文档前会自己检查三条不变式：

1. 文档能被 YAML 解析出 `groups`
2. 每个分组都有名字、且至少有一条链接（ghome 会为"整份文档一个链接都没有"拒收）
3. 每条链接的 `name` 与 `url` 都非空

这三条是**这个工具对自己输出的要求**，不是 ghome 规则的复述：哪些链接可服务（协议黑名单、
http(s) 要有 host）在采集阶段就决定了，空名字在这里之前已经用主机名兜底。所以没有第二份
ghome schema 需要同步，也就不存在"镜像漂移"。

代价说清：如果 ghome 将来新增了必需字段或改了加载规则，这个工具**不会知道**——它会照旧写出
文档，由 push 之后的 `/healthz` 报出来。这是有意的取舍：为一个"把书签转成导航配置"的工具
维护 ghome 的判据副本不值得。
