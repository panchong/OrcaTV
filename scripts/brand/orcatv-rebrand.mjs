#!/usr/bin/env node

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const args = new Set(process.argv.slice(2));
const checkOnly = args.has("--check");

const targetFiles = [
    "agent-host/system-prompt.mjs",
    "backend/cmd/beeftv/business.go",
    "backend/cmd/beeftv/main.go",
    "backend/internal/app/storage_quota.go",
    "backend/internal/app/task_timeline.go",
    "backend/internal/appearance/service.go",
    "backend/internal/appearance/types.go",
    "backend/internal/beefapi/client.go",
    "backend/internal/beefapi/http_test.go",
    "backend/internal/beefapi/service.go",
    "backend/internal/desktopstorage/cleanup.go",
    "backend/internal/desktopstorage/files.go",
    "backend/internal/desktopstorage/lock.go",
    "backend/internal/diagnostics/ports.go",
    "backend/internal/generation/beefapi_h3.go",
    "backend/internal/generation/provider_error.go",
    "backend/internal/handler/agent_clients.go",
    "backend/internal/handler/openapi.yaml",
    "backend/internal/app/operations_model_catalog_test.go",
    "backend/internal/outbound/outbound.go",
    "backend/internal/prompts/agent-media-policy.md",
    "backend/internal/prompts/agent-system-policy.md",
    "backend/internal/prompts/agent_policy_test.go",
    "backend/internal/provider/bailian/discovery.go",
    "backend/internal/providerpreset/catalog/beefapi.v1.json",
    "backend/internal/providerpreset/catalog_test.go",
    "backend/internal/runtimeinfo/workspace_lock.go",
    "backend/internal/task/errors.go",
    "backend/internal/workspace/brand_upgrade_test.go",
    "backend/internal/workspace/provider_config_test.go",
    "web/index.html",
    "web/public/logo.svg",
    "web/public/welcome/credits.html",
    "web/scripts/beeftv-local-network-audit.mjs",
    "web/scripts/test-canvas-transcription.mjs",
    "web/src/components/canvas/canvas-transcription-controls.tsx",
    "web/src/components/canvas/nodes/media-conversion-node.tsx",
    "web/src/components/channel-headers-editor.tsx",
    "web/src/components/layout/app-changelog.zh.ts",
    "web/src/components/layout/model-setup-guide.tsx",
    "web/src/components/layout/workspace-sidebar-nav.tsx",
    "web/src/components/layout/workspace-sidebar-social-links.tsx",
    "web/src/components/onboarding/first-creation-guide.tsx",
    "web/src/lib/model-connection-test.ts",
    "web/src/lib/plugins/builtin/editor/editor-shell.tsx",
    "web/src/lib/plugins/builtin/editor/editor-subtitle-tools.tsx",
    "web/src/lib/plugins/builtin/media-conversion.ts",
    "web/src/lib/timeline/ai-command-schema.ts",
    "web/src/pages/agents/agent-client-presentation.ts",
    "web/src/pages/agents/agent-connect-panel.tsx",
    "web/src/pages/agents/index.tsx",
    "web/src/pages/canvas/canvas-assistant-copy.ts",
    "web/src/pages/canvas/canvas-assistant-sidebar.tsx",
    "web/src/pages/canvas/index.tsx",
    "web/src/pages/dev/assistant-panel-lab.tsx",
    "web/src/pages/home/home-dashboard.tsx",
    "web/src/pages/home/home-data.ts",
    "web/src/pages/projects/detail.tsx",
    "web/src/pages/projects/detail/assets.tsx",
    "web/src/pages/settings/channel-settings-pane.tsx",
    "web/src/pages/settings/model-default-grid.tsx",
    "web/src/pages/settings/model-service-editor.tsx",
    "web/src/pages/settings/reference-storage-pane.tsx",
    "web/src/services/api/agent-clients.ts",
    "web/src/services/api/audio.ts",
    "web/src/services/api/beefapi-connection.ts",
    "web/src/services/api/timeline-tasks.ts",
    "web/src/services/api/video-validation.ts",
    "web/src/services/diagnostics/diagnostics-api.ts",
    "web/src/stores/use-appearance-store.ts",
    "web/src/styles/globals.css",
    "web/src/styles/workspace-product.css",
    "web/test/assistant-input.browser.test.ts",
    "web/test/assistant-model-picker.browser.test.ts",
    "web/test/beefapi-connection.test.ts",
    "web/test/beefapi-generation-readiness.test.ts",
    "web/test/canvas-assistant-presentation.test.ts",
    "web/test/fixtures/storage-feedback-harness.tsx",
    "web/test/storage-feedback.browser.test.ts",
];

const protectedText = new Map([
    ["__ORCA_KEEP_UPSTREAM_RELEASE_URL__", "https://github.com/glanderness/BeefTV/releases/"],
    ["__ORCA_KEEP_OPEN_BEEFTV__", "OpenBeefTV"],
    ["__ORCA_KEEP_BEEFTV_AND_EXE__", "BeefTV / BeefTV.exe"],
    ["__ORCA_KEEP_BEEFTV_APP__", "BeefTV.app"],
    ["__ORCA_KEEP_BEEFTV_EXE__", "BeefTV.exe"],
    ["__ORCA_KEEP_BEEFTV_WIN_PATH__", "\\\\BeefTV"],
    ["__ORCA_KEEP_BEEFTV_UNIX_PATH__", "/BeefTV"],
]);

const orcatvSidebar = `import { Tooltip } from "antd";
import { Star } from "lucide-react";
import Github from "@lobehub/icons/es/Github";
import { getDesktopAppBinding } from "@/services/desktop-runtime";

const ORCATV_GITHUB_URL = "https://github.com/panchong/OrcaTV";

export function WorkspaceSidebarSocialLinks() {
    const openGitHub = async (event: React.MouseEvent<HTMLAnchorElement>) => {
        const openInBrowser = getDesktopAppBinding()?.OpenBeefTVGitHub;
        if (!openInBrowser) return;
        event.preventDefault();
        await openInBrowser();
    };

    return (
        <div className="app-workspace-social-links">
            <Tooltip
                placement="top"
                trigger={["hover", "focus"]}
                mouseEnterDelay={0.15}
                mouseLeaveDelay={0.1}
                rootClassName="app-workspace-github-tooltip"
                title={<span className="app-workspace-github-hint">点点 star <Star aria-hidden="true" /></span>}
            >
                <a className="app-workspace-social-action" href={ORCATV_GITHUB_URL} target="_blank" rel="noopener noreferrer" aria-label="打开 OrcaTV GitHub 仓库" onClick={openGitHub}>
                    <Github aria-hidden="true" />
                </a>
            </Tooltip>
        </div>
    );
}
`;

function protect(text) {
    let next = text;
    for (const [marker, replacement] of protectedText) {
        next = next.replaceAll(replacement, marker);
    }
    return next;
}

function restore(text) {
    let next = text;
    for (const [marker, replacement] of protectedText) {
        next = next.replaceAll(marker, replacement);
    }
    return next;
}

function replaceGeneralBrand(text) {
    return restore(
        protect(text)
            .replaceAll("https://github.com/glanderness/BeefTV", "https://github.com/panchong/OrcaTV")
            .replaceAll("/beef-logo.png", "/orcatv-logo.png")
            .replaceAll("beef-logo.png", "orcatv-logo.png")
            .replaceAll("/beef-mark.png", "/orcatv-mark.png")
            .replaceAll("beef-mark.png", "orcatv-mark.png")
            .replaceAll("beeftv-diagnostics.zip", "orcatv-diagnostics.zip")
            .replaceAll("例如 beeftv-media", "例如 orcatv-media")
            .replaceAll('DefaultBrandSlug = "beeftv"', 'DefaultBrandSlug = "orcatv"')
            .replaceAll('brandSlug: "beeftv"', 'brandSlug: "orcatv"')
            .replaceAll("BEEF CREATIVE STUDIO", "ORCA CREATIVE STUDIO")
            .replaceAll("BeefTV", "OrcaTV"),
    );
}

function transform(file, text) {
    let next = replaceGeneralBrand(text);

    if (file === "backend/internal/appearance/types.go") {
        next = next.replace(/DefaultLogoURL\s*=\s*"\/logo\.svg"/, 'DefaultLogoURL   = "/orcatv-logo.png"');
    }

    if (file === "web/index.html") {
        next = next
            .replace("<title>正在加载</title>", "<title>OrcaTV</title>")
            .replace("aspect-ratio: 17 / 10;", "aspect-ratio: 1 / 1;");
    }

    if (file === "web/src/components/layout/app-changelog.zh.ts") {
        next = next
            .replaceAll("内置 BeefAPI", "内置渠道")
            .replaceAll("修复 BeefAPI 视频模型调用错误", "修复内置渠道视频模型调用错误")
            .replaceAll("在 beeftv.app 完成授权并管理账户", "在平台页面完成授权并管理账户")
            .replaceAll("修复 内置渠道", "修复内置渠道");
    }

    if (file === "web/src/components/layout/workspace-sidebar-social-links.tsx") {
        next = orcatvSidebar;
    }

    if (file === "web/src/pages/settings/channel-settings-pane.tsx") {
        next = next
            .replace('import beefTVAppIcon from "../../../../assets/app-icon.png";', 'import orcaTVAppIcon from "../../../../assets/orcatv-app-icon.svg";')
            .replaceAll("beefTVAppIcon", "orcaTVAppIcon");
    }

    return next;
}

let changed = 0;
let missing = 0;

for (const file of targetFiles) {
    const absolute = join(repoRoot, file);
    if (!existsSync(absolute)) {
        missing += 1;
        console.warn(`missing target: ${file}`);
        continue;
    }
    const before = readFileSync(absolute, "utf8");
    const after = transform(file, before);
    if (before === after) continue;
    changed += 1;
    if (checkOnly) {
        console.log(`needs rebrand: ${relative(repoRoot, absolute)}`);
    } else {
        writeFileSync(absolute, after);
        console.log(`rebranded: ${relative(repoRoot, absolute)}`);
    }
}

if (checkOnly && changed > 0) {
    console.error(`\n${changed} file(s) need the OrcaTV rebrand replay. Run: node scripts/brand/orcatv-rebrand.mjs`);
    process.exitCode = 1;
} else if (checkOnly) {
    console.log("OrcaTV rebrand replay is up to date.");
} else {
    console.log(`OrcaTV rebrand replay complete: ${changed} file(s) updated, ${missing} missing target(s).`);
}
