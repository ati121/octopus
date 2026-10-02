import assert from 'node:assert/strict';
import test from 'node:test';
import { formatTokens } from '../src/lib/utils.ts';

test('Token 不足一万显示整数，达到一万和一亿才切换单位', () => {
    for (const [input, value, unit] of [
        [0, '0', ''], [1, '1', ''], [10, '10', ''], [99, '99', ''],
        [100, '100', ''], [1000, '1000', ''], [9999, '9999', ''],
        [10000, '1', '万'], [25840, '2.58', '万'],
        [100000000, '1', '亿'], [150000000, '1.5', '亿'], [1000000000, '10', '亿'],
    ]) {
        assert.deepEqual(formatTokens(input), { raw: input, formatted: { value, unit } });
    }
    assert.deepEqual(formatTokens(undefined).formatted, { value: '0', unit: '' });
    assert.deepEqual(formatTokens(NaN).formatted, { value: '0', unit: '' });
});
