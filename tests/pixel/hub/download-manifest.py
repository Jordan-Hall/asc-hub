"""Writes the release the Download boards show into a directory: manifest.json and the files it lists (the files are
small and made of their own names, so the sizes in the manifest are the board's and the checksums are of nothing real).
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
files = []
for name, os_, arch, kind, mb in FILES:
    files.append({'name': name, 'os': os_, 'arch': arch, 'kind': kind, 'size': int(mb * MB),
                  'sha256': hashlib.sha256(name.encode()).hexdigest(), 'signature': name + '.minisig'})
manifest = {
    'version': '1.4.2', 'released_at': '2026-10-20', 'release_notes_url': 'https://github.com/Jordan-Hall/asc/releases/tag/v1.4.2',
    'files': files, 'checksums': 'SHA256SUMS', 'checksums_signature': 'SHA256SUMS.minisig',
    'minisign_public_key': 'RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y76G',
    'extensions': {'vscode': 'https://marketplace.visualstudio.com/items?itemName=asc.asc', 'jetbrains': 'https://plugins.jetbrains.com/plugin/asc'},
    'package_managers': {'brew': 'asc', 'winget': 'asc.asc', 'scoop': 'asc'},
}
with open(os.path.join(d, 'manifest.json'), 'w') as f:
    json.dump(manifest, f, indent=2)
for name in [f['name'] for f in files] + [f['signature'] for f in files] + ['SHA256SUMS', 'SHA256SUMS.minisig']:
    with open(os.path.join(d, name), 'w') as f:
        f.write(name + '\n')
