import assert from 'node:assert/strict';
import test from 'node:test';
import { formatTokens } from '../src/lib/utils.ts';

test('Token 始终使用万或亿，小用量不被归零', () => {
    for (const [input, value, unit] of [
        [0, '0', '万'], [1, '0.0001', '万'], [99, '0.0099', '万'],
        [9999, '0.9999', '万'], [10000, '1', '万'], [25840, '2.58', '万'],
        [100000000, '1', '亿'], [150000000, '1.5', '亿'], [1000000000, '10', '亿'],
    ]) {
        assert.deepEqual(formatTokens(input), { raw: input, formatted: { value, unit } });
    }
    assert.deepEqual(formatTokens(undefined).formatted, { value: '0', unit: '万' });
    assert.deepEqual(formatTokens(NaN).formatted, { value: '0', unit: '万' });
});
