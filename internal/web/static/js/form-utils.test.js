/**
 * Unit tests for form-utils.js using Node built-in test runner.
 * Run: node --test internal/web/static/js/form-utils.test.js
 */
import test from 'node:test';
import assert from 'node:assert';
import {
    buildCameraConfigFromFormValues,
    uploadCredentialKey,
    findConflictingCameraId,
    slugCameraIdFromName,
    encodeBasicAuthHeader,
} from './form-utils.js';

test('buildCameraConfigFromFormValues returns null when type is missing', () => {
    assert.strictEqual(buildCameraConfigFromFormValues({}), null);
    assert.strictEqual(buildCameraConfigFromFormValues({ type: '' }), null);
});

test('buildCameraConfigFromFormValues returns null when type is unknown', () => {
    assert.strictEqual(buildCameraConfigFromFormValues({ type: 'unknown' }), null);
});

test('buildCameraConfigFromFormValues builds HTTP camera config with required fields', () => {
    const result = buildCameraConfigFromFormValues({
        type: 'http',
        snapshot_url: 'http://example.com/snap.jpg',
    });
    assert.deepStrictEqual(result, {
        id: 'test',
        type: 'http',
        snapshot_url: 'http://example.com/snap.jpg',
    });
});

test('buildCameraConfigFromFormValues builds HTTP camera config with auth', () => {
    const result = buildCameraConfigFromFormValues({
        type: 'http',
        id: 'cam-1',
        snapshot_url: 'http://example.com/snap.jpg',
        auth_user: 'admin',
        auth_pass: 'secret',
    });
    assert.deepStrictEqual(result, {
        id: 'cam-1',
        type: 'http',
        snapshot_url: 'http://example.com/snap.jpg',
        auth: { type: 'basic', username: 'admin', password: 'secret' },
    });
});

test('buildCameraConfigFromFormValues returns null for HTTP when snapshot_url is missing', () => {
    assert.strictEqual(buildCameraConfigFromFormValues({ type: 'http' }), null);
    assert.strictEqual(buildCameraConfigFromFormValues({ type: 'http', snapshot_url: '' }), null);
});

test('buildCameraConfigFromFormValues normalizes id: lowercase and replace spaces with hyphens', () => {
    const result = buildCameraConfigFromFormValues({
        type: 'http',
        id: 'My Camera 1',
        snapshot_url: 'http://example.com/snap.jpg',
    });
    assert.strictEqual(result.id, 'my-camera-1');
});

test('buildCameraConfigFromFormValues defaults id to "test" when missing', () => {
    const result = buildCameraConfigFromFormValues({
        type: 'http',
        snapshot_url: 'http://example.com/snap.jpg',
    });
    assert.strictEqual(result.id, 'test');
});

test('buildCameraConfigFromFormValues builds RTSP camera config', () => {
    const result = buildCameraConfigFromFormValues({
        type: 'rtsp',
        rtsp_url: 'rtsp://192.168.1.100/stream',
        rtsp_user: 'user',
        rtsp_pass: 'pass',
    });
    assert.deepStrictEqual(result, {
        id: 'test',
        type: 'rtsp',
        rtsp: {
            url: 'rtsp://192.168.1.100/stream',
            username: 'user',
            password: 'pass',
        },
    });
});

test('buildCameraConfigFromFormValues returns null for RTSP when rtsp_url is missing', () => {
    assert.strictEqual(buildCameraConfigFromFormValues({ type: 'rtsp' }), null);
});

test('buildCameraConfigFromFormValues builds ONVIF camera config', () => {
    const result = buildCameraConfigFromFormValues({
        type: 'onvif',
        onvif_endpoint: 'http://192.168.1.100/onvif',
        onvif_user: 'admin',
        onvif_pass: 'secret',
        onvif_profile: 'Profile_1',
    });
    assert.deepStrictEqual(result, {
        id: 'test',
        type: 'onvif',
        onvif: {
            endpoint: 'http://192.168.1.100/onvif',
            username: 'admin',
            password: 'secret',
            profile_token: 'Profile_1',
        },
    });
});

test('buildCameraConfigFromFormValues returns null for ONVIF when onvif_endpoint is missing', () => {
    assert.strictEqual(buildCameraConfigFromFormValues({ type: 'onvif' }), null);
});

test('buildCameraConfigFromFormValues omits profile_token when empty', () => {
    const result = buildCameraConfigFromFormValues({
        type: 'onvif',
        onvif_endpoint: 'http://192.168.1.100/onvif',
    });
    assert.strictEqual(result.onvif.profile_token, undefined);
});

test('slugCameraIdFromName matches server rules', () => {
    assert.strictEqual(slugCameraIdFromName('KORD West Camera'), 'kord-west-camera');
    assert.strictEqual(slugCameraIdFromName('Test_Camera'), 'test-camera');
    assert.strictEqual(slugCameraIdFromName('a  -  b'), 'a-b');
    assert.strictEqual(slugCameraIdFromName(''), '');
});

test('uploadCredentialKey matches server normalization', () => {
    assert.strictEqual(
        uploadCredentialKey('  Upload.AVIATIONWX.org ', 2222, '  cam-a  '),
        'upload.aviationwx.org:2222:cam-a',
    );
    assert.strictEqual(uploadCredentialKey('h', 0, 'u'), 'h:2222:u');
    assert.strictEqual(uploadCredentialKey('', 2222, 'u'), '');
});

test('findConflictingCameraId detects duplicate SFTP identity', () => {
    const list = [
        { id: 'a', upload: { host: 'upload.aviationwx.org', port: 2222, username: 'u1' } },
        { id: 'b', upload: { host: 'upload.aviationwx.org', port: 2222, username: 'u2' } },
    ];
    assert.strictEqual(
        findConflictingCameraId(list, null, 'upload.aviationwx.org', 2222, 'u1'),
        'a',
    );
    assert.strictEqual(
        findConflictingCameraId(list, 'a', 'upload.aviationwx.org', 2222, 'u1'),
        null,
    );
    assert.strictEqual(
        findConflictingCameraId(list, null, 'upload.aviationwx.org', 2222, 'u3'),
        null,
    );
});

test('encodeBasicAuthHeader encodes UTF-8 passwords', () => {
    const header = encodeBasicAuthHeader('admin', 'пароль');
    assert.ok(header.startsWith('Basic '));
    const decoded = Buffer.from(header.slice(6), 'base64').toString('utf8');
    assert.strictEqual(decoded, 'admin:пароль');
});
