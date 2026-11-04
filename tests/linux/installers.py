"""Installer filesystem tests, exclusively in a disposable root test container."""
import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
assert os.geteuid()==0 and os.environ.get('LINAD_ISOLATED_PAM_TEST')=='1' and Path('/.dockerenv').exists()
root=Path('/src')
def run(script,*args,ok=True):
    result=subprocess.run(['sh',str(root/'scripts'/script),*args],capture_output=True,text=True,timeout=20)
    assert (result.returncode==0)==ok,(script,result.stderr)
    return result
with tempfile.TemporaryDirectory() as tmp:
    config=Path(tmp)/'server config'
    run('install-server.sh','--no-start','--check','--config-dir',str(config))
    assert not config.exists()
    result=run('install-server.sh','--no-start','--config-dir',str(config))
    secret=(config/'admin-password').read_text()
    assert len(secret)==64 and secret not in result.stdout and secret not in result.stderr
    info=(config/'admin-password').stat()
    assert info.st_uid==65532 and stat.S_IMODE(info.st_mode)==0o400
    compose=config/'compose.yaml'
    content=compose.read_text()+'\n# local configuration\n'
    compose.write_text(content)
    run('install-server.sh','--no-start','--config-dir',str(config))
    assert compose.read_text()==content and (config/'admin-password').read_text()==secret
    foreign=Path(tmp)/'foreign'
    foreign.mkdir()
    (foreign/'compose.yaml').write_text('services: {}\n')
    run('install-server.sh','--no-start','--config-dir',str(foreign),ok=False)
    assert not (foreign/'admin-password').exists()
    prefix=Path(tmp)/'client with spaces'
    run('install-client-linux.sh','--prefix',str(prefix))
    desktop=prefix/'share/applications/linad.desktop'
    assert 'Exec="'+str(prefix)+'/bin/linad-client-gui"' in desktop.read_text()
    assert (prefix/'bin/linad-client').stat().st_mode & stat.S_IXUSR
    run('install-client-linux.sh','--prefix',str(prefix))
    run('install-client-linux.sh','--prefix',str(prefix),'--uninstall')
    assert not desktop.exists() and not (prefix/'bin/linad-client').exists()
    assert (config/'admin-password').read_text()==secret
print('PASS: server preflight/no mutation, secret privacy/permissions, repeat setup/config preservation, foreign config refusal, client desktop/CLI install/update/uninstall with spaces')
