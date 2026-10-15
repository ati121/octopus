export function formatDurationCompact(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    const seconds = ms / 1000;
    if (seconds < 10) return `${seconds.toFixed(2)}s`;
    if (seconds < 100) return `${seconds.toFixed(1)}s`;
    return `${Math.round(seconds)}s`;
}

export function formatDurationPair(firstTokenMs: number | null, useTimeMs: number | null): string {
    const firstToken = firstTokenMs != null ? formatDurationCompact(firstTokenMs) : '--';
    const total = useTimeMs != null ? formatDurationCompact(useTimeMs) : '--';
    return `${firstToken} / ${total}`;
}

// 日志页按 Magpie 口径把首响固定显示为「首字」；缺少首响的旧日志回退到 ftut。
// 首响到达后总耗时立即跟随实时计时，避免响应处理中长时间显示未开始。
export function durationPairText(firstResponseMs: number, ftutMs: number, liveMs: number | null, useTimeMs: number): string {
    const firstTokenMs = firstResponseMs > 0 ? firstResponseMs : ftutMs > 0 ? ftutMs : null;
    if (liveMs == null) {
        return formatDurationPair(firstTokenMs, useTimeMs);
    }
    if (firstTokenMs == null) {
        return formatDurationPair(liveMs, null);
    }
    return formatDurationPair(firstTokenMs, Math.max(firstTokenMs, liveMs));
}
