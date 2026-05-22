/**
 * Unit tests for web-api.js
 */
import test from 'node:test';
import assert from 'node:assert';
import { shouldRetryAuth } from './web-api.js';

test('shouldRetryAuth is true for 401 on first attempt', () => {
    assert.strictEqual(shouldRetryAuth(401, false), true);
});

test('shouldRetryAuth is false after retry', () => {
    assert.strictEqual(shouldRetryAuth(401, true), false);
});

test('shouldRetryAuth is false for other status codes', () => {
    assert.strictEqual(shouldRetryAuth(403, false), false);
    assert.strictEqual(shouldRetryAuth(200, false), false);
});
