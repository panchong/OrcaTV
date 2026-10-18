# OrcaTV 上游同步与品牌重放

OrcaTV 基于 BeefTV 的 MIT 源码维护。身份桥接、按用户隔离、托管部署等 OrcaTV 业务能力保留为独立功能提交；产品可见品牌单独成层，避免每次同步上游都手工重新搜索和替换。

## 远端与分层

- `origin`: `https://github.com/panchong/OrcaTV.git`
- `upstream`: `https://github.com/glanderness/BeefTV.git`
- 不迁移现有 BeefTV 数据，不改变 BeefTV 的业务数据目录和协议标识。
- `brand(orcatv): add brand assets` 只新增 `assets/orcatv-app-icon.svg`、`web/public/orcatv-logo.png`、`web/public/orcatv-mark.png`。
- `brand(orcatv): rebrand product surface` 只放产品可见文案、logo 引用和 `scripts/brand/orcatv-rebrand.mjs`。
- `docs(orcatv): document upstream branding replay` 放本文件。

同步上游时只回退品牌文案提交，不回退 OrcaTV 的身份桥接和用户隔离提交。

## 必须保留

以下内容属于真实协议、数据目录、桌面标识或上游版权，不参与品牌替换：

- `beeftv.plugin/*` 插件协议 ID。
- `BEEFTV_*` 环境变量，例如 `BEEFTV_DATA_DIR`、`BEEFTV_CLIENT_ID`。
- 数据目录名 `BeefTV`，包括 `backend/internal/desktopstorage/location.go` 的默认根目录。
- 桌面可执行文件和安装器标识：`BeefTV.exe`、`BeefTV.app`、`desktopupdate`、`OpenBeefTV*` 接口。
- 真实端点：`beeftv.app`、`beefapi.com`、`enterprise.beefapi.com`、`whatstoken.ai`、`global.beefapi.com`。
- 内置渠道 id `beefapi`、凭据引用 `beefapi-enterprise`、`isBuiltinBeefAPIChannel` 等内部标识。
- 深度模型下载地址 `https://github.com/glanderness/BeefTV/releases/...`。
- `LICENSE`、`NOTICE`、`THIRD_PARTY_NOTICES.md` 和上游版权声明。

## 同步流程

1. 记录当前品牌提交：

   ```bash
   git log --oneline --grep '^brand(orcatv)'
   ```

2. 从 OrcaTV 的 `main` 或 `dev` 拉出同步分支：

   ```bash
   git checkout main
   git pull --ff-only
   git checkout -b codex/orcatv-upstream-sync
   ```

3. 拉取上游并先移除旧品牌文案层：

   ```bash
   git fetch upstream --tags
   git revert <rebrand-product-surface-commit>
   git merge upstream/main --no-ff
   ```

   如果品牌资源提交也被回退了，先从品牌资源提交恢复文件：

   ```bash
   git checkout <brand-assets-commit> -- \
     assets/orcatv-app-icon.svg \
     web/public/orcatv-logo.png \
     web/public/orcatv-mark.png
   ```

4. 冲突处理原则：产品文案、logo 引用和 changelog 先采用上游版本，再运行品牌重放；OrcaTV 身份桥接、用户隔离、托管部署和平台渠道能力保留 OrcaTV 版本。

5. 重放品牌层：

   ```bash
   node scripts/brand/orcatv-rebrand.mjs
   node scripts/brand/orcatv-rebrand.mjs --check
   ```

   如果脚本报告 `missing target`，说明上游移动或删除了文件。先核对实际文件，再更新脚本的目标列表和特殊替换，不要改用全仓库无差别替换。

6. 跑验证：

   ```bash
   cd backend && go test ./...
   cd ../web && bun run typecheck
   bun run test
   bun run build
   ```

7. 审计内部标识没有被误改：

   ```bash
   rg -n 'BEEFTV_|beeftv\\.plugin|beefapi-enterprise|clientChannelID|beeftv\\.app|beefapi\\.com|enterprise\\.beefapi\\.com|whatstoken\\.ai|BeefTV\\.app|BeefTV\\.exe|OpenBeefTV|BeefTV$' backend web agent-host
   ```

8. 独立提交同步结果，例如：

   ```bash
   git add <changed-files>
   git commit -m "chore(orcatv): sync upstream and replay branding"
   ```

## 验收清单

- `web/index.html` 标题和 favicon 为 OrcaTV。
- 设置页内置渠道显示 OrcaTV，渠道 id 和请求契约仍是 `beefapi`。
- 系统提示词、User-Agent、应用内 changelog、诊断文件名显示 OrcaTV。
- 普通成员看到的模型渠道不含 API Key，平台系统渠道仍按管理员配置工作。
- `BEEFTV_*`、`beeftv.plugin`、真实端点、桌面标识和 `BeefTV` 数据目录未变。
- OrcaDock handoff、退出失效、按用户隔离和本地 Docker 验收仍通过。
