/**
 * OrcaTV 会话失效处理。
 *
 * OrcaTV 复用 OrcaDock 身份：一旦 OrcaDock 侧退出、改密、删号或撤销 token，
 * OrcaTV 后端会在下一次业务请求返回 401，并带上 `X-OrcaTV-Login` 头。只有
 * 携带该响应头时才视为「需要回到 OrcaDock 重新登录」，桌面形态的 401 不受影响。
 */

const LOGIN_HEADER = "x-orcatv-login";
const FALLBACK_LOGIN_URL = "http://127.0.0.1:3010/";
const REDIRECT_DELAY_MS = 1_200;

let redirecting = false;

function headerValue(headers: unknown): string {
    if (!headers) return "";
    const bag = headers as Record<string, unknown> & { get?: (name: string) => unknown };
    if (typeof bag.get === "function") {
        const viaGetter = bag.get(LOGIN_HEADER);
        if (typeof viaGetter === "string") return viaGetter.trim();
    }
    for (const [key, value] of Object.entries(bag)) {
        if (key.toLowerCase() === LOGIN_HEADER && typeof value === "string") return value.trim();
    }
    return "";
}

function resolveLoginURL(headers: unknown): string {
    const fromResponse = headerValue(headers);
    if (fromResponse) return fromResponse;
    const configured = (import.meta.env.VITE_ORCATV_LOGIN_URL || "").trim();
    return configured || FALLBACK_LOGIN_URL;
}

function showSessionExpiredNotice() {
    if (typeof document === "undefined") return;
    const overlay = document.createElement("div");
    overlay.setAttribute("role", "alert");
    overlay.style.cssText = [
        "position:fixed", "inset:0", "z-index:2147483647",
        "display:flex", "align-items:center", "justify-content:center",
        "background:rgba(15,15,17,0.92)", "color:#f4f4f5",
        "font-family:system-ui,-apple-system,'Segoe UI',sans-serif",
        "font-size:15px", "letter-spacing:0", "text-align:center", "padding:24px",
    ].join(";");
    overlay.textContent = "登录状态已失效，正在返回 OrcaDock 重新登录…";
    document.body.appendChild(overlay);
}

/**
 * 返回 true 表示这次 401 属于 OrcaTV 会话失效，调用方不应再向用户展示普通错误。
 * 同一页面只跳转一次，避免并发请求重复触发。
 */
export function handleOrcaTVUnauthorized(headers: unknown): boolean {
    // 只有后端身份层显式标记的响应才触发跳转，桌面形态的 401 不会被误判。
    if (!headerValue(headers)) return false;
    if (redirecting) return true;
    redirecting = true;
    const loginURL = resolveLoginURL(headers);
    showSessionExpiredNotice();
    if (typeof window !== "undefined") {
        window.setTimeout(() => {
            window.location.assign(loginURL);
        }, REDIRECT_DELAY_MS);
    }
    return true;
}
