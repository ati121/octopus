'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { ArrowUpFromLine, Braces, Waypoints } from 'lucide-react';
import {
    MorphingDialog,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogTrigger,
} from '@/components/ui/morphing-dialog';
import { SettingKey, useSettingList } from '@/api/endpoints/setting';
import { SettingCard, SettingRow } from './shared';
import { UpstreamRequestDialogContent } from './UpstreamRequestDialog';

const SEPARATOR = ' · ';

// 只用于卡片摘要计数，解析失败按 0 处理，不影响弹窗内的编辑。
function countJSONArray(raw: string | undefined): number {
    if (!raw) return 0;
    try {
        const parsed = JSON.parse(raw) as unknown;
        return Array.isArray(parsed) ? parsed.length : 0;
    } catch {
        return 0;
    }
}

// 判断参数覆盖是否配置过：与后端 isBlankParamOverride 的口径一致。
function hasParamOverride(raw: string | undefined): boolean {
    const trimmed = (raw ?? '').trim();
    if (!trimmed || trimmed === '{}' || trimmed === 'null') return false;
    try {
        const parsed = JSON.parse(trimmed) as unknown;
        return !!parsed && typeof parsed === 'object' && !Array.isArray(parsed) && Object.keys(parsed).length > 0;
    } catch {
        return false;
    }
}

export function SettingUpstreamRequest() {
    const t = useTranslations('setting.upstreamRequest');
    const { data: settings } = useSettingList();

    const summary = useMemo(() => {
        const get = (key: string) => settings?.find((s) => s.key === key)?.value;
        return {
            headerCount: countJSONArray(get(SettingKey.UpstreamGlobalHeaders)),
            headerRuleCount: countJSONArray(get(SettingKey.UpstreamModelHeaderRules)),
            paramEnabled: hasParamOverride(get(SettingKey.UpstreamGlobalParamOverride)),
            paramRuleCount: countJSONArray(get(SettingKey.UpstreamModelParamRules)),
        };
    }, [settings]);

    const headerSummary =
        t('summaryItems', { count: summary.headerCount }) +
        SEPARATOR +
        t('summaryRules', { count: summary.headerRuleCount });
    const paramSummary =
        (summary.paramEnabled ? t('summaryParamOn') : t('summaryParamOff')) +
        SEPARATOR +
        t('summaryRules', { count: summary.paramRuleCount });

    return (
        <SettingCard icon={Waypoints} title={t('title')} tooltip={t('cardHint')}>
            <SettingRow icon={ArrowUpFromLine} label={t('summaryHeaders')}>
                <span className="text-sm text-muted-foreground">{headerSummary}</span>
            </SettingRow>

            <SettingRow icon={Braces} label={t('summaryParam')}>
                <span className="text-sm text-muted-foreground">{paramSummary}</span>
            </SettingRow>

            {/* 触发器自身就是可点区域（role=button）：不能再往里嵌真实 <button>，
                嵌套交互元素会吞掉点击，导致弹窗打不开。样式直接给到 trigger，与 APIKey 卡片一致。 */}
            <MorphingDialog>
                <MorphingDialogTrigger className="flex h-10 w-full items-center justify-center rounded-xl bg-secondary text-sm font-medium text-secondary-foreground transition-colors hover:bg-secondary/80">
                    {t('configure')}
                </MorphingDialogTrigger>
                <MorphingDialogContainer>
                    <MorphingDialogContent className="custom-shadow flex max-h-[calc(100vh-2rem)] w-fit max-w-full flex-col overflow-hidden rounded-3xl bg-card px-6 py-4 text-card-foreground">
                        <UpstreamRequestDialogContent />
                    </MorphingDialogContent>
                </MorphingDialogContainer>
            </MorphingDialog>
        </SettingCard>
    );
}
