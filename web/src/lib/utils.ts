import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}


function formatNumber(num: number | undefined, compare: number[], units: string[]): { value: string, unit: string } {
  if (num === undefined) return { value: "0.00", unit: units[0] };
  else if (num >= compare[0]) return { value: (num / compare[0]).toFixed(2), unit: units[1] };
  else if (num >= compare[1]) return { value: (num / compare[1]).toFixed(2), unit: units[2] };
  else if (num >= compare[2]) return { value: (num / compare[2]).toFixed(2), unit: units[3] };
  else if (num >= compare[3]) return { value: (num / compare[3]).toFixed(2), unit: units[4] };
  else return { value: (num).toFixed(2), unit: units[5] };
}

export function formatCount(num: number | undefined): { raw: number, formatted: { value: string, unit: string } } {
  return {
    raw: num ?? 0,
    formatted: formatNumber(num, [1000000000, 1000000, 1000, 1], ['', 'B', 'M', 'K', '', '']),
  };
}

// 去掉小数末尾多余的 0：2.50 -> 2.5，3001.00 -> 3001
function trimDecimals(value: number, digits: number): string {
  return value.toFixed(digits).replace(/\.?0+$/, '');
}

/**
 * 中文习惯的计数单位：万 / 亿。
 * 不足 1000 显示原数字，1000 起走「0.X万」，1 亿起走「亿」。
 * 例：500 -> 500，5000 -> 0.5万，25840 -> 2.58万，1.5e8 -> 1.5亿
 */
export function formatCountCJK(num: number | undefined): { raw: number, formatted: { value: string, unit: string } } {
  const raw = num ?? 0;
  if (!Number.isFinite(raw)) return { raw: 0, formatted: { value: '0', unit: '' } };
  const abs = Math.abs(raw);
  if (abs >= 100000000) return { raw, formatted: { value: trimDecimals(raw / 100000000, 2), unit: '亿' } };
  if (abs >= 1000) return { raw, formatted: { value: trimDecimals(raw / 10000, 2), unit: '万' } };
  return { raw, formatted: { value: String(Math.round(raw)), unit: '' } };
}
export function formatMoney(num: number | undefined): { raw: number, formatted: { value: string, unit: string } } {
  return {
    raw: num ?? 0,
    formatted: formatNumber(num, [1000000000, 1000000, 1000, 1], ['$', 'B$', 'M$', 'K$', '$', '$']),
  };
}

export function formatTime(ms: number | undefined): { raw: number, formatted: { value: string, unit: string } } {
  return {
    raw: ms ?? 0,
    formatted: formatNumber(ms, [86400000, 3600000, 60000, 1000], ['', 'd', 'h', 'm', 's', 'ms']),
  };
}