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
    releaseNotesURL,
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

test('releaseNotesURL uses latest_url when present', () => {
    assert.strictEqual(
        releaseNotesURL({ latest_url: 'https://example.com/release' }),
        'https://example.com/release',
    );
});

test('releaseNotesURL falls back when latest_url missing or empty', () => {
    assert.match(releaseNotesURL({}), /github\.com\/alexwitherspoon\/aviationwx\.org-bridge\/releases/);
    assert.match(releaseNotesURL({ latest_url: '' }), /github\.com\/alexwitherspoon\/aviationwx\.org-bridge\/releases/);
    assert.match(releaseNotesURL({ latest_url: '   ' }), /github\.com\/alexwitherspoon\/aviationwx\.org-bridge\/releases/);
});

test('releaseNotesURL rejects non-http(s) schemes', () => {
    assert.match(
        releaseNotesURL({ latest_url: 'javascript:alert(1)' }),
        /github\.com\/alexwitherspoon\/aviationwx\.org-bridge\/releases/,
    );
    assert.match(
        releaseNotesURL({ latest_url: 'ftp://example.com/release' }),
        /github\.com\/alexwitherspoon\/aviationwx\.org-bridge\/releases/,
    );
    assert.match(
        releaseNotesURL({ latest_url: 'not-a-url' }),
        /github\.com\/alexwitherspoon\/aviationwx\.org-bridge\/releases/,
    );
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
