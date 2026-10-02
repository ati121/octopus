'use client';

import { useStatsChannel, useStatsModel, type StatsMetricsFormatted } from '@/api/endpoints/stats';
import { useTranslations } from 'next-intl';
import { TrendingUp } from 'lucide-react';
import { Tabs, TabsList, TabsTrigger, TabsContents, TabsContent } from '@/components/animate-ui/components/animate/tabs';
import { useHomeViewStore, type RankSortMode } from './store';
import { formatCount, formatTokens } from '@/lib/utils';

type RankRow = StatsMetricsFormatted & { name: string; channel_id: number; id?: number };

function RankList({ rows, mode }: { rows: RankRow[]; mode: RankSortMode }) {
    const t = useTranslations('home.rank');
    const key = mode === 'count' ? 'request_count' : 'total_token';
    const sorted = [...rows].sort((a, b) => b[key].raw - a[key].raw || a.name.localeCompare(b.name) || (a.id ?? a.channel_id) - (b.id ?? b.channel_id));
    const sum = rows.reduce((total, row) => total + row[key].raw, 0);
    const total = (mode === 'count' ? formatCount(sum) : formatTokens(sum)).formatted;
    if (!rows.length) return (
        <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
            <TrendingUp className="mb-3 size-12 opacity-30" />
            <p className="text-sm">{t('noData')}</p>
        </div>
    );
    return (
        <div>
            <div className="flex items-center justify-end px-3 pb-2 text-muted-foreground">
                <span className="mr-2 text-sm font-medium">{t('total')}:</span>
                <span className="text-base font-semibold">{total.value}<span className="text-xs">{total.unit}</span></span>
            </div>
            <div className="max-h-[300px] space-y-3 overflow-y-auto">
                {sorted.map((row, index) => {
                    const rate = row.request_count.raw > 0 ? row.request_success.raw / row.request_count.raw * 100 : 0;
                    return (
                        <div key={row.id ?? row.channel_id} className="flex items-center gap-3 rounded-2xl p-3 transition-colors hover:bg-accent/5">
                            <div className="flex size-8 shrink-0 items-center justify-center rounded-lg text-lg font-bold">{['🥇', '🥈', '🥉'][index] ?? index + 1}</div>
                            <div className="min-w-0 flex-1">
                                <p className="truncate text-sm font-medium" title={row.name}>{row.name}</p>
                                {mode === 'count' && <p className="mt-1 text-xs text-muted-foreground">{t('successRate')}: {rate.toFixed(1)}%</p>}
                            </div>
                            <div className="shrink-0 text-right font-semibold tabular-nums">
                                {mode === 'count' ? (
                                    <span className="text-sm">
                                        <span className="text-accent">{row.request_success.formatted.value}{row.request_success.formatted.unit}</span>
                                        <span className="mx-1 text-muted-foreground/40">/</span>
                                        <span className="text-destructive">{row.request_failed.formatted.value}{row.request_failed.formatted.unit}</span>
                                    </span>
                                ) : <span>{row.total_token.formatted.value}<span className="text-xs text-muted-foreground">{row.total_token.formatted.unit}</span></span>}
                            </div>
                        </div>
                    );
                })}
            </div>
        </div>
    );
}

export function Rank() {
    const { data: channels = [] } = useStatsChannel();
    const { data: models = [] } = useStatsModel();
    const t = useTranslations('home.rank');
    const mode = useHomeViewStore((state) => state.rankSortMode);
    const setMode = useHomeViewStore((state) => state.setRankSortMode);
    return (
        <div className="space-y-3 rounded-3xl border border-card-border bg-card p-4 text-card-foreground">
            <Tabs value={mode} onValueChange={(value) => setMode(value as RankSortMode)}>
                <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-base font-semibold">{t('title')}</h3>
                    <TabsList>
                        <TabsTrigger value="tokens">{t('sortByTokens')}</TabsTrigger>
                        <TabsTrigger value="count">{t('sortByCount')}</TabsTrigger>
                    </TabsList>
                </div>
                <Tabs defaultValue="channel">
                    <TabsList className="mt-2">
                        <TabsTrigger value="channel">{t('dimensionChannel')}</TabsTrigger>
                        <TabsTrigger value="model">{t('dimensionModel')}</TabsTrigger>
                    </TabsList>
                    <TabsContents>
                        <TabsContent value="channel"><RankList rows={channels} mode={mode} /></TabsContent>
                        <TabsContent value="model"><RankList rows={models} mode={mode} /></TabsContent>
                    </TabsContents>
                </Tabs>
            </Tabs>
        </div>
    );
}
