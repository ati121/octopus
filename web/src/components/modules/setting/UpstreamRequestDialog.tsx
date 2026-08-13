'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Plus, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { MorphingDialogClose, MorphingDialogTitle, useMorphingDialog } from '@/components/ui/morphing-dialog';
import { toast } from '@/components/common/Toast';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import type { CustomHeader } from '@/api/endpoints/channel';
import type { ApiError } from '@/api/types';

// 参数覆盖在草稿态用字符串承载：编辑中途允许是非法 JSON，保存前统一校验。
type ParamRuleDraft = { models: string; paramOverride: string };
type HeaderRuleDraft = { models: string; headers: CustomHeader[] };

const TEXTAREA_CLASS =
    'min-h-24 w-full rounded-xl border border-border bg-background px-3 py-2 font-mono text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring';

function emptyHeader(): CustomHeader {
    return { header_key: '', header_value: '' };
}

function findSetting(settings: { key: string; value: string }[] | undefined, key: string): string {
    return settings?.find((s) => s.key === key)?.value ?? '';
}

// 后端存的是 JSON 文本，解析失败时退化为空，不阻塞界面。
function parseHeaders(raw: string): CustomHeader[] {
    try {
        const parsed = JSON.parse(raw || '[]') as unknown;
        if (!Array.isArray(parsed)) return [];
        return parsed
            .filter((item): item is CustomHeader => !!item && typeof item === 'object')
            .map((item) => ({ header_key: item.header_key ?? '', header_value: item.header_value ?? '' }));
    } catch {
        return [];
    }
}

function parseHeaderRules(raw: string): HeaderRuleDraft[] {
    try {
        const parsed = JSON.parse(raw || '[]') as unknown;
        if (!Array.isArray(parsed)) return [];
        return parsed.map((item) => {
            const rule = (item ?? {}) as { models?: string; headers?: unknown };
            const headers = Array.isArray(rule.headers)
                ? rule.headers.map((h) => {
                    const header = (h ?? {}) as CustomHeader;
                    return { header_key: header.header_key ?? '', header_value: header.header_value ?? '' };
                })
                : [];
            return { models: rule.models ?? '', headers: headers.length > 0 ? headers : [emptyHeader()] };
        });
    } catch {
        return [];
    }
}

function parseParamRules(raw: string): ParamRuleDraft[] {
    try {
        const parsed = JSON.parse(raw || '[]') as unknown;
        if (!Array.isArray(parsed)) return [];
        return parsed.map((item) => {
            const rule = (item ?? {}) as { models?: string; param_override?: unknown };
            const override = rule.param_override;
            return {
                models: rule.models ?? '',
                paramOverride:
                    override === undefined || override === null ? '' : JSON.stringify(override, null, 2),
            };
        });
    } catch {
        return [];
    }
}

// 返回 null 表示合法（空串视为无覆盖）；否则返回 i18n 错误键。
function checkParamOverride(text: string): 'invalidJSON' | 'forbiddenModel' | null {
    const trimmed = text.trim();
    if (!trimmed) return null;
    let parsed: unknown;
    try {
        parsed = JSON.parse(trimmed);
    } catch {
        return 'invalidJSON';
    }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return 'invalidJSON';
    const hasModel = Object.keys(parsed as Record<string, unknown>).some(
        (key) => key.trim().toLowerCase() === 'model'
    );
    return hasModel ? 'forbiddenModel' : null;
}

export function UpstreamRequestDialogContent() {
    const t = useTranslations('setting.upstreamRequest');
    // 保存成功/失败沿用设置页通用文案，与其它卡片一致
    const tSetting = useTranslations('setting');
    const { setIsOpen } = useMorphingDialog();
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [globalHeaders, setGlobalHeaders] = useState<CustomHeader[]>([]);
    const [headerRules, setHeaderRules] = useState<HeaderRuleDraft[]>([]);
    const [globalParam, setGlobalParam] = useState('');
    const [paramRules, setParamRules] = useState<ParamRuleDraft[]>([]);
    const [isSaving, setIsSaving] = useState(false);

    // 只首次回填：useSettingList 每 30s 轮询，反复回填会覆盖正在编辑的内容。
    const initialized = useRef(false);
    useEffect(() => {
        if (!settings || initialized.current) return;
        const headers = parseHeaders(findSetting(settings, SettingKey.UpstreamGlobalHeaders));
        const hRules = parseHeaderRules(findSetting(settings, SettingKey.UpstreamModelHeaderRules));
        const param = findSetting(settings, SettingKey.UpstreamGlobalParamOverride).trim();
        const pRules = parseParamRules(findSetting(settings, SettingKey.UpstreamModelParamRules));
        queueMicrotask(() => {
            setGlobalHeaders(headers.length > 0 ? headers : [emptyHeader()]);
            setHeaderRules(hRules);
            setGlobalParam(param === '{}' ? '' : param);
            setParamRules(pRules);
        });
        initialized.current = true;
    }, [settings]);

    const updateGlobalHeader = useCallback((index: number, patch: Partial<CustomHeader>) => {
        setGlobalHeaders((prev) => prev.map((h, i) => (i === index ? { ...h, ...patch } : h)));
    }, []);

    const updateRuleHeader = useCallback((ruleIndex: number, headerIndex: number, patch: Partial<CustomHeader>) => {
        setHeaderRules((prev) =>
            prev.map((rule, i) =>
                i === ruleIndex
                    ? { ...rule, headers: rule.headers.map((h, j) => (j === headerIndex ? { ...h, ...patch } : h)) }
                    : rule
            )
        );
    }, []);

    const handleSave = async () => {
        // 服务端拒绝时不会保留输入，先在本地拦一道。
        const globalError = checkParamOverride(globalParam);
        if (globalError) {
            toast.error(t(globalError));
            return;
        }
        for (const rule of headerRules) {
            if (!rule.models.trim()) {
                toast.error(t('emptyModels'));
                return;
            }
            if (rule.headers.some((h) => !h.header_key.trim() && h.header_value.trim())) {
                toast.error(t('emptyHeaderKey'));
                return;
            }
        }
        for (const rule of paramRules) {
            if (!rule.models.trim()) {
                toast.error(t('emptyModels'));
                return;
            }
            const error = checkParamOverride(rule.paramOverride);
            if (error) {
                toast.error(t(error));
                return;
            }
        }

        // 提交前剔除空行/空规则，避免存进无意义的条目。
        const cleanHeaders = globalHeaders.filter((h) => h.header_key.trim());
        const cleanHeaderRules = headerRules
            .map((rule) => ({
                models: rule.models.trim(),
                headers: rule.headers.filter((h) => h.header_key.trim()),
            }))
            .filter((rule) => rule.headers.length > 0);
        const cleanParamRules = paramRules
            .filter((rule) => checkParamOverride(rule.paramOverride) === null && rule.paramOverride.trim())
            .map((rule) => ({
                models: rule.models.trim(),
                param_override: JSON.parse(rule.paramOverride) as unknown,
            }));

        const payload: { key: string; value: string }[] = [
            { key: SettingKey.UpstreamGlobalHeaders, value: JSON.stringify(cleanHeaders) },
            { key: SettingKey.UpstreamModelHeaderRules, value: JSON.stringify(cleanHeaderRules) },
            { key: SettingKey.UpstreamGlobalParamOverride, value: globalParam.trim() || '{}' },
            { key: SettingKey.UpstreamModelParamRules, value: JSON.stringify(cleanParamRules) },
        ];

        setIsSaving(true);
        try {
            for (const item of payload) {
                await setSetting.mutateAsync(item);
            }
            toast.success(tSetting('saved'));
            setIsOpen(false);
        } catch (error) {
            // 服务端校验失败的具体原因（如 rule 2: models must not be empty）走 description
            toast.error(tSetting('saveFailed'), { description: (error as ApiError)?.message });
        } finally {
            setIsSaving(false);
        }
    };

    const sectionTitle = 'text-sm font-semibold text-card-foreground';
    const hintText = 'text-xs text-muted-foreground';

    return (
        <div className="flex h-[calc(100vh-2rem)] min-h-0 w-screen max-w-full flex-col overflow-hidden md:max-w-2xl">
            <MorphingDialogTitle className="shrink-0">
                <div className="flex items-start justify-between gap-4">
                    <div className="space-y-1">
                        <h2 className="text-lg font-bold text-card-foreground">{t('title')}</h2>
                        <p className={hintText}>{t('cardHint')}</p>
                    </div>
                    <MorphingDialogClose className="relative right-0 top-0 shrink-0" />
                </div>
            </MorphingDialogTitle>

            <div className="mt-4 min-h-0 flex-1 space-y-6 overflow-y-auto px-1">
                {/* 全局请求头 */}
                <section className="space-y-2">
                    <div className="flex items-center justify-between">
                        <span className={sectionTitle}>
                            {t('globalHeaders')}{globalHeaders.length > 0 ? ` (${globalHeaders.length})` : ''}
                        </span>
                        <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => setGlobalHeaders((prev) => [...prev, emptyHeader()])}
                            className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                        >
                            <Plus className="mr-1 h-3 w-3" />
                            {t('add')}
                        </Button>
                    </div>
                    <p className={hintText}>{t('globalHeadersHint')}</p>
                    <div className="space-y-2">
                        {globalHeaders.length === 0 ? (
                            <p className={hintText}>{t('noHeaders')}</p>
                        ) : (
                            globalHeaders.map((header, index) => (
                                <div key={`gh-${index}`} className="flex items-center gap-2">
                                    <Input
                                        value={header.header_key}
                                        onChange={(e) => updateGlobalHeader(index, { header_key: e.target.value })}
                                        placeholder={t('headerKey')}
                                        className="flex-1 rounded-xl"
                                    />
                                    <Input
                                        value={header.header_value}
                                        onChange={(e) => updateGlobalHeader(index, { header_value: e.target.value })}
                                        placeholder={t('headerValue')}
                                        className="flex-1 rounded-xl"
                                    />
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => setGlobalHeaders((prev) => prev.filter((_, i) => i !== index))}
                                        className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive"
                                        title={t('remove')}
                                    >
                                        <X className="h-4 w-4" />
                                    </Button>
                                </div>
                            ))
                        )}
                    </div>
                </section>

                {/* 模型请求头规则 */}
                <section className="space-y-2 border-t border-border pt-4">
                    <div className="flex items-center justify-between">
                        <span className={sectionTitle}>{t('modelHeaderRules')}</span>
                        <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => setHeaderRules((prev) => [...prev, { models: '', headers: [emptyHeader()] }])}
                            className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                        >
                            <Plus className="mr-1 h-3 w-3" />
                            {t('addRule')}
                        </Button>
                    </div>
                    <p className={hintText}>{t('modelHeaderRulesHint')}</p>
                    {headerRules.length === 0 ? (
                        <p className={hintText}>{t('noRules')}</p>
                    ) : (
                        <div className="space-y-3">
                            {headerRules.map((rule, ruleIndex) => (
                                <div key={`hr-${ruleIndex}`} className="space-y-2 rounded-2xl border border-border/60 p-3">
                                    <div className="flex items-center gap-2">
                                        <Input
                                            value={rule.models}
                                            onChange={(e) =>
                                                setHeaderRules((prev) =>
                                                    prev.map((r, i) => (i === ruleIndex ? { ...r, models: e.target.value } : r))
                                                )
                                            }
                                            placeholder={t('modelsPlaceholder')}
                                            className="flex-1 rounded-xl"
                                        />
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => setHeaderRules((prev) => prev.filter((_, i) => i !== ruleIndex))}
                                            className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive"
                                            title={t('remove')}
                                        >
                                            <X className="h-4 w-4" />
                                        </Button>
                                    </div>
                                    {rule.headers.map((header, headerIndex) => (
                                        <div key={`hr-${ruleIndex}-h-${headerIndex}`} className="flex items-center gap-2">
                                            <Input
                                                value={header.header_key}
                                                onChange={(e) => updateRuleHeader(ruleIndex, headerIndex, { header_key: e.target.value })}
                                                placeholder={t('headerKey')}
                                                className="flex-1 rounded-xl"
                                            />
                                            <Input
                                                value={header.header_value}
                                                onChange={(e) => updateRuleHeader(ruleIndex, headerIndex, { header_value: e.target.value })}
                                                placeholder={t('headerValue')}
                                                className="flex-1 rounded-xl"
                                            />
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="sm"
                                                onClick={() =>
                                                    setHeaderRules((prev) =>
                                                        prev.map((r, i) =>
                                                            i === ruleIndex
                                                                ? { ...r, headers: r.headers.filter((_, j) => j !== headerIndex) }
                                                                : r
                                                        )
                                                    )
                                                }
                                                disabled={rule.headers.length <= 1}
                                                className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive disabled:opacity-40"
                                                title={t('remove')}
                                            >
                                                <X className="h-4 w-4" />
                                            </Button>
                                        </div>
                                    ))}
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={() =>
                                            setHeaderRules((prev) =>
                                                prev.map((r, i) => (i === ruleIndex ? { ...r, headers: [...r.headers, emptyHeader()] } : r))
                                            )
                                        }
                                        className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                                    >
                                        <Plus className="mr-1 h-3 w-3" />
                                        {t('add')}
                                    </Button>
                                </div>
                            ))}
                        </div>
                    )}
                </section>

                {/* 全局参数覆盖 */}
                <section className="space-y-2 border-t border-border pt-4">
                    <span className={sectionTitle}>{t('globalParam')}</span>
                    <textarea
                        value={globalParam}
                        onChange={(e) => setGlobalParam(e.target.value)}
                        placeholder={t('globalParamPlaceholder')}
                        spellCheck={false}
                        className={TEXTAREA_CLASS}
                    />
                    <p className={hintText}>{t('globalParamHint')}</p>
                </section>

                {/* 模型参数覆盖规则 */}
                <section className="space-y-2 border-t border-border pt-4">
                    <div className="flex items-center justify-between">
                        <span className={sectionTitle}>{t('modelParamRules')}</span>
                        <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => setParamRules((prev) => [...prev, { models: '', paramOverride: '' }])}
                            className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                        >
                            <Plus className="mr-1 h-3 w-3" />
                            {t('addRule')}
                        </Button>
                    </div>
                    {paramRules.length === 0 ? (
                        <p className={hintText}>{t('noRules')}</p>
                    ) : (
                        <div className="space-y-3">
                            {paramRules.map((rule, ruleIndex) => (
                                <div key={`pr-${ruleIndex}`} className="space-y-2 rounded-2xl border border-border/60 p-3">
                                    <div className="flex items-center gap-2">
                                        <Input
                                            value={rule.models}
                                            onChange={(e) =>
                                                setParamRules((prev) =>
                                                    prev.map((r, i) => (i === ruleIndex ? { ...r, models: e.target.value } : r))
                                                )
                                            }
                                            placeholder={t('modelsPlaceholder')}
                                            className="flex-1 rounded-xl"
                                        />
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => setParamRules((prev) => prev.filter((_, i) => i !== ruleIndex))}
                                            className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive"
                                            title={t('remove')}
                                        >
                                            <X className="h-4 w-4" />
                                        </Button>
                                    </div>
                                    <textarea
                                        value={rule.paramOverride}
                                        onChange={(e) =>
                                            setParamRules((prev) =>
                                                prev.map((r, i) => (i === ruleIndex ? { ...r, paramOverride: e.target.value } : r))
                                            )
                                        }
                                        placeholder={t('globalParamPlaceholder')}
                                        spellCheck={false}
                                        className={TEXTAREA_CLASS}
                                    />
                                </div>
                            ))}
                        </div>
                    )}
                    <p className={hintText}>{t('mergeOrder')}</p>
                    <p className={hintText}>{t('scopeHint')}</p>
                </section>
            </div>

            <div className="mt-4 flex shrink-0 flex-col gap-2 sm:flex-row">
                <Button
                    type="button"
                    variant="secondary"
                    className="h-11 flex-1 rounded-xl"
                    onClick={() => setIsOpen(false)}
                    disabled={isSaving}
                >
                    {t('cancel')}
                </Button>
                <Button type="button" className="h-11 flex-1 rounded-xl" onClick={handleSave} disabled={isSaving}>
                    {isSaving ? t('saving') : t('save')}
                </Button>
            </div>
        </div>
    );
}
