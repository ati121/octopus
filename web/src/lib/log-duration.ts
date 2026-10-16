export interface LogTiming {
    time: number;
    start_time_ms?: number;
    elapsed_ms?: number;
    processing: boolean;
    liveTiming?: { elapsedMs: number; observedAt: number };
}

// Anchor a server snapshot to the browser's monotonic clock as soon as it arrives.
export function observeLogTiming<T extends LogTiming>(log: T, observedAt = performance.now(), wallNow = Date.now()): T {
    if (!log.processing) return log;
    const startMs = log.start_time_ms && log.start_time_ms > 0 ? log.start_time_ms : log.time * 1000;
    const elapsedMs = log.elapsed_ms != null ? log.elapsed_ms : Math.max(0, wallNow - startMs);
    return { ...log, liveTiming: { elapsedMs, observedAt } };
}

export function liveLogElapsedMs(log: LogTiming, now: number): number | null {
    if (!log.processing) return null;
    if (log.liveTiming) {
        return Math.round(log.liveTiming.elapsedMs + Math.max(0, now - log.liveTiming.observedAt));
    }
    return 0;
}

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

// 日志页按 Magpie TTFT 口径把首内容固定显示为「首字」；缺少该字段的旧日志回退到 ftut。
// 首响尚未到达时先显示前段实时计时，首响到达后再开始显示后段总耗时。
export function durationPairText(firstResponseMs: number, ftutMs: number, liveMs: number | null, useTimeMs: number): string {
    const firstTokenMs = firstResponseMs > 0 ? firstResponseMs : ftutMs > 0 ? ftutMs : null;
    if (liveMs != null) {
        if (firstTokenMs == null) {
            return formatDurationPair(liveMs, null);
        }
        return formatDurationPair(firstTokenMs, Math.max(firstTokenMs, liveMs));
    }
    return formatDurationPair(firstTokenMs, useTimeMs);
}
