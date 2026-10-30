"""Writes a release the way asc's release process does (scripts/release/assemble.py, docs/RELEASE.md of asc: schema 1, the five
targets, an archive of each in tar.gz and zip, the editor packages, the SBOM, the package manager files) into a directory:
manifest.json, SHA256SUMS and the files. The hub checks size and SHA-256 of every file it offers, so each file is as long as the
manifest says (sparse: it takes no disk) and its checksum is of its zeros.
usage: python3 download-manifest.py <directory>"""
import hashlib, json, os, sys

d = sys.argv[1]
os.makedirs(d, exist_ok=True)
MB = 1_000_000
VERSION = '1.4.2'
# target, os, arch, size of the archive in MB
TARGETS = [
    ('x86_64-unknown-linux-gnu', 'linux', 'x86_64', 21.2),
    ('aarch64-unknown-linux-gnu', 'linux', 'aarch64', 19.6),
    ('x86_64-apple-darwin', 'macos', 'x86_64', 19.8),
    ('aarch64-apple-darwin', 'macos', 'aarch64', 18.4),
    ('x86_64-pc-windows-msvc', 'windows', 'x86_64', 17.2),
]


def zeros(name, size):
    """Makes the file of `size` zero bytes and returns its SHA-256."""
    h = hashlib.sha256()
    chunk = bytes(1 << 20)
    left = size
    while left > 0:
        n = min(left, len(chunk))
        h.update(chunk[:n])
        left -= n
    with open(os.path.join(d, name), 'wb') as f:
        f.truncate(size)
    return h.hexdigest()


def text(name, body):
    data = body.encode()
    with open(os.path.join(d, name), 'wb') as f:
        f.write(data)
    return hashlib.sha256(data).hexdigest(), len(data)


files, sums = [], []


def row(kind, os_, arch, target, fmt, name, sha, size):
    files.append({'version': VERSION, 'kind': kind, 'os': os_, 'arch': arch, 'target': target, 'format': fmt, 'file': name,
                  'sha256': sha, 'size': size, 'signature': name + '.minisig'})
    sums.append(f'{sha}  {name}')
    text(name + '.minisig', f'untrusted comment: file:{name} version:{VERSION}\n')


for target, os_, arch, mb in TARGETS:
    for fmt in (['tar.gz', 'zip'] if os_ != 'windows' else ['zip']):
        name = f'asc-{VERSION}-{target}.{fmt}'
        size = int(mb * MB)
        row('cli', os_, arch, target, fmt, name, zeros(name, size), size)
# the files of the release that are not the command line: the hub's page leaves them out
for kind, name, fmt in [('vscode', f'asc-vscode-{VERSION}.vsix', 'vsix'), ('jetbrains', f'asc-jetbrains-{VERSION}.zip', 'zip'),
                        ('sbom', f'asc-{VERSION}.cdx.json', 'json'), ('homebrew', 'asc.rb', 'rb'), ('scoop', 'asc.scoop.json', 'json'),
                        ('winget', 'winget-asc.asc.yaml', 'yaml'), ('aur', 'PKGBUILD', 'pkgbuild')]:
    sha, size = text(name, name + '\n')
    row(kind, 'any', 'any', None, fmt, name, sha, size)

manifest = {'schema': 1, 'name': 'asc', 'version': VERSION, 'commit': '0123456789abcdef0123456789abcdef01234567',
            'released_at': '2026-10-20T12:00:00Z', 'files': sorted(files, key=lambda e: (e['kind'], e['os'], e['arch'], e['file']))}
with open(os.path.join(d, 'manifest.json'), 'w') as f:
    json.dump(manifest, f, indent=2, sort_keys=True)
text('SHA256SUMS', '\n'.join(sorted(sums)) + '\n')
text('SHA256SUMS.minisig', f'untrusted comment: file:SHA256SUMS version:{VERSION}\n')
