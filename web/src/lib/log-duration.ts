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

// 首字写出前显示等待时间，写出后固定首字时间并继续推进总耗时。
export function durationPairText(ftutMs: number, liveMs: number | null, useTimeMs: number): string {
    const firstTokenMs = ftutMs > 0 ? ftutMs : null;
    if (liveMs != null) {
        if (firstTokenMs == null) {
            return formatDurationPair(liveMs, null);
        }
        return formatDurationPair(firstTokenMs, Math.max(firstTokenMs, liveMs));
    }
    return formatDurationPair(firstTokenMs, useTimeMs);
}

// Magpie 的输出速度口径：输出 Token / 首字之后的生成时长。
// 首字到结束不足 1 秒时不显示，避免短请求或一次性突发输出造成误导。
export function outputSpeedTokensPerSecond(
    outputTokens: number,
    firstTokenMs: number,
    totalMs: number,
): number | null {
    if (!Number.isFinite(outputTokens) || outputTokens <= 0) return null;
    if (!Number.isFinite(firstTokenMs) || firstTokenMs <= 0) return null;
    if (!Number.isFinite(totalMs) || totalMs <= firstTokenMs) return null;

    const generationMs = totalMs - firstTokenMs;
    if (generationMs < 1000) return null;
    return outputTokens * 1000 / generationMs;
}

export function formatOutputSpeed(
    outputTokens: number,
    firstTokenMs: number,
    totalMs: number,
): string | null {
    const speed = outputSpeedTokensPerSecond(outputTokens, firstTokenMs, totalMs);
    return speed == null ? null : `${Math.round(speed)} tok/s`;
}
