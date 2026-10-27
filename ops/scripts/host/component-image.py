#!/usr/bin/env python3
"""Read-only candidate-image preflight and exact local pin verification."""
import json
import re
import subprocess
import sys


def run(args):
    return subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                          text=True, timeout=45, check=False)


def verify(image, data, allow_unpinned=False):
    image_id = data['Id']
    assert re.fullmatch(r'sha256:[0-9a-f]{64}', image_id), 'invalid local image ID'
    pin = image.rsplit('@', 1)[-1] if '@' in image else ''
    if pin:
        assert re.fullmatch(r'sha256:[0-9a-f]{64}', pin), 'invalid image pin'
        assert pin == image_id or any(ref.endswith('@' + pin) for ref in data.get('RepoDigests') or []), 'local image does not match pinned digest'
    else:
        assert allow_unpinned, 'registry images must pin a sha256 digest'
    return {'image': image, 'image_id': image_id, 'source': 'local', 'digest_verified': bool(pin)}


def inspect(runtime, image, mode, allow_unpinned=False):
    assert allow_unpinned or not image.startswith(('rollback-', 'localhost/')), 'preloaded image requires pull: false'
    local = run([runtime, 'image', 'inspect', image, '--format={{json .}}'])
    if local.returncode == 0:
        return verify(image, json.loads(local.stdout), allow_unpinned)
    assert mode == 'plan' and not allow_unpinned, 'candidate image is not present locally'
    assert re.search(r'@sha256:[0-9a-f]{64}$', image), 'registry images must pin a sha256 digest'
    remote = run([runtime, 'manifest', 'inspect', image])
    assert remote.returncode == 0, 'candidate image is neither present nor pullable'
    manifest = json.loads(remote.stdout)
    assert manifest.get('schemaVersion') == 2 and ('manifests' in manifest or 'config' in manifest), 'invalid registry manifest'
    return {'image': image, 'image_id': None, 'source': 'registry_manifest', 'digest_verified': True}


if __name__ == '__main__':
    try:
        runtime, image, mode, preload = sys.argv[1:]
        assert mode in ('plan', 'local')
        print(json.dumps(inspect(runtime, image, mode, preload == 'true')))
    except (AssertionError, KeyError, ValueError, subprocess.SubprocessError) as exc:
        sys.exit(str(exc) if isinstance(exc, AssertionError) else 'candidate image verification failed')
