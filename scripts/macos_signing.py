"""Use an explicitly selected existing macOS identity; never create one."""
from dataclasses import dataclass, replace
from datetime import datetime, timezone
import hashlib
import os
from pathlib import Path
import re
import ssl
import subprocess
import tempfile


class SigningError(ValueError):
    pass


def command(*arguments, capture=False, diagnostics=False):
    environment = dict(os.environ)
    for name in ('MYGO_UPDATER_PRIVATE_KEY', 'MACOS_SIGNING_CERTIFICATE',
                 'MACOS_SIGNING_CERTIFICATE_PASSWORD', 'MACOS_NOTARY_KEY_BASE64',
                 'MACOS_NOTARY_KEY_ID', 'MACOS_NOTARY_ISSUER'):
        environment.pop(name, None)
    environment['LC_ALL'] = 'C'
    result = subprocess.run(arguments, env=environment, text=True,
                            stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE)
    if result.returncode:
        raise SigningError(f'{Path(arguments[0]).name} failed ({result.returncode}): '
                           + (result.stderr or '').strip())
    return ((result.stdout or '') + ('\n'+(result.stderr or '') if diagnostics else '')).strip()


@dataclass(frozen=True)
class Signing:
    identity_hash: str
    name: str
    profile: str = ''
    keychain: str = ''
    certificate_sha256: str = ''
    not_before: str = ''
    not_after: str = ''
    signing_keychain: str = ''


def _certificate_date(value):
    """Parse OpenSSL's fixed English/GMT output without process locale changes."""
    match = re.fullmatch(r'([A-Z][a-z]{2})\s+(\d{1,2}) (\d{2}):(\d{2}):(\d{2}) (\d{4}) GMT', value)
    months = ('Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
              'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec')
    if match is None or match[1] not in months:
        raise SigningError('Cannot read the signing certificate validity period.')
    try:
        return datetime(int(match[6]), months.index(match[1]) + 1, int(match[2]),
                        int(match[3]), int(match[4]), int(match[5]), tzinfo=timezone.utc)
    except ValueError as error:
        raise SigningError('Cannot read the signing certificate validity period.') from error


def discover_identities(keychain=''):
    """Find key-paired identities and validate public leaf metadata, independent of trust."""
    scope = [keychain] if keychain else []
    listing = command('/usr/bin/security', 'find-identity', '-p', 'codesigning', *scope, capture=True)
    identities = {}
    for line in listing.splitlines():
        # security prints the label verbatim between quotes, including embedded
        # quotes or backslashes. The final quote precedes only a trust result.
        match = re.fullmatch(r'\s*\d+\)\s+([0-9A-Fa-f]{40})\s+"(.+)"(?:\s+\(([^()]*)\))?\s*', line)
        if match and 'CSSMERR_TP_CERT_REVOKED' not in (match[3] or ''):
            digest, name, _ = match.groups()
            identities.setdefault(digest.upper(), name)
    if not identities:
        return []
    # This exports only public certificates, never private keys or PKCS#12.
    exported = command('/usr/bin/security', 'find-certificate', '-a', '-p', *scope, capture=True)
    certificates = re.findall(r'-----BEGIN CERTIFICATE-----\s+.*?-----END CERTIFICATE-----',
                              exported, re.DOTALL)
    found = {}
    now = datetime.now(timezone.utc)
    with tempfile.TemporaryDirectory(prefix='picup-public-certificates-') as temporary:
        certificate_path = Path(temporary) / 'certificate.pem'
        for pem in certificates:
            try:
                der = ssl.PEM_cert_to_DER_cert(pem)
            except ValueError as error:
                raise SigningError('Cannot decode a public certificate from the keychain.') from error
            digest = hashlib.sha1(der).hexdigest().upper()
            if digest not in identities or digest in found:
                continue
            certificate_path.write_text(pem + '\n')
            metadata = command('/usr/bin/openssl', 'x509', '-in', str(certificate_path),
                               '-noout', '-dates', '-text', capture=True)
            dates = dict(re.findall(r'^(notBefore|notAfter)=(.+)$', metadata, re.MULTILINE))
            if set(dates) != {'notBefore', 'notAfter'}:
                raise SigningError('Cannot read the signing certificate validity period.')
            not_before = _certificate_date(dates['notBefore'])
            not_after = _certificate_date(dates['notAfter'])
            usage = re.search(r'^\s*X509v3 Extended Key Usage:[^\n]*\n([^\n]+)',
                              metadata, re.MULTILINE)
            purposes = set() if usage is None else {value.strip() for value in usage[1].split(',')}
            if (not not_before <= now <= not_after or
                    not purposes.intersection({'Code Signing', '1.3.6.1.5.5.7.3.3'})):
                continue
            found[digest] = Signing(digest, identities[digest],
                                    certificate_sha256=hashlib.sha256(der).hexdigest(),
                                    not_before=not_before.isoformat(), not_after=not_after.isoformat())
    return sorted(found.values(), key=lambda signing: (signing.name.casefold(), signing.identity_hash))


def configuration(environment=None):
    environment = os.environ if environment is None else environment
    identity = environment.get('PICUP_MACOS_SIGNING_IDENTITY', '').strip()
    profile = environment.get('PICUP_MACOS_NOTARY_PROFILE', '').strip()
    keychain = environment.get('PICUP_MACOS_NOTARY_KEYCHAIN', '').strip()
    signing_keychain = environment.get('PICUP_MACOS_SIGNING_KEYCHAIN', '').strip()
    pin = environment.get('PICUP_MACOS_CERTIFICATE_SHA256', '').strip().lower()
    if not identity:
        if profile or keychain or signing_keychain or pin:
            raise SigningError('Notary/profile/certificate pin requires PICUP_MACOS_SIGNING_IDENTITY.')
        if 'PICUP_MACOS_SIGNING_IDENTITY' in environment:
            raise SigningError('Explicit PICUP_MACOS_SIGNING_IDENTITY is empty.')
        return None
    if identity == '-':
        raise SigningError('Explicit signing requires a real identity; omit the variable for ad-hoc builds.')
    if pin and not re.fullmatch(r'[a-f0-9]{64}', pin):
        raise SigningError('PICUP_MACOS_CERTIFICATE_SHA256 must contain 64 hexadecimal characters.')
    available = discover_identities(signing_keychain) if signing_keychain else discover_identities()
    matches = [signing for signing in available
               if signing.identity_hash.lower() == identity.lower() or signing.name == identity]
    if len(matches) != 1:
        raise SigningError('Signing identity must match exactly one valid existing codesigning identity.')
    signing = matches[0]
    if pin and signing.certificate_sha256 != pin:
        raise SigningError('Signing certificate does not match its SHA-256 pin.')
    if profile and not signing.name.startswith('Developer ID Application:'):
        raise SigningError('Apple notarization requires a Developer ID Application identity.')
    if keychain and not profile:
        raise SigningError('PICUP_MACOS_NOTARY_KEYCHAIN requires PICUP_MACOS_NOTARY_PROFILE.')
    return replace(signing, profile=profile, keychain=keychain, signing_keychain=signing_keychain)


def verify_signed_code(target, signing, executable='Picup', is_cli=False):
    """Check the selected certificate and a stable identity for one code object."""
    target = Path(target)
    command('/usr/bin/codesign', '--verify', '--strict', str(target))
    with tempfile.TemporaryDirectory(prefix='picup-signature-') as temporary:
        prefix = str(Path(temporary) / 'certificate-')
        command('/usr/bin/codesign', '--display', '--extract-certificates='+prefix, str(target))
        certificate = Path(prefix + '0')
        if not certificate.is_file():
            raise SigningError(f'{target.name}: missing signing certificate.')
        der = certificate.read_bytes()
        if hashlib.sha1(der).hexdigest().upper() != signing.identity_hash:
            raise SigningError(f'{target.name}: signed by a different identity.')
        if signing.certificate_sha256 and hashlib.sha256(der).hexdigest() != signing.certificate_sha256:
            raise SigningError(f'{target.name}: certificate does not match its SHA-256 pin.')
        certificate_hashes = [signing.identity_hash]
        index = 1
        while Path(prefix + str(index)).is_file():
            certificate_hashes.append(hashlib.sha1(Path(prefix + str(index)).read_bytes()).hexdigest().upper())
            index += 1
        verify_designated_requirement(target, signing, executable, is_cli, certificate_hashes)


def sign_development_binaries(directory, signing):
    """Give direct Dev builds the same fixed code identifiers as packaged builds."""
    binaries = (
        # MyGo signs standalone resources using their basename as identifier.
        ('picup-dev', 'picup-dev', True),
        ('picup-gui', 'com.knothhe.picup.dev', False),
    )
    for name, identifier, is_cli in binaries:
        target = Path(directory) / 'bin' / name
        scope = ['--keychain', signing.signing_keychain] if signing.signing_keychain else []
        command('/usr/bin/codesign', '--force', '--sign', signing.identity_hash, *scope,
                '--identifier', identifier, str(target))
        verify_signed_code(target, signing, 'Picup Dev', is_cli)


def verify_application(app, signing, executable='Picup', cli='picup'):
    """Verify signature integrity and the selected certificate on all owned code."""
    app = Path(app)
    command('/usr/bin/codesign', '--verify', '--deep', '--strict', str(app))
    targets = [app, app / 'Contents/MacOS' / executable, app / 'Contents/Resources' / cli]
    for index, target in enumerate(targets):
        verify_signed_code(target, signing, executable, index == 2)
    if signing.profile:
        command('/usr/bin/xcrun', 'stapler', 'validate', str(app))
        command('/usr/sbin/spctl', '--assess', '--type', 'execute', str(app))


def _requirement_literal(value):
    """Match the Security framework's C-locale requirement dumper spelling."""
    encoded = value.encode('utf-8')
    if any(not (32 <= byte <= 126 or byte in b'\t\n\r\v\f') for byte in encoded):
        return '0x' + encoded.hex()
    return '"' + value.replace('\\', '\\\\').replace('"', '\\"') + '"'


def _normalize_requirement(value):
    # Ignore dumper annotations and formatting outside literals. Certificate
    # names can themselves contain spaces, escaped quotes, or comment markers.
    tokens = r'"(?:\\.|[^"\\])*"|(?:/\*.*?\*/|\s+)+'
    normalized = re.sub(tokens, lambda match: match[0] if match[0].startswith('"') else ' ',
                        value, flags=re.DOTALL).strip()
    # Security's dumper may omit quotes around simple identifiers such as
    # "picup". Canonicalize only that literal, keeping the complete predicate
    # comparison below strict (including all certificate constraints).
    return re.sub(r'^identifier ([A-Za-z0-9_.-]+)(?= and )',
                  lambda match: 'identifier "' + match[1] + '"', normalized)


def verify_designated_requirement(target,signing,executable,is_cli=False,certificate_hashes=None):
    metadata=command('/usr/bin/codesign','--display','--verbose=4',str(target),capture=True,diagnostics=True)
    identifiers=re.findall(r'^Identifier=(.+)$',metadata,re.MULTILINE)
    bundle_id='com.knothhe.picup.dev' if executable=='Picup Dev' else 'com.knothhe.picup'
    allowed={'a.out','picup-dev' if executable=='Picup Dev' else 'picup',bundle_id+'.cli'} if is_cli else {bundle_id}
    if len(identifiers)!=1 or identifiers[0] not in allowed:
        raise SigningError(f'{target.name}: unexpected signed code identifier.')
    identifier=identifiers[0]
    displayed=command('/usr/bin/codesign','--display','--requirements','-',str(target),capture=True,diagnostics=True)
    matches=re.findall(r'^(?:#\s*)?designated => (.+)$',displayed,re.MULTILINE)
    if len(matches)!=1:
        raise SigningError(f'{target.name}: missing or ambiguous designated requirement.')
    requirement=_normalize_requirement(matches[0])
    requirement=re.sub(r'H"([0-9a-fA-F]+)"',lambda match:'H"'+match[1].upper()+'"',requirement)
    prefix='identifier "'+identifier+'" and '
    if signing.name.startswith('Developer ID Application:'):
        team=re.search(r'\(([A-Z0-9]{10})\)$',signing.name)
        if not team:raise SigningError('Developer ID identity has no unambiguous Team ID.')
        expected={prefix+'anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] and '
                  'certificate leaf[field.1.2.840.113635.100.6.1.13] and certificate leaf[subject.OU] = "'+team[1]+'"'}
    elif signing.name.startswith(('Apple Development:', 'Mac Developer:')):
        # Apple's default development identity binds the code identifier and
        # certificate CN to its Worldwide Developer Relations issuer (TN3127).
        expected={prefix+'anchor apple generic and certificate leaf[subject.CN] = '+
                  _requirement_literal(signing.name)+' and certificate 1[field.1.2.840.113635.100.6.2.1]'}
    else:
        # Non-Apple default requirements may pin an intermediate or root when
        # its Organization matches the leaf. Accept only a certificate at its
        # actual position in this code object's verified, exported chain.
        chain = certificate_hashes or [signing.identity_hash]
        expected={prefix+f'certificate {index} = H"{digest}"'
                  for index,digest in enumerate(chain)}
        expected.update({prefix+f'certificate leaf = H"{chain[0]}"',
                         prefix+f'certificate root = H"{chain[-1]}"',
                         prefix+f'anchor H"{chain[-1]}"'})
    if requirement not in {_normalize_requirement(value) for value in expected}:
        raise SigningError(f'{target.name}: designated requirement is not the expected stable certificate identity.')
    command('/usr/bin/codesign','--verify','--strict','--test-requirement',
            f'=identifier "{identifier}" and certificate leaf = H"{signing.identity_hash}"',str(target))
