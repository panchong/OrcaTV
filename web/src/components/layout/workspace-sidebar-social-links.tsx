import { Tooltip } from "antd";
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
