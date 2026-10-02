'use client';

import { memo, useMemo, useState } from 'react';
import { ChevronDown, ChevronRight, Copy } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { toast } from '@/components/common/Toast';
import { getLogContentPreview, textBoundary, type LogContentKind } from './content-preview';

type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };

const ITEMS_PER_PAGE = 25;
const TEXT_PER_PAGE = 8000;
const TEXT_PREVIEW_LENGTH = 100;
const actionClass = 'rounded px-1.5 py-0.5 text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-2 disabled:opacity-40';

async function copyText(text: string) {
    try {
        await navigator.clipboard.writeText(text);
        return;
    } catch {
        // 局域网 HTTP 页面没有 Clipboard API，沿用原查看器的复制兜底。
        const active = document.activeElement as HTMLElement | null;
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.style.cssText = 'position:fixed;opacity:0;pointer-events:none';
        document.body.appendChild(textarea);
        try {
            textarea.select();
            if (!document.execCommand('copy')) throw new Error('Copy failed');
        } finally {
            textarea.remove();
            active?.focus({ preventScroll: true });
        }
    }
}

function ContentPager({ page, pages, onChange }: { page: number; pages: number; onChange: (page: number) => void }) {
    const t = useTranslations('log.content');
    if (pages <= 1) return null;
    return (
        <div className="flex flex-wrap items-center gap-2 py-1 font-sans text-xs">
            <button type="button" className={actionClass} disabled={page === 0} onClick={() => onChange(page - 1)}>{t('previous')}</button>
            <span className="text-muted-foreground tabular-nums">{t('page', { current: page + 1, total: pages })}</span>
            <button type="button" className={actionClass} disabled={page === pages - 1} onClick={() => onChange(page + 1)}>{t('next')}</button>
        </div>
    );
}

function CopyValue({ value, raw = false }: { value: JsonValue; raw?: boolean }) {
    const t = useTranslations('log.content');
    const copy = useTranslations('common.copy');
    return (
        <button
            type="button"
            className={`${actionClass} shrink-0 opacity-0 group-hover/line:opacity-100 focus:opacity-100 [@media(hover:none)]:opacity-100`}
            aria-label={t('copyFull')}
            title={t('copyFull')}
            onClick={async () => {
                try {
                    // 仅点击时序列化；悬停用 CSS，不广播整棵树的 React 状态。
                    await copyText(raw && typeof value === 'string' ? value : JSON.stringify(value, null, 2));
                    toast.success(copy('success'));
                } catch {
                    toast.error(copy('failed'));
                }
            }}
        >
            <Copy className="size-3.5" />
        </button>
    );
}

const TextValue = memo(function TextValue({ text, plain = false }: { text: string; plain?: boolean }) {
    const t = useTranslations('log.content');
    const [expanded, setExpanded] = useState(plain);
    const [requestedPage, setPage] = useState(0);
    const pages = Math.max(1, Math.ceil(text.length / TEXT_PER_PAGE));
    const page = Math.min(requestedPage, pages - 1);
    const long = text.length > TEXT_PREVIEW_LENGTH;
    const start = textBoundary(text, page * TEXT_PER_PAGE);
    const end = textBoundary(text, Math.min(text.length, (page + 1) * TEXT_PER_PAGE));

    if (!long) return <span className="whitespace-pre-wrap break-all text-blue-600 dark:text-blue-300">{plain ? text : JSON.stringify(text)}</span>;

    return (
        <div className="min-w-0 flex-1">
            {!plain && (
                <button type="button" className="max-w-full text-left text-blue-600 dark:text-blue-300 break-all" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
                    {expanded ? t('collapseText') : `${JSON.stringify(text.slice(0, textBoundary(text, TEXT_PREVIEW_LENGTH)))}… ${t('expandText')}`}
                </button>
            )}
            {expanded && (
                <div className="min-w-0">
                    <ContentPager page={page} pages={pages} onChange={setPage} />
                    <pre className="m-0 whitespace-pre-wrap break-all font-mono leading-relaxed text-blue-600 dark:text-blue-300">{text.slice(start, end)}</pre>
                </div>
            )}
        </div>
    );
});

const JsonNode = memo(function JsonNode({ value, name, depth = 0 }: { value: JsonValue; name?: string | number; depth?: number }) {
    const t = useTranslations('log.content');
    const [expanded, setExpanded] = useState(depth === 0);
    const [requestedPage, setPage] = useState(0);
    const container = value !== null && typeof value === 'object';
    const array = Array.isArray(value);
    // 数组只生成当前页索引；折叠对象不创建后代组件。
    const keys = useMemo(() => container && !array ? Object.keys(value) : [], [array, container, value]);
    const count = array ? value.length : keys.length;
    const pages = Math.max(1, Math.ceil(count / ITEMS_PER_PAGE));
    const page = Math.min(requestedPage, pages - 1);
    const from = page * ITEMS_PER_PAGE;
    const visibleKeys = array
        ? Array.from({ length: Math.min(ITEMS_PER_PAGE, count - from) }, (_, i) => from + i)
        : keys.slice(from, from + ITEMS_PER_PAGE);
    const label = name === undefined ? '' : typeof name === 'number' ? `${name}: ` : `${JSON.stringify(name.length > 100 ? `${name.slice(0, 100)}…` : name)}: `;

    return (
        <div className="min-w-0 font-mono text-xs leading-6" data-log-node>
            <div className="group/line flex min-w-0 items-start gap-1">
                {container ? (
                    <button type="button" className="flex min-w-0 items-start gap-1 text-left break-all" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
                        {expanded ? <ChevronDown className="mt-1 size-3.5 shrink-0" /> : <ChevronRight className="mt-1 size-3.5 shrink-0" />}
                        <span><span className="text-violet-600 dark:text-violet-300">{label}</span>{array ? '[' : '{'}<span className="mx-1 text-muted-foreground">{t('items', { count })}</span>{expanded ? '' : array ? ']' : '}'}</span>
                    </button>
                ) : (
                    <div className="flex min-w-0 flex-1 flex-wrap items-start gap-x-1 pl-4">
                        {label && <span className="break-all text-violet-600 dark:text-violet-300">{label}</span>}
                        {typeof value === 'string' ? <TextValue text={value} /> : <span className="text-amber-700 dark:text-amber-300">{String(value)}</span>}
                    </div>
                )}
                <CopyValue value={value} raw={typeof value === 'string'} />
            </div>
            {container && expanded && (
                <div className="ml-2 min-w-0 border-l border-border pl-3">
                    <ContentPager page={page} pages={pages} onChange={setPage} />
                    {visibleKeys.map((key) => (
                        <JsonNode key={key} name={key} value={array ? value[Number(key)] : (value as Record<string, JsonValue>)[String(key)]} depth={depth + 1} />
                    ))}
                </div>
            )}
            {container && expanded && <div className="pl-4 text-muted-foreground">{array ? ']' : '}'}</div>}
        </div>
    );
});

export const LogContent = memo(function LogContent({ content, kind, fallbackText, isLoading }: { content: string | undefined; kind: LogContentKind; fallbackText: string; isLoading?: boolean }) {
    const t = useTranslations('log.content');
    const [detailsOpen, setDetailsOpen] = useState(false);
    const parsed = useMemo(() => {
        if (!content) return null;
        try {
            return { value: JSON.parse(content) as JsonValue };
        } catch {
            return null;
        }
    }, [content]);
    const preview = useMemo(() => parsed ? getLogContentPreview(parsed.value, kind) : content ?? '', [parsed, kind, content]);

    if (!content) return <div className="p-4 text-xs text-muted-foreground">{isLoading ? t('loading') : fallbackText}</div>;

    return (
        <div className="min-w-0 p-4 [contain:layout_paint]">
            <div className="group/line flex items-center justify-between gap-2 text-xs">
                <button type="button" className="flex items-center gap-1 py-1" aria-expanded={detailsOpen} onClick={() => setDetailsOpen(!detailsOpen)}>
                    {detailsOpen ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
                    {t('details')}
                </button>
                <CopyValue value={content} raw />
            </div>
            {detailsOpen && (parsed ? <JsonNode value={parsed.value} /> : <TextValue text={content} plain />)}
            <div className="group/line mb-2 mt-3 flex items-center justify-between gap-2 border-t border-border pt-3 font-sans text-xs text-muted-foreground">
                <span>{t(kind === 'request' ? 'latestInput' : 'latestOutput')}</span>
                {preview && <CopyValue value={preview} raw />}
            </div>
            {preview ? <div className="text-sm"><TextValue text={preview} plain /></div> : <p className="text-xs text-muted-foreground">{t('noText')}</p>}
        </div>
    );
});
