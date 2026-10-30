"""Writes the release the Download boards show into a directory: manifest.json and the files it lists. The hub checks size and
SHA-256 of every file it offers, so each file is as long as the board says (sparse: it takes no disk) and the checksum is of its zeros.
usage: python3 download-manifest.py <directory>"""
import hashlib, json, os, sys

d = sys.argv[1]
os.makedirs(d, exist_ok=True)
MB = 1_000_000
FILES = [
    # name, os, arch, kind, size
    ('asc-1.4.2-aarch64-apple-darwin.tar.gz', 'macos', 'arm64', 'archive', 18.4),
    ('asc-1.4.2-x86_64-apple-darwin.tar.gz', 'macos', 'x86_64', 'archive', 19.8),
    ('asc-1.4.2-x86_64-unknown-linux-musl.tar.gz', 'linux', 'x86_64', 'archive', 21.2),
    ('asc-1.4.2-aarch64-unknown-linux-musl.tar.gz', 'linux', 'arm64', 'archive', 19.6),
    ('asc_1.4.2_amd64.deb', 'linux', 'x86_64', 'installer', 20.1),
    ('asc-1.4.2-1.x86_64.rpm', 'linux', 'x86_64', 'installer', 20.3),
    ('asc-1.4.2-x86_64-pc-windows-msvc.msi', 'windows', 'x86_64', 'installer', 17.9),
    ('asc-1.4.2-x86_64-pc-windows-msvc.zip', 'windows', 'x86_64', 'archive', 17.2),
    ('asc-1.4.2-aarch64-pc-windows-msvc.zip', 'windows', 'arm64', 'archive', 16.4),
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


files = []
for name, os_, arch, kind, mb in FILES:
    size = int(mb * MB)
    files.append({'name': name, 'os': os_, 'arch': arch, 'kind': kind, 'size': size, 'sha256': zeros(name, size), 'signature': name + '.minisig'})
manifest = {
    'version': '1.4.2', 'released_at': '2026-10-20', 'release_notes_url': 'https://github.com/Jordan-Hall/asc/releases/tag/v1.4.2',
    'files': files, 'checksums': 'SHA256SUMS', 'checksums_signature': 'SHA256SUMS.minisig',
    'minisign_public_key': 'RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y76G',
    'extensions': {'vscode': 'https://marketplace.visualstudio.com/items?itemName=asc.asc', 'jetbrains': 'https://plugins.jetbrains.com/plugin/asc'},
    'package_managers': {'brew': 'asc', 'winget': 'asc.asc', 'scoop': 'asc'},
}
with open(os.path.join(d, 'manifest.json'), 'w') as f:
    json.dump(manifest, f, indent=2)
for name in [f['signature'] for f in files] + ['SHA256SUMS', 'SHA256SUMS.minisig']:
    with open(os.path.join(d, name), 'w') as f:
        f.write(name + '\n')
