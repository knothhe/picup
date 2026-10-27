#!/usr/bin/env python3
"""Select and remember an existing local certificate for Picup Dev builds."""
import json
import os
from pathlib import Path
import re
import sys
import tempfile

import macos_signing


CACHE_FILENAME = '.picup-dev-signing.json'
SIGNING_VARIABLES = (
    'PICUP_MACOS_SIGNING_IDENTITY', 'PICUP_MACOS_NOTARY_PROFILE',
    'PICUP_MACOS_NOTARY_KEYCHAIN', 'PICUP_MACOS_CERTIFICATE_SHA256',
)


def identities():
    return macos_signing.discover_identities()


def _read_cache(path):
    try:
        data = json.loads(path.read_text())
        if (not isinstance(data, dict) or type(data.get('version')) is not int or
                data['version'] not in (1, 2) or
                not isinstance(data.get('identity_hash'), str) or
                not re.fullmatch(r'[0-9A-Fa-f]{40}', data['identity_hash']) or
                not isinstance(data.get('name'), str) or not data['name']):
            raise ValueError('invalid certificate selection')
        pin = data.get('certificate_sha256', '')
        if ((data['version'] == 2 or 'certificate_sha256' in data) and
                (not isinstance(pin, str) or not re.fullmatch(r'[0-9A-Fa-f]{64}', pin))):
            raise ValueError('invalid certificate SHA-256')
        return data['identity_hash'].upper(), pin.lower(), data['version']
    except FileNotFoundError:
        return None
    except (OSError, UnicodeError, ValueError) as error:
        raise macos_signing.SigningError(
            f'Cannot read Dev signing selection at {path}; run make dev-signing to select again.') from error


def _save_cache(path, signing):
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w', prefix='.picup-dev-signing-',
                                         suffix='.tmp', dir=path.parent, delete=False) as output:
            temporary = Path(output.name)
            os.chmod(temporary, 0o600)
            json.dump({'version': 2, 'identity_hash': signing.identity_hash,
                       'name': signing.name, 'certificate_sha256': signing.certificate_sha256},
                      output, indent=2)
            output.write('\n')
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def _choose(available):
    if not available:
        raise macos_signing.SigningError(
            'No valid macOS code-signing identity was found. In Keychain Access, choose '
            'Certificate Assistant > Create a Certificate; use Identity Type: Self Signed Root '
            'and Certificate Type: Code Signing. After creating the certificate, run make dev-signing.')
    if not sys.stdin.isatty():
        raise macos_signing.SigningError(
            'No Dev signing certificate is selected. Run make dev-signing in a terminal, '
            'or set PICUP_MACOS_SIGNING_IDENTITY explicitly.')
    print('Available certificates for Picup Dev:', file=sys.stderr)
    for number, signing in enumerate(available, 1):
        print(f'  {number}) {signing.name}\n     SHA-1: {signing.identity_hash}\n'
              f'     SHA-256: {signing.certificate_sha256}\n'
              f'     Valid from: {signing.not_before}\n     Valid until: {signing.not_after}',
              file=sys.stderr)
    try:
        while True:
            answer = input('Choose a certificate number (q to cancel): ').strip()
            if answer.lower() == 'q':
                raise macos_signing.SigningError('Dev signing selection cancelled; previous selection preserved.')
            if answer.isdecimal() and 1 <= int(answer) <= len(available):
                return available[int(answer) - 1]
            print(f'Enter a number from 1 to {len(available)}, or q to cancel.', file=sys.stderr)
    except (EOFError, KeyboardInterrupt) as error:
        raise macos_signing.SigningError(
            'Dev signing selection cancelled; previous selection preserved.') from error


def configuration(root, environment=None, reselect=False):
    """Return a validated Dev identity; explicit release settings take precedence."""
    environment = os.environ if environment is None else environment
    local = (sys.platform == 'darwin' and environment.get('GOOS', 'darwin') in ('', 'darwin') and
             environment.get('CI', '').strip().lower() in ('', '0', 'false', 'no', 'off'))
    explicit = ('PICUP_MACOS_SIGNING_IDENTITY' in environment or
                any(environment.get(name, '').strip() for name in SIGNING_VARIABLES[1:]))
    path = Path(root) / CACHE_FILENAME
    if explicit:
        signing = macos_signing.configuration(environment)
        if reselect and local and signing is not None:
            _save_cache(path, signing)
            print(f'Dev signing: saved {signing.name} ({signing.identity_hash}).', file=sys.stderr)
        return signing
    if not local:
        return None
    cached = None if reselect else _read_cache(path)
    available = identities()
    if cached:
        identity_hash, pin, version = cached
        matches = [signing for signing in available if signing.identity_hash == identity_hash]
        if len(matches) != 1:
            raise macos_signing.SigningError(
                'The saved Dev signing certificate is no longer available; restore its certificate '
                'and private key, or run make dev-signing to select again.')
        signing = matches[0]
        if pin and signing.certificate_sha256 != pin:
            raise macos_signing.SigningError(
                'The saved Dev signing certificate does not match its SHA-256 fingerprint; '
                'restore the selected certificate or run make dev-signing to select again.')
        if version == 1:
            _save_cache(path, signing)
        print(f'Dev signing: using {signing.name} ({signing.identity_hash}).', file=sys.stderr)
        return signing
    signing = _choose(available)
    _save_cache(path, signing)
    print(f'Dev signing: saved {signing.name} ({signing.identity_hash}).', file=sys.stderr)
    return signing


def main():
    try:
        signing = configuration(Path(__file__).resolve().parent.parent, reselect=True)
        if signing is None:
            raise macos_signing.SigningError('Local Dev certificate selection requires macOS outside CI.')
    except (macos_signing.SigningError, OSError) as error:
        print(f'dev-signing: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
