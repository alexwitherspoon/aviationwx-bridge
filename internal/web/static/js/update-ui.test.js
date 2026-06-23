/**
 * Unit tests for update-ui.js
 */
import test from 'node:test';
import assert from 'node:assert';
import {
    canApplyUpdateFromUI,
    updateBannerLabel,
    manualUpdateConfirmMessage,
    selfUpdateConfirmMessage,
} from './update-ui.js';

test('canApplyUpdateFromUI is true only when enabled', () => {
    assert.strictEqual(canApplyUpdateFromUI(true), true);
    assert.strictEqual(canApplyUpdateFromUI(false), false);
    assert.strictEqual(canApplyUpdateFromUI(undefined), false);
});

test('updateBannerLabel differs by deployment mode', () => {
    assert.strictEqual(updateBannerLabel('2.10.0', true), 'Update to 2.10.0');
    assert.strictEqual(updateBannerLabel('2.10.0', false), '2.10.0 available');
});

test('manualUpdateConfirmMessage includes release URL', () => {
    const msg = manualUpdateConfirmMessage({
        latest_version: '2.10.0',
        latest_url: 'https://example.com/release',
    });
    assert.match(msg, /2\.10\.0/);
    assert.match(msg, /Self-update is disabled/);
    assert.match(msg, /https:\/\/example\.com\/release/);
});

test('selfUpdateConfirmMessage describes supervisor apply', () => {
    const msg = selfUpdateConfirmMessage({
        current_version: '2.9.0',
        latest_version: '2.10.0',
    });
    assert.match(msg, /2\.9\.0/);
    assert.match(msg, /2\.10\.0/);
    assert.match(msg, /supervisor/);
});
