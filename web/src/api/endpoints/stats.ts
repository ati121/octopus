import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { formatCount, formatMoney, formatTime } from '@/lib/utils';

/**
 * 统计数据
 */
export interface StatsMetrics {
    input_token: number;
    output_token: number;
    cache_read_token: number;
    cache_write_token?: number;
    input_cost: number;
    output_cost: number;
    wait_time: number;
    request_success: number;
    request_failed: number;
}

export interface StatsMetricsFormatted {
    input_token: ReturnType<typeof formatCount>;
    output_token: ReturnType<typeof formatCount>;
    cache_read_token: ReturnType<typeof formatCount>;
    cache_write_token: ReturnType<typeof formatCount>;
    cache_hit_rate: ReturnType<typeof formatCacheHitRate>;
    input_cost: ReturnType<typeof formatMoney>;
    output_cost: ReturnType<typeof formatMoney>;
    wait_time: ReturnType<typeof formatTime>;
    request_success: ReturnType<typeof formatCount>;
    request_failed: ReturnType<typeof formatCount>;

    request_count: ReturnType<typeof formatCount>;
    total_token: ReturnType<typeof formatCount>;
    total_cost: ReturnType<typeof formatMoney>;
}

export function formatCacheHitRate(inputToken: number, cacheReadToken: number) {
    const totalInput = Math.max(0, inputToken);
    const rate = totalInput > 0 ? (Math.max(0, cacheReadToken) / totalInput) * 100 : 0;
    return {
        raw: rate,
        formatted: { value: rate.toFixed(2), unit: '%' },
    };
}

export function formatStatsMetrics(item: StatsMetrics): StatsMetricsFormatted {
    const inputToken = item.input_token || 0;
    const outputToken = item.output_token || 0;
    const cacheReadToken = item.cache_read_token || 0;
    const cacheWriteToken = item.cache_write_token || 0;
    return {
        input_token: formatCount(inputToken),
        output_token: formatCount(outputToken),
        cache_read_token: formatCount(cacheReadToken),
        cache_write_token: formatCount(cacheWriteToken),
        cache_hit_rate: formatCacheHitRate(inputToken, cacheReadToken),
        total_token: formatCount(inputToken + outputToken),
        input_cost: formatMoney(item.input_cost),
        output_cost: formatMoney(item.output_cost),
        total_cost: formatMoney(item.input_cost + item.output_cost),
        wait_time: formatTime(item.wait_time),
        request_success: formatCount(item.request_success),
        request_failed: formatCount(item.request_failed),
        request_count: formatCount(item.request_success + item.request_failed),
    };
}

export interface StatsChannel extends StatsMetrics {
    channel_id: number;
}

export interface StatsDaily extends StatsMetrics {
    date: string;
}
export interface StatsDailyFormatted extends StatsMetricsFormatted {
    date: string;
}

export interface StatsTotal extends StatsMetrics {
    id: number;
}
export type StatsTotalFormatted = StatsMetricsFormatted;

export interface StatsHourly extends StatsMetrics {
    hour: number;
    date: string;
}
export interface StatsHourlyFormatted extends StatsMetricsFormatted {
    hour: number;
    date: string;
}
/**
 * API Key 统计数据
 */
export interface StatsAPIKey extends StatsMetrics {
    api_key_id: number;
}

export interface StatsAPIKeyFormatted extends StatsMetricsFormatted {
    api_key_id: number;
}

export interface StatsModel extends StatsMetrics {
    id: number;
    name: string;
    channel_id: number;
}

export interface StatsModelFormatted extends StatsMetricsFormatted {
    id: number;
    name: string;
    channel_id: number;
}
/**
 * 获取今日统计数据 Hook
 */
export function useStatsToday() {
    return useQuery({
        queryKey: ['stats', 'today'],
        queryFn: async () => {
            return apiClient.get<StatsDaily>('/api/v1/stats/today');
        },
        refetchInterval: 30000,
    });
}

/**
 * 获取每日统计数据 Hook
 */
export function useStatsDaily() {
    return useQuery({
        queryKey: ['stats', 'daily'],
        queryFn: async () => {
            return apiClient.get<StatsDaily[]>('/api/v1/stats/daily');
        },
        select: (data) => data.map((item): StatsDailyFormatted => ({
            ...formatStatsMetrics(item),
            date: item.date,
        })),
        refetchInterval: 3600000, // 1 小时
    });
}
/**
 * 获取总统计数据 Hook
 */
export function useStatsHourly() {
    return useQuery({
        queryKey: ['stats', 'hourly'],
        queryFn: async () => {
            return apiClient.get<StatsHourly[]>('/api/v1/stats/hourly');
        },
        select: (data) => data.map((item): StatsHourlyFormatted => ({
            hour: item.hour,
            date: item.date,
            ...formatStatsMetrics(item),
        })),
        refetchInterval: 10000,// 10 秒
    });
}

export function useStatsTotal() {
    return useQuery({
        queryKey: ['stats', 'total'],
        queryFn: async () => {
            return apiClient.get<StatsTotal>('/api/v1/stats/total');
        },
        select: (data) => formatStatsMetrics(data),
        refetchInterval: 10000,// 10 秒
    });
}

export function useClearStats() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () => apiClient.delete<null>('/api/v1/stats/clear'),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['stats'] });
        },
    });
}



/**
 * 获取 API Key 统计数据列表 Hook
 */
export function useStatsAPIKey() {
    return useQuery({
        queryKey: ['stats', 'apikey'],
        queryFn: async () => {
            return apiClient.get<StatsAPIKey[]>('/api/v1/stats/apikey');
        },
        select: (data) => data.map((item): StatsAPIKeyFormatted => ({
            api_key_id: item.api_key_id,
            ...formatStatsMetrics(item),
        })),
        refetchInterval: 30000,
    });
}

export function useStatsModel() {
    return useQuery({
        queryKey: ['stats', 'model'],
        queryFn: async () => apiClient.get<StatsModel[]>('/api/v1/stats/model'),
        select: (data) => data.map((item): StatsModelFormatted => ({
            id: item.id,
            name: item.name,
            channel_id: item.channel_id,
            ...formatStatsMetrics(item),
        })),
        refetchInterval: 30000,
    });
}
