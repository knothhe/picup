"""Derive channel packaging configuration without rewriting the version source."""
from contextlib import contextmanager
import hashlib
import json
from pathlib import Path
import tempfile


def development_manifest(root):
    root = Path(root).resolve()
    data = json.loads((root / 'mygo.json').read_text())
    data.update(name='Picup Dev', identifier='com.knothhe.picup.dev',
                out=str(root / 'build/dev/dist'), icon=str(root / 'assets/dev/icon-dev.png'),
                resources=[str(root / 'build/dev/bin/picup-dev'),
                           str(root / 'build/dev/THIRD_PARTY_NOTICES.txt')])
    data.pop('updates', None)
    data['macos'].pop('notarize', None)
    data['macos'].pop('signingIdentity', None)
    data['macos'].setdefault('infoPlist', {})['PicupDevMaintenanceProtocol'] = '1'
    data['main'] = str(root / 'cmd/picup-gui')
    return data


def verify_development_icon(root):
    """Normal builds consume checked-in output and need no Pillow installation."""
    root = Path(root)
    directory = root / 'assets/dev'
    manifest = json.loads((directory / '.dev-icon-manifest.json').read_text())
    sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
    if (manifest.get('schema') != 1 or manifest.get('formats') != ['png'] or
            manifest.get('name') != 'icon-dev' or
            manifest.get('png_sizes') != [32, 64, 128, 256, 512, 1024]):
        raise ValueError('Unexpected Dev icon generation parameters; run make icons-dev.')
    expected_files={'icon-dev.png',*(f'icon-dev-{size}.png' for size in (32,64,128,256,512,1024))}
    if set(manifest.get('files',{}))!=expected_files:
        raise ValueError('Dev icon output hashes are incomplete; run make icons-dev.')
    if manifest.get('source_sha256') != sha(root / 'assets/icon.png'):
        raise ValueError('Dev icon source changed; run make icons-dev with Pillow 10.1+.')
    if manifest.get('generator_sha256') != sha(root / 'scripts/dev_icon.py'):
        raise ValueError('Dev icon generator changed; run make icons-dev.')
    for filename, digest in manifest.get('files', {}).items():
        if Path(filename).name != filename or sha(directory / filename) != digest:
            raise ValueError('Dev icon output changed; regenerate using make icons-dev.')
    if not manifest.get('files'):
        raise ValueError('Dev icon manifest has no output hashes.')


@contextmanager
def packaging_configuration(root, development=False, signing=None):
    root = Path(root).resolve()
    if not development and not signing:
        yield root
        return
    if development:
        verify_development_icon(root)
        data = development_manifest(root)
    else:
        data = json.loads((root / 'mygo.json').read_text())
        for field in ('main', 'icon', 'out'):
            if field in data:
                data[field] = str(root / data[field])
        data['resources'] = [str(root / path) for path in data.get('resources', [])]
        if data.get('updates'):
            changelog = data['updates'].get('changelog', 'CHANGELOG.md')
            if (root / changelog).is_file():
                data['updates']['changelog'] = str(root / changelog)
    if signing:
        data['macos']['signingIdentity'] = signing.identity_hash
        if signing.profile:
            data['macos']['notarize'] = {'keychainProfile': signing.profile}
            if signing.keychain:
                data['macos']['notarize']['keychain'] = signing.keychain
    directory = root / 'build'
    directory.mkdir(exist_ok=True)
    # A directory inside the original module lets MyGo compile the original
    # packages and read their build metadata without copying or editing sources.
    with tempfile.TemporaryDirectory(prefix='.picup-package-', dir=directory) as temporary:
        path = Path(temporary)
        (path / 'mygo.json').write_text(json.dumps(data, indent=2) + '\n')
        yield path
