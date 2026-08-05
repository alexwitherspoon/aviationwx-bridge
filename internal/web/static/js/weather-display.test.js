import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    formatObservationAge,
    formatRawPayload,
    consumeSSEBuffer,
    discoverHostValue,
} from './weather-display.js';

test('formatObservationAge never when missing', () => {
    assert.equal(formatObservationAge(null), 'never');
    assert.equal(formatObservationAge(''), 'never');
});

test('formatObservationAge seconds', () => {
    const now = Date.parse('2026-08-02T12:00:30Z');
    assert.equal(formatObservationAge('2026-08-02T12:00:00Z', now), '30s ago');
});

test('formatObservationAge minutes', () => {
    const now = Date.parse('2026-08-02T12:05:00Z');
    assert.equal(formatObservationAge('2026-08-02T12:00:00Z', now), '5m ago');
});

test('formatRawPayload pretty JSON', () => {
    const out = formatRawPayload({ temp: 62.7, wind_speed_last: 10 });
    assert.match(out, /"temp": 62\.7/);
    assert.match(out, /"wind_speed_last": 10/);
});

test('formatRawPayload empty for null', () => {
    assert.equal(formatRawPayload(null), '');
});

test('consumeSSEBuffer parses complete frames and keeps remainder', () => {
    const { events, rest } = consumeSSEBuffer(
        'data: {"type":"phase","phase":"mdns"}\n\ndata: {"type":"candidate"}\n\ndata: {"typ'
    );
    assert.equal(events.length, 2);
    assert.equal(events[0].type, 'phase');
    assert.equal(events[1].type, 'candidate');
    assert.equal(rest, 'data: {"typ');
});

test('consumeSSEBuffer skips malformed JSON frames', () => {
    const { events } = consumeSSEBuffer('data: not-json\n\ndata: {"type":"done"}\n\n');
    assert.equal(events.length, 1);
    assert.equal(events[0].type, 'done');
});

test('discoverHostValue omits default port', () => {
    assert.equal(discoverHostValue({ host: 'wll.local', port: 80 }), 'wll.local');
    assert.equal(discoverHostValue({ host: 'wll.local', port: 8080 }), 'wll.local:8080');
});
