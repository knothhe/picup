#!/usr/bin/env python3
"""Preview or explicitly upload one existing macOS signing identity to GitHub."""
import argparse
import base64
import getpass
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import shutil
import ssl
import stat
import subprocess
import sys
import tempfile

import macos_signing
from release import github_repo, ReleaseError


ROOT = Path(__file__).resolve().parent.parent
CERTIFICATE_SECRET = 'MACOS_SIGNING_CERTIFICATE'
PASSWORD_SECRET = 'MACOS_SIGNING_CERTIFICATE_PASSWORD'
SECRET_NAMES = (CERTIFICATE_SECRET, PASSWORD_SECRET)
OPENSSL = '/usr/bin/openssl'


class SetupError(ValueError):
    pass


def run(arguments, *, input=None, environment=None):
    """Keep private stdin, command arguments and tool diagnostics out of logs."""
    child = {key: value for key, value in os.environ.items()
             if not key.startswith(('MACOS_', 'PICUP_MACOS_'))
             and key not in ('MYGO_UPDATER_PRIVATE_KEY', 'PICUP_P12_PASSWORD')}
    child['LC_ALL'] = 'C'
    if arguments[0] == 'gh':
        # repository() accepts github.com remotes, independent of GH_HOST.
        child['GH_HOST'] = 'github.com'
    if environment:
        child.update(environment)
    result = subprocess.run(list(map(str, arguments)), input=input, env=child,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        raise SetupError(f'{Path(arguments[0]).name} failed during signing setup '
                         f'({result.returncode}); private tool diagnostics were suppressed.')
    return result.stdout


def private_write(path, data):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(data)


def private_read(path, limit=1024 * 1024):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | getattr(os, 'O_NONBLOCK', 0))
    with os.fdopen(descriptor, 'rb') as stream:
        metadata = os.fstat(stream.fileno())
        if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.getuid()
                or stat.S_IMODE(metadata.st_mode) != 0o600):
            raise SetupError('Private input must be an owned regular file with mode 0600, without symlinks.')
        data = stream.read(limit + 1)
    if not data or len(data) > limit:
        raise SetupError('Private input is empty or exceeds its size limit.')
    return data


def read_pin(root):
    if (root / 'config').is_symlink():
        raise SetupError('Configuration directory must not be a symlink.')
    path = root / 'config/macos-signing.json'
    if not path.exists() and not path.is_symlink():
        return None
    if path.is_symlink():
        raise SetupError('The distribution pin must not be a symlink.')
    data = json.loads(path.read_text())
    if (not isinstance(data, dict) or set(data) != {'schema', 'certificateSHA256'}
            or type(data['schema']) is not int or data['schema'] != 1
            or not isinstance(data['certificateSHA256'], str)
            or not re.fullmatch('[a-f0-9]{64}', data['certificateSHA256'])):
        raise SetupError('Invalid distribution pin; expected only schema: 1 and a lowercase certificateSHA256.')
    return data['certificateSHA256']


def repository(root, requested=None):
    remotes = run(['git', '-C', root, 'remote', 'get-url', '--push', '--all', 'origin']).decode().splitlines()
    if len(remotes) != 1:
        raise SetupError('origin must have exactly one push URL before configuring signing Secrets.')
    selected = github_repo(remotes[0])
    if requested and requested != selected:
        raise SetupError('Requested repository differs from the origin push repository.')
    return selected


def choose_identity(identities, requested, pin):
    if requested:
        matches = [item for item in identities
                   if item.identity_hash.lower() == requested.lower() or item.name == requested]
        if len(matches) != 1:
            raise SetupError('--identity must match exactly one valid existing code-signing identity.')
        return matches[0]
    matches = [item for item in identities if item.certificate_sha256 == pin]
    if len(matches) == 1:
        return matches[0]
    if not identities:
        raise SetupError('No valid code-signing identity is available. Restore an existing identity, '
                         'or follow docs/macos-distribution.md to create a local certificate once.')
    for index, item in enumerate(identities, 1):
        print(f'{index}) {item.name}\n   SHA-1: {item.identity_hash}\n'
              f'   SHA-256: {item.certificate_sha256}\n   Expires: {item.not_after}')
    if not sys.stdin.isatty():
        raise SetupError('Select an identity explicitly with --identity; no private material was exported.')
    try:
        index = int(input('Select the existing release identity (empty cancels): '))
    except (ValueError, EOFError):
        raise SetupError('Identity selection cancelled.') from None
    if not 1 <= index <= len(identities):
        raise SetupError('Invalid identity selection.')
    return identities[index - 1]


def source_keychain(signing, explicit=None):
    if explicit:
        return str(Path(explicit).expanduser().resolve())
    listing = run(['/usr/bin/security', 'list-keychains', '-d', 'user']).decode()
    matches = []
    for keychain in shlex.split(listing):
        found = macos_signing.discover_identities(keychain)
        if any(item.identity_hash == signing.identity_hash for item in found):
            matches.append(keychain)
    if len(matches) != 1:
        raise SetupError('The identity must belong to one source keychain; specify --keychain explicitly.')
    return matches[0]


def single_identity_package(source, password, signing, directory):
    """Select by DER fingerprint, pair by SPKI, and discard unrelated identities."""
    environment = {'PICUP_P12_PASSWORD': password}
    decrypted = directory / 'export.pem'
    private_write(decrypted, b'')
    run([OPENSSL, 'pkcs12', '-in', source, '-passin', 'env:PICUP_P12_PASSWORD',
         '-nodes', '-out', decrypted], environment=environment)
    content = decrypted.read_text()
    certificates = {}
    for pem in re.findall(r'-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----', content, re.S):
        der = ssl.PEM_cert_to_DER_cert(pem)
        certificates[hashlib.sha256(der).hexdigest()] = pem
    pem = certificates.get(signing.certificate_sha256)
    if pem is None:
        raise SetupError('Exported certificate does not match the selected SHA-256 fingerprint.')
    leaf = directory / 'leaf.pem'
    private_write(leaf, (pem + '\n').encode())
    public_key = run([OPENSSL, 'x509', '-in', leaf, '-noout', '-pubkey']).strip()
    matching = set()
    candidate = directory / 'candidate.key'
    private_write(candidate, b'')
    blocks = re.finditer(r'-----BEGIN ((?:RSA |EC )?PRIVATE KEY)-----.*?-----END \1-----', content, re.S)
    for match in blocks:
        key = match[0] + '\n'
        candidate.write_text(key)
        if run([OPENSSL, 'pkey', '-in', candidate, '-pubout']).strip() == public_key:
            matching.add(key)
    if len(matching) != 1:
        raise SetupError('Export must contain exactly one private key matching the selected certificate.')
    selected_key = directory / 'selected.key'
    private_write(selected_key, matching.pop().encode())
    # Carry only the selected certificate's issuer chain, never other identities.
    chain = []
    current = pem
    remaining = dict(certificates)
    while True:
        metadata = run([OPENSSL, 'x509', '-in', leaf, '-noout', '-subject', '-issuer',
                        '-nameopt', 'RFC2253']).decode()
        subject, issuer = [line.split('=', 1)[1].strip() for line in metadata.splitlines()]
        if subject == issuer:
            break
        remaining.pop(hashlib.sha256(ssl.PEM_cert_to_DER_cert(current)).hexdigest(), None)
        issuers = []
        for value in remaining.values():
            candidate.write_text(value + '\n')
            name = run([OPENSSL, 'x509', '-in', candidate, '-noout', '-subject',
                        '-nameopt', 'RFC2253']).decode().split('=', 1)[1].strip()
            if name == issuer:
                issuers.append(value)
        if len(issuers) > 1:
            raise SetupError('Certificate issuer chain is ambiguous; supply an existing single-identity P12.')
        if not issuers:
            break
        current = issuers[0]
        chain.append(current)
        leaf.write_text(current + '\n')
    leaf.write_text(pem + '\n')
    password = secrets.token_urlsafe(32)
    package = directory / 'identity.p12'
    private_write(package, b'')
    arguments = [OPENSSL, 'pkcs12', '-export', '-in', leaf, '-inkey', selected_key,
                 '-name', signing.name, '-out', package, '-passout', 'env:PICUP_P12_PASSWORD',
                 '-keypbe', 'PBE-SHA1-3DES', '-certpbe', 'PBE-SHA1-3DES', '-macalg', 'sha1']
    if chain:
        chainfile = directory / 'chain.pem'
        private_write(chainfile, ('\n'.join(chain) + '\n').encode())
        arguments.extend(['-certfile', chainfile])
    run(arguments, environment={'PICUP_P12_PASSWORD': password})
    return package, password


def validate_import(package, password, signing, directory):
    """Test the macOS importer and restore create-keychain's search-list change."""
    original = shlex.split(run(['/usr/bin/security', 'list-keychains', '-d', 'user']).decode())
    keychain = directory / 'validation.keychain-db'
    keychain_password = secrets.token_urlsafe(32)
    created = False
    cleanup_errors = []
    try:
        run(['/usr/bin/security', 'create-keychain', '-p', keychain_password, keychain])
        created = True
        run(['/usr/bin/security', 'unlock-keychain', '-p', keychain_password, keychain])
        run(['/usr/bin/security', 'import', package, '-k', keychain, '-f', 'pkcs12',
             '-P', password, '-T', '/usr/bin/codesign'])
        identities = macos_signing.discover_identities(str(keychain))
        if (len(identities) != 1 or identities[0].identity_hash != signing.identity_hash
                or identities[0].certificate_sha256 != signing.certificate_sha256):
            raise SetupError('Imported P12 does not contain the selected unique, valid signing identity.')
    finally:
        try:
            run(['/usr/bin/security', 'list-keychains', '-d', 'user', '-s', *original])
        except (SetupError, OSError):
            cleanup_errors.append('restore keychain search list')
        if created or keychain.exists():
            try:
                run(['/usr/bin/security', 'delete-keychain', keychain])
            except (SetupError, OSError):
                cleanup_errors.append('delete validation keychain')
        if cleanup_errors:
            raise SetupError('Signing validation cleanup failed: ' + ', '.join(cleanup_errors))


def save_backup(root, package, password, signing, backup_root=None):
    base = Path(backup_root).expanduser() if backup_root else (
        Path.home() / 'Library/Application Support/picup-release/macos-signing')
    if base.resolve().is_relative_to(root.resolve()):
        raise SetupError('Private backups must be outside the repository.')
    base.mkdir(mode=0o700, parents=True, exist_ok=True)
    if base.is_symlink() or base.stat().st_uid != os.getuid() or stat.S_IMODE(base.stat().st_mode) != 0o700:
        raise SetupError('Backup directory must be owned, mode 0700 and not a symlink.')
    destination = Path(tempfile.mkdtemp(prefix=signing.certificate_sha256[:12] + '-', dir=base))
    try:
        private_write(destination / 'identity.p12', package.read_bytes())
        private_write(destination / 'password', password.encode())
    except BaseException:
        shutil.rmtree(destination)
        raise
    return destination


def write_pin(root, pin):
    directory = root / 'config'
    if directory.is_symlink():
        raise SetupError('Configuration directory must not be a symlink.')
    directory.mkdir(exist_ok=True)
    data = json.dumps({'schema': 1, 'certificateSHA256': pin}, indent=2) + '\n'
    with tempfile.NamedTemporaryFile(mode='w', prefix='.macos-signing-', dir=directory, delete=False) as stream:
        temporary = Path(stream.name)
        stream.write(data)
    try:
        os.replace(temporary, directory / 'macos-signing.json')
    finally:
        temporary.unlink(missing_ok=True)


def configure(args, root=ROOT):
    repo = repository(root, args.repo)
    pin = read_pin(root)
    entries = json.loads(run(['gh', 'secret', 'list', '--repo', repo, '--json', 'name,updatedAt']))
    present = {item['name'] for item in entries}
    identities = (macos_signing.discover_identities(str(Path(args.keychain).expanduser().resolve()))
                  if args.keychain else macos_signing.discover_identities())
    signing = choose_identity(identities, args.identity, pin)
    print(f'Repository: {repo}\nIdentity: {signing.name}\nSHA-1: {signing.identity_hash}\n'
          f'SHA-256: {signing.certificate_sha256}\nExpires: {signing.not_after}\n'
          f'Current distribution pin: {pin or "not configured"}')
    for name in SECRET_NAMES:
        print(f'{name}: {"present (value cannot be read)" if name in present else "missing"}')
    replacement = ((pin is not None and pin != signing.certificate_sha256)
                   or (pin is None and bool(present.intersection(SECRET_NAMES))))
    if replacement:
        print('This changes or replaces an unverified release identity; --replace-identity is required.')
    if not args.apply:
        print('Preview only. Add --apply to export, validate, back up, upload both Secrets and write the public pin.')
        return
    if replacement and not args.replace_identity:
        raise SetupError('Review the fingerprints above and use --replace-identity to explicitly replace the release identity.')
    if args.p12:
        if args.password_file:
            password = private_read(Path(args.password_file).expanduser(), 4096).decode().rstrip('\r\n')
        elif sys.stdin.isatty():
            password = getpass.getpass('Existing P12 password: ')
        else:
            raise SetupError('Existing P12 input needs --password-file outside an interactive terminal.')
        source_bytes = private_read(Path(args.p12).expanduser())
    elif args.password_file:
        raise SetupError('--password-file requires --p12.')
    with tempfile.TemporaryDirectory(prefix='picup-release-signing-') as temporary:
        directory = Path(temporary)
        directory.chmod(0o700)
        source = directory / 'export.p12'
        private_write(source, b'')
        if args.p12:
            source.write_bytes(source_bytes)
        else:
            password = secrets.token_urlsafe(32)
            keychain = source_keychain(signing, args.keychain)
            print('Exporting the existing identity; macOS may request keychain authorization.')
            run(['/usr/bin/security', 'export', '-k', keychain, '-t', 'identities', '-f', 'pkcs12',
                 '-P', password, '-o', source])
            source.chmod(0o600)
        package, password = single_identity_package(source, password, signing, directory)
        validate_import(package, password, signing, directory)
        encoded = base64.b64encode(package.read_bytes())
        if len(encoded) > 48 * 1024:
            raise SetupError('The selected P12 exceeds the GitHub Secret size limit after Base64 encoding.')
        backup = save_backup(root, package, password, signing, args.backup_dir)
        print(f'Validated private backup: {backup}')
        uploaded = []
        try:
            for name, value in ((PASSWORD_SECRET, password.encode()),
                                (CERTIFICATE_SECRET, encoded)):
                run(['gh', 'secret', 'set', name, '--repo', repo], input=value)
                uploaded.append(name)
            write_pin(root, signing.certificate_sha256)
        except (SetupError, OSError):
            raise SetupError(f'Setup incomplete. Uploaded: {", ".join(uploaded) or "none"}. '
                             f'Private backup retained at {backup}. The public pin was not updated; '
                             'retry the same identity before running signing CI.') from None
    print('Both Secrets uploaded and config/macos-signing.json written. '
          'Commit the public pin before running signing CI. Secret values still require CI import and final signature verification.')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--identity', help='Exact existing certificate name or SHA-1 identity hash')
    parser.add_argument('--repo', help='GitHub owner/repo; must match origin push URL')
    parser.add_argument('--keychain', help='Limit discovery and export to this existing keychain')
    parser.add_argument('--p12', help='Use an existing private P12 instead of exporting the keychain')
    parser.add_argument('--password-file', help='Owned mode-0600 password file for --p12')
    parser.add_argument('--backup-dir', help='Private mode-0700 backup directory outside the repository')
    parser.add_argument('--apply', action='store_true', help='Export and upload after the preview checks')
    parser.add_argument('--replace-identity', action='store_true', help='Explicitly approve a changed or previously unknown release identity')
    args = parser.parse_args(argv)
    try:
        if sys.platform != 'darwin':
            raise SetupError('Release identity setup requires macOS.')
        configure(args)
    except (SetupError, macos_signing.SigningError, ReleaseError, ValueError, OSError) as error:
        print(f'release-signing: {error}', file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print('release-signing: cancelled; temporary private files were cleaned up.', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
