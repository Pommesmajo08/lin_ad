"""Collect tested artifacts with stable release filenames and SHA-256 checksums."""
import hashlib
from pathlib import Path
import shutil
root=Path(__file__).resolve().parents[1]
output=root/'release'
output.mkdir(exist_ok=True)
assets={
    'install.sh':'scripts/install-release.sh',
    'install.ps1':'scripts/install-release.ps1',
    'install-windows.cmd':'install-from-release.cmd',
    'linad-server-docker.tar.gz':'dist/linad-server-docker.tar.gz',
    'linad-client-linux-amd64.tar.gz':'dist/linad-client-linux-amd64.tar.gz',
    'linad-client-windows-amd64.zip':'dist/linad-client-windows-amd64.zip',
    'linad-linux-provisioner-amd64.tar.gz':'dist/linad-linux-provisioner-amd64.tar.gz',
    'linad-linux-provisioner-arm64.tar.gz':'dist/linad-linux-provisioner-arm64.tar.gz',
    'linad-windows-agent-amd64.zip':'dist/linad-windows-agent-amd64.zip',
    'install-agent-windows.cmd':'install-agent-from-release.cmd',
    'installation.md':'docs/installation.md',
}
# Validate completeness before replacing any previous output.
for source in assets.values():
    if not (root/source).is_file():
        raise SystemExit('Missing release input: '+source+'; build and export the tested server image first.')
for target,source in assets.items():
    shutil.copyfile(root/source,output/target)
with (output/'SHA256SUMS').open('w') as sums:
    for target in sorted(assets):
        with (output/target).open('rb') as source:
            digest=hashlib.sha256(source.read()).hexdigest()
        sums.write(f'{digest}  {target}\n')
print('Release assets prepared in release/ (not published).')
