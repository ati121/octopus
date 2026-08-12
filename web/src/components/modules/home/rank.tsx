'use client';

import { useChannelList } from '@/api/endpoints/channel';
import { useStatsModel } from '@/api/endpoints/stats';
import { useMemo } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { TrendingUp } from 'lucide-react';
import { Tabs, TabsList, TabsTrigger, TabsContents, TabsContent } from '@/components/animate-ui/components/animate/tabs';
import { useHomeViewStore, type RankSortMode } from '@/components/modules/home/store';
import { formatCount, formatCountCJK, formatMoney } from '@/lib/utils';

type ChannelData = NonNullable<ReturnType<typeof useChannelList>['data']>[number];
type ModelData = NonNullable<ReturnType<typeof useStatsModel>['data']>[number];

type FormattedValue = { value: string; unit: string };

export function Rank() {
    const { data: channelData } = useChannelList();
    const { data: modelData } = useStatsModel();
    const t = useTranslations('home.rank');
    const locale = useLocale();
    const rankSortMode = useHomeViewStore((state) => state.rankSortMode);
    const setRankSortMode = useHomeViewStore((state) => state.setRankSortMode);

    // 中文界面下 token 按万/亿显示，其余语言沿用 K/M/B。
    const useCJKCount = locale.startsWith('zh');
    const tokenText = (token: { raw: number; formatted: FormattedValue }): FormattedValue =>
        useCJKCount ? formatCountCJK(token.raw).formatted : token.formatted;

    const rankedChannels = useMemo(() => {
        if (!channelData) return { cost: [] as ChannelData[], count: [] as ChannelData[], tokens: [] as ChannelData[] };
        return {
            cost: [...channelData].sort((a, b) => b.formatted.total_cost.raw - a.formatted.total_cost.raw),
            count: [...channelData].sort((a, b) => b.formatted.request_count.raw - a.formatted.request_count.raw),
            tokens: [...channelData].sort((a, b) => b.formatted.total_token.raw - a.formatted.total_token.raw),
        };
    }, [channelData]);

    const rankedModels = useMemo(() => {
        if (!modelData) return { cost: [] as ModelData[], count: [] as ModelData[], tokens: [] as ModelData[] };
        return {
            cost: [...modelData].sort((a, b) => b.total_cost.raw - a.total_cost.raw),
            count: [...modelData].sort((a, b) => b.request_count.raw - a.request_count.raw),
            tokens: [...modelData].sort((a, b) => b.total_token.raw - a.total_token.raw),
        };
    }, [modelData]);

    const getMedalEmoji = (rank: number): string => {
        if (rank === 1) return '🥇';
        if (rank === 2) return '🥈';
        if (rank === 3) return '🥉';
        return '';
    };

    const emptyState = (
        <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <TrendingUp className="mb-3 size-12 opacity-30" />
            <p className="text-sm">{t('noData')}</p>
        </div>
    );

    // 总计：按当前激活标签（金额/次数/Tokens）对列表内全部条目求和。
    const totalText = (items: ChannelData[] | ModelData[], mode: RankSortMode, isChannel: boolean): FormattedValue => {
        const key = mode === 'count' ? 'request_count' : mode === 'tokens' ? 'total_token' : 'total_cost';
        let sum = 0;
        for (const item of items) {
            const value = isChannel ? (item as ChannelData).formatted[key].raw : (item as ModelData)[key].raw;
            sum += value || 0;
        }
        if (mode === 'count') return formatCount(sum).formatted;
        if (mode === 'tokens') return tokenText({ raw: sum, formatted: formatCount(sum).formatted });
        return formatMoney(sum).formatted;
    };

    const renderTotalRow = (total: FormattedValue) => (
        <div className="flex items-center gap-3 px-3 pb-2 text-muted-foreground">
            {/* 留出与列表项奖杯等宽的占位（size-8 = 32px） */}
            <div className="flex size-8 shrink-0 items-center justify-center" />
            <div className="min-w-0 flex-1">
                <span className="text-xs font-medium">{t('total')}</span>
            </div>
            <div className="flex shrink-0 items-center gap-1 text-right">
                <span className="text-base font-semibold">
                    {total.value}
                    <span className="text-xs">{total.unit}</span>
                </span>
            </div>
        </div>
    );

    const renderChannelList = (channels: ChannelData[], mode: RankSortMode) => {
        if (channels.length === 0) return emptyState;
        return (
            <div>
                {renderTotalRow(totalText(channels, mode, true))}
                <div className="max-h-[300px] space-y-3 overflow-y-auto">
                {channels.map((channel, index) => {
                    const rank = index + 1;
                    const successCount = channel.formatted.request_success.raw;
                    const failedCount = channel.formatted.request_failed.raw;
                    const totalCount = successCount + failedCount;
                    const successRate = totalCount > 0 ? (successCount / totalCount) * 100 : 0;
                    const tokenFmt = tokenText(channel.formatted.total_token);
                    return (
                        <div key={channel.raw.id} className="flex items-center gap-3 rounded-2xl p-3 transition-colors hover:bg-accent/5">
                            <div className="flex size-8 shrink-0 items-center justify-center rounded-lg text-lg font-bold">
                                {getMedalEmoji(rank) || rank}
                            </div>
                            <div className="min-w-0 flex-1">
                                <p className="truncate text-sm font-medium">{channel.raw.name}</p>
                                {mode === 'count' ? (
                                    <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
                                        <span>{t('successRate')}:</span>
                                        <span>{successRate.toFixed(1)}%</span>
                                    </div>
                                ) : null}
                            </div>
                            <div className="flex shrink-0 items-center gap-1 text-right">
                                {mode === 'count' ? (
                                    <div className="flex items-center gap-1 text-sm font-medium tabular-nums">
                                        <span className="text-accent">
                                            {channel.formatted.request_success.formatted.value}
                                            <span className="text-xs text-muted-foreground">{channel.formatted.request_success.formatted.unit}</span>
                                        </span>
                                        <span className="font-light text-muted-foreground/40">/</span>
                                        <span className="text-destructive">
                                            {channel.formatted.request_failed.formatted.value}
                                            <span className="text-xs text-muted-foreground">{channel.formatted.request_failed.formatted.unit}</span>
                                        </span>
                                    </div>
                                ) : mode === 'tokens' ? (
                                    <span className="text-base font-semibold">
                                        {tokenFmt.value}
                                        <span className="text-xs text-muted-foreground">{tokenFmt.unit}</span>
                                    </span>
                                ) : (
                                    <span className="text-base font-semibold">
                                        {channel.formatted.total_cost.formatted.value}
                                        <span className="text-xs text-muted-foreground">{channel.formatted.total_cost.formatted.unit}</span>
                                    </span>
                                )}
                            </div>
                        </div>
                    );
                })}
                </div>
            </div>
        );
    };

    const renderModelList = (models: ModelData[], mode: RankSortMode) => {
        if (models.length === 0) return emptyState;
        return (
            <div>
                {renderTotalRow(totalText(models, mode, false))}
                <div className="max-h-[300px] space-y-3 overflow-y-auto">
                {models.map((row, index) => {
                    const rank = index + 1;
                    const success = row.request_success.raw;
                    const failed = row.request_failed.raw;
                    const total = success + failed;
                    const successRate = total > 0 ? (success / total) * 100 : 0;
                    const tokenFmt = tokenText(row.total_token);
                    return (
                        <div key={`${row.id}-${row.name}`} className="flex items-center gap-3 rounded-2xl p-3 transition-colors hover:bg-accent/5">
                            <div className="flex size-8 shrink-0 items-center justify-center rounded-lg text-lg font-bold">
                                {getMedalEmoji(rank) || rank}
                            </div>
                            <div className="min-w-0 flex-1">
                                <p className="truncate text-sm font-medium" title={row.name}>{row.name || '—'}</p>
                                {mode === 'count' ? (
                                    <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
                                        <span>{t('successRate')}:</span>
                                        <span>{successRate.toFixed(1)}%</span>
                                    </div>
                                ) : row.cache_read_token.raw > 0 ? (
                                    <div className="mt-1 text-xs text-muted-foreground">cache {row.cache_hit_rate.raw.toFixed(1)}%</div>
                                ) : null}
                            </div>
                            <div className="flex shrink-0 items-center gap-1 text-right">
                                {mode === 'count' ? (
                                    <div className="flex items-center gap-1 text-sm font-medium tabular-nums">
                                        <span className="text-accent">
                                            {row.request_success.formatted.value}
                                            <span className="text-xs text-muted-foreground">{row.request_success.formatted.unit}</span>
                                        </span>
                                        <span className="font-light text-muted-foreground/40">/</span>
                                        <span className="text-destructive">
                                            {row.request_failed.formatted.value}
                                            <span className="text-xs text-muted-foreground">{row.request_failed.formatted.unit}</span>
                                        </span>
                                    </div>
                                ) : mode === 'tokens' ? (
                                    <span className="text-base font-semibold">
                                        {tokenFmt.value}
                                        <span className="text-xs text-muted-foreground">{tokenFmt.unit}</span>
                                    </span>
                                ) : (
                                    <span className="text-base font-semibold">
                                        {row.total_cost.formatted.value}
                                        <span className="text-xs text-muted-foreground">{row.total_cost.formatted.unit}</span>
                                    </span>
                                )}
                            </div>
                        </div>
                    );
                })}
                </div>
            </div>
        );
    };

    const channels = rankSortMode === 'count' ? rankedChannels.count : rankSortMode === 'tokens' ? rankedChannels.tokens : rankedChannels.cost;
    const models = rankSortMode === 'count' ? rankedModels.count : rankSortMode === 'tokens' ? rankedModels.tokens : rankedModels.cost;

    return (
        <div className="space-y-3 rounded-3xl border border-card-border bg-card p-4 text-card-foreground">
            <Tabs value={rankSortMode} onValueChange={(value) => setRankSortMode(value as RankSortMode)}>
                <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-base font-semibold">{t('title')}</h3>
                    <TabsList>
                        <TabsTrigger value="cost">{t('sortByCost')}</TabsTrigger>
                        <TabsTrigger value="count">{t('sortByCount')}</TabsTrigger>
                        <TabsTrigger value="tokens">{t('sortByTokens')}</TabsTrigger>
                    </TabsList>
                </div>
                <Tabs defaultValue="channel">
                    <TabsList className="mt-2">
                        <TabsTrigger value="channel">{t('dimensionChannel')}</TabsTrigger>
                        <TabsTrigger value="model">{t('dimensionModel')}</TabsTrigger>
                    </TabsList>
                    <TabsContents>
                        <TabsContent value="channel">{renderChannelList(channels, rankSortMode)}</TabsContent>
                        <TabsContent value="model">{renderModelList(models, rankSortMode)}</TabsContent>
                    </TabsContents>
                </Tabs>
            </Tabs>
        </div>
    );
}
