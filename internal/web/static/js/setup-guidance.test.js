import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    selectConfigBanners,
    firstIncompleteWizardStep,
    isFirstRunSetup,
} from './setup-guidance.js';

test('weather without API key yields strong banner', () => {
    const banners = selectConfigBanners({
        config: { api: { enabled: false } },
        status: {},
        stations: [{ id: 'st-1', enabled: true }],
        cameras: [],
    });
    assert.equal(banners.some((b) => b.id === 'weather_no_key'), true);
    assert.equal(banners.find((b) => b.id === 'weather_no_key').tone, 'strong');
});

test('API down yields link failed banner', () => {
    const banners = selectConfigBanners({
        config: { api: { enabled: true, key_set: true } },
        status: { api_link: { configured: true, status: 'down', last_error: '401' } },
        stations: [],
        cameras: [],
    });
    assert.equal(banners.some((b) => b.id === 'api_link_failed'), true);
});

test('key OK with no sources yields soft hint', () => {
    const banners = selectConfigBanners({
        config: { api: { enabled: true, key_set: true } },
        status: { api_link: { configured: true, status: 'operational' } },
        stations: [],
        cameras: [],
    });
    assert.equal(banners.some((b) => b.id === 'no_sources'), true);
});

test('pending enable when station not in bootstrap enabled_sources', () => {
    const banners = selectConfigBanners({
        config: { api: { enabled: true, key_set: true } },
        status: {
            api_link: {
                configured: true,
                status: 'operational',
                enabled_sources: [{ kind: 'weather', bridge_source_id: 'other', enabled: true }],
            },
        },
        stations: [{ id: 'station-davis', enabled: true }],
        cameras: [],
    });
    assert.equal(banners.some((b) => b.id === 'pending_enable'), true);
});

test('firstIncompleteWizardStep prefers api then cameras then weather', () => {
    assert.equal(firstIncompleteWizardStep({ config: {}, cameras: [], stations: [] }), 'api');
    assert.equal(
        firstIncompleteWizardStep({
            config: { api: { enabled: true, key_set: true } },
            cameras: [],
            stations: [],
        }),
        'cameras'
    );
    assert.equal(
        firstIncompleteWizardStep({
            config: { api: { enabled: true, key_set: true } },
            cameras: [{ id: 'c1' }],
            stations: [],
        }),
        'weather'
    );
    assert.equal(
        firstIncompleteWizardStep({
            config: { api: { enabled: true, key_set: true } },
            cameras: [{ id: 'c1' }],
            stations: [{ id: 's1' }],
        }),
        'done'
    );
});

test('isFirstRunSetup uses first_run or empty inventories', () => {
    assert.equal(isFirstRunSetup({ first_run: true }), true);
    assert.equal(isFirstRunSetup({ first_run: false, total_cameras: 0, total_stations: 0 }), false);
    assert.equal(isFirstRunSetup({ total_cameras: 0, total_stations: 0 }), true);
    assert.equal(isFirstRunSetup({ total_cameras: 1, total_stations: 0 }), false);
});
